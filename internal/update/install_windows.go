//go:build windows

package update

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func installDesktopAsset(assetPath, exe string) error {
	temp, err := os.MkdirTemp("", "uiautodev-dmg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err := unzip(assetPath, temp); err != nil {
		return err
	}
	newExe, err := findFile(temp, filepath.Base(exe))
	if err != nil {
		return err
	}

	// Drop backups left by earlier updates before creating another one.
	cleanupOldExecutables(exe)

	backup := exe + ".old-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("replace running executable: %w", err)
	}
	if err := copyFile(newExe, exe); err != nil {
		_ = os.Rename(backup, exe)
		return err
	}
	copyExtras(temp, filepath.Dir(exe), filepath.Base(exe))
	return nil
}

func unzip(src, dest string) error {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		name := filepath.Clean(file.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(dest, name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func findFile(dir, name string) (string, error) {
	var found string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("update package does not contain %s", name)
	}
	return found, nil
}

// copyExtras copies tools and support files next to the executable. Files in
// use (for example a running adb.exe) are skipped so the update still succeeds.
func copyExtras(sourceDir, targetDir, exeName string) {
	_ = filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return nil
		}
		if strings.EqualFold(filepath.Base(rel), exeName) {
			return nil
		}
		if strings.HasPrefix(rel, "__MACOSX") || strings.HasSuffix(rel, ".DS_Store") {
			return nil
		}
		target := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil
		}
		_ = copyFile(path, target)
		return nil
	})
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
