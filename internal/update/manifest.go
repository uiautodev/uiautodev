package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildManifest hashes every release artifact in dir and returns the manifest
// published as release.json. Metadata files and the version marker are skipped.
func BuildManifest(dir, version, tag, notes, releaseURL, publishedAt string) (Manifest, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	tag = strings.TrimSpace(tag)
	if tag == "" {
		tag = version
	}
	if version == "" {
		version = strings.TrimPrefix(tag, "v")
	}
	if version == "" {
		return Manifest{}, fmt.Errorf("version is required")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Manifest{}, err
	}
	assets := make([]Asset, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if skipManifestAsset(name, version) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := entry.Info()
		if err != nil {
			return Manifest{}, err
		}
		hash, err := fileSHA256(path)
		if err != nil {
			return Manifest{}, err
		}
		assets = append(assets, Asset{Name: name, Size: info.Size(), SHA256: hash})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	if len(assets) == 0 {
		return Manifest{}, fmt.Errorf("no release assets found in %s", dir)
	}
	return Manifest{
		Version:     version,
		Tag:         tag,
		PublishedAt: strings.TrimSpace(publishedAt),
		ReleaseURL:  strings.TrimSpace(releaseURL),
		Notes:       strings.TrimSpace(notes),
		Assets:      assets,
	}, nil
}

// ReadManifest reads a release.json manifest from path.
func ReadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return manifest, nil
}

// WriteManifest writes the manifest as indented JSON.
func WriteManifest(path string, manifest Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func skipManifestAsset(name, version string) bool {
	switch name {
	case "", ".DS_Store", "release.json", "SHA256SUMS", version:
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".log")
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
