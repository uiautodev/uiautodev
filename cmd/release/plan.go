package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uiautodev/uiautodev/internal/update"
)

// uploadItem describes a local file to upload and its key in the bucket.
type uploadItem struct {
	Path string
	Key  string
	Size int64
}

// planUploads builds the upload plan from the assets listed in the manifest:
// <prefix><version>/<name>. Only files listed in the manifest are uploaded, so
// temporary files in the directory are never pushed to production.
func planUploads(assets []update.Asset, dir, prefix, version, manifestName string) ([]uploadItem, error) {
	names := make([]string, 0, len(assets)+1)
	for _, asset := range assets {
		names = append(names, asset.Name)
	}
	names = append(names, manifestName)

	items := make([]uploadItem, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if skipAsset(name) || seen[name] {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("manifest asset %s does not exist in %s: %w", name, dir, err)
		}
		seen[name] = true
		items = append(items, uploadItem{
			Path: path,
			Key:  assetKey(prefix, version, name),
			Size: info.Size(),
		})
	}
	return items, nil
}

func assetKey(prefix, version, name string) string {
	key := version + "/" + name
	if prefix == "" {
		return key
	}
	return strings.TrimSuffix(prefix, "/") + "/" + key
}

// skipAsset skips macOS metadata and local build logs, matching the exclusion
// rules in upload.sh.
func skipAsset(name string) bool {
	switch name {
	case "", ".DS_Store":
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".log")
}

func contentTypeFor(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		return "application/json"
	case ".dmg":
		return "application/x-apple-diskimage"
	case ".zip":
		return "application/zip"
	case ".exe":
		return "application/vnd.microsoft.portable-executable"
	default:
		return "application/octet-stream"
	}
}
