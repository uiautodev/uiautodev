package webserver

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/codeskyblue/jsonc"
)

// pluginZipMaxSize caps the uploaded plugin archive size (100 MB).
const pluginZipMaxSize = 100 << 20

// pluginSourceUpload marks a plugin record whose archive came from a manual
// upload rather than a plugin market.
const pluginSourceUpload = "upload"

// pluginIDPattern matches the allowed characters of a plugin id. A leading dot
// is rejected because dot-prefixed directories are reserved for internal
// bookkeeping (e.g. install backups) and are skipped by the plugin scanner.
var pluginIDPattern = regexp.MustCompile(`^[a-z0-9_-][a-z0-9._-]*$`)

// validatePluginID reports whether id is a legal plugin id. Unlike
// sanitizePluginDirName it never rewrites the id: illegal ids are rejected.
func validatePluginID(id string) error {
	if id == "" || id == "." || id == ".." || !pluginIDPattern.MatchString(id) {
		return fmt.Errorf("invalid plugin id: %q", id)
	}
	return nil
}

// extractPluginZip extracts a plugin zip archive into userDir. It supports both
// archives that contain a top-level plugin directory and those that contain the
// plugin files at the archive root. The target directory is named after the
// plugin's manifest name (its ID), not the archive filename. Returns the plugin
// directory name and the absolute path to the extracted plugin.
func extractPluginZip(zipPath, userDir string) (string, string, error) {
	if !strings.HasSuffix(strings.ToLower(zipPath), ".zip") {
		return "", "", fmt.Errorf("only .zip files are supported")
	}
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return "", "", err
	}

	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", "", fmt.Errorf("open zip: %w", err)
	}
	defer zipReader.Close()

	if len(zipReader.File) == 0 {
		return "", "", fmt.Errorf("zip archive is empty")
	}

	// Detect a single shared top-level directory (plugin zipped with its parent folder).
	rootDir := detectZipRootDir(zipReader.File)

	// Determine the target directory name from the plugin manifest id, falling
	// back to the zip's top-level plugin directory name, then the manifest name.
	// It is independent of the archive filename. Same-name installs overwrite.
	manifest, err := readPluginManifestFromZip(zipReader, rootDir)
	if err != nil {
		return "", "", err
	}
	pluginName := sanitizePluginDirName(manifest.ID)
	if pluginName == "" && rootDir != "" {
		pluginName = sanitizePluginDirName(strings.TrimSuffix(rootDir, "/"))
	}
	if pluginName == "" {
		pluginName = sanitizePluginDirName(manifest.Name)
	}
	if pluginName == "" {
		return "", "", fmt.Errorf("invalid plugin name: %q", manifest.Name)
	}

	targetDir := filepath.Join(userDir, pluginName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", "", err
	}

	if err := extractZipEntries(zipReader, targetDir, rootDir); err != nil {
		os.RemoveAll(targetDir)
		return "", "", err
	}

	return pluginName, targetDir, nil
}

// extractZipEntries writes all files from the archive into targetDir, stripping
// the optional rootDir prefix. It enforces zip-slip protection and per-entry
// size limits. The caller owns targetDir cleanup on error.
func extractZipEntries(zipReader *zip.ReadCloser, targetDir, rootDir string) error {
	for _, f := range zipReader.File {
		name := f.Name
		if rootDir != "" {
			name = strings.TrimPrefix(name, rootDir)
		}
		name = strings.TrimPrefix(filepath.FromSlash(name), string(filepath.Separator))
		if name == "" {
			continue
		}

		// Zip-slip protection: ensure the resolved path stays inside targetDir.
		dest := filepath.Join(targetDir, name)
		rel, err := filepath.Rel(targetDir, dest)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("illegal path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := writeZipEntry(dest, f); err != nil {
			return err
		}
	}
	return nil
}

// installPluginFromMarketZip installs a downloaded market zip into userDir under
// the expected plugin id. It validates that the manifest id matches the id from
// the request and atomically replaces any existing installation, keeping the old
// version intact if extraction or validation fails.
func installPluginFromMarketZip(zipPath, expectedID, userDir string) (string, error) {
	if err := validatePluginID(expectedID); err != nil {
		return "", err
	}

	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer zipReader.Close()
	if len(zipReader.File) == 0 {
		return "", fmt.Errorf("zip archive is empty")
	}

	rootDir := detectZipRootDir(zipReader.File)
	manifest, err := readPluginManifestFromZip(zipReader, rootDir)
	if err != nil {
		return "", err
	}
	if manifest.ID != expectedID {
		return "", fmt.Errorf("plugin id mismatch: archive has %q, expected %q", manifest.ID, expectedID)
	}

	if err := os.MkdirAll(userDir, 0755); err != nil {
		return "", err
	}
	// Extract into a system temp dir (not under userDir) so a concurrent plugin
	// scan never picks up the half-extracted directory.
	tmpDir, err := os.MkdirTemp("", "uiautodev-market-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	if err := extractZipEntries(zipReader, tmpDir, rootDir); err != nil {
		return "", err
	}
	if _, err := readPluginManifest(tmpDir); err != nil {
		return "", fmt.Errorf("invalid plugin: %w", err)
	}

	targetDir := filepath.Join(userDir, expectedID)
	if err := replaceDir(tmpDir, targetDir); err != nil {
		return "", err
	}
	return targetDir, nil
}

// replaceDir replaces target with src, keeping a backup of the old directory
// until the swap succeeds so a failure never loses the previous version. The
// backup is dot-prefixed so a concurrent plugin scan ignores it.
func replaceDir(src, target string) error {
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return moveDir(src, target)
		}
		return err
	}
	backup := filepath.Join(
		filepath.Dir(target),
		"."+filepath.Base(target)+".bak-"+strconv.FormatInt(time.Now().UnixNano(), 10),
	)
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := moveDir(src, target); err != nil {
		if restoreErr := os.Rename(backup, target); restoreErr != nil {
			return fmt.Errorf("install failed: %w (restore failed: %v)", err, restoreErr)
		}
		return err
	}
	if err := os.RemoveAll(backup); err != nil {
		slog.Warn("failed to remove plugin backup dir", "path", backup, "error", err)
	}
	return nil
}

// moveDir moves src to dst, falling back to a recursive copy when a plain
// rename fails (e.g. src and dst live on different filesystems).
func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyDir(src, dst); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return os.RemoveAll(src)
}

// copyDir recursively copies regular files and directories from src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// readPluginManifestFromZip reads and parses plugin.json from a zip archive.
// The plugin id or name may be empty; extractPluginZip resolves the final
// directory name.
func readPluginManifestFromZip(zipReader *zip.ReadCloser, rootDir string) (*PluginManifest, error) {
	for _, f := range zipReader.File {
		name := f.Name
		if rootDir != "" {
			name = strings.TrimPrefix(name, rootDir)
		}
		if filepath.Base(name) != "plugin.json" || filepath.Dir(name) != "." {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		var manifest PluginManifest
		if err := jsonc.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("invalid plugin.json: %w", err)
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("plugin.json not found in archive")
}

// sanitizePluginDirName converts a plugin name into a safe single directory
// name component, replacing characters that are invalid in file paths.
func sanitizePluginDirName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimLeft(b.String(), ".- ")
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return ""
	}
	return cleaned
}

func writeZipEntry(dest string, f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	n, err := io.Copy(out, io.LimitReader(rc, pluginZipMaxSize+1))
	if err != nil {
		return err
	}
	if n > pluginZipMaxSize {
		return fmt.Errorf("zip entry too large: %s", f.Name)
	}
	return nil
}

// detectZipRootDir returns the common top-level directory name when all entries
// in the archive share it, or an empty string when files are at the archive root.
func detectZipRootDir(files []*zip.File) string {
	root := ""
	for _, f := range files {
		// Zip entries are always slash-separated, so inspect f.Name as-is
		// instead of filepath.FromSlash, which would rewrite '/' to '\' on
		// Windows and break the prefix detection below.
		name := strings.TrimPrefix(f.Name, "/")
		if name == "" {
			continue
		}
		idx := strings.IndexByte(name, '/')
		if idx < 0 {
			return ""
		}
		top := name[:idx]
		if root == "" {
			root = top
		} else if root != top {
			return ""
		}
	}
	if root == "" {
		return ""
	}
	// Only treat as a wrapping directory if it appears for every entry.
	for _, f := range files {
		name := strings.TrimPrefix(f.Name, "/")
		if name != "" && name != root && !strings.HasPrefix(name, root+"/") {
			return ""
		}
	}
	return root + "/"
}
