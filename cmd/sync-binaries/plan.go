package main

import (
	"path/filepath"
	"strings"
)

// remoteObject is an entry from the remote listing.
type remoteObject struct {
	Key  string
	Size int64
}

// downloadItem describes a single download: remote key and local path.
type downloadItem struct {
	Key  string
	Path string
	Size int64
}

// planDownloads filters out directory placeholder objects and skips existing
// files whose local size already matches.
func planDownloads(objects []remoteObject, dir, prefix string, sizeOf func(string) (int64, bool)) ([]downloadItem, int) {
	items := make([]downloadItem, 0, len(objects))
	skipped := 0
	for _, object := range objects {
		relative, ok := relativeKey(prefix, object.Key)
		if !ok {
			continue
		}
		path := filepath.Join(dir, filepath.FromSlash(relative))
		if size, exists := sizeOf(path); exists && size == object.Size {
			skipped++
			continue
		}
		items = append(items, downloadItem{Key: object.Key, Path: path, Size: object.Size})
	}
	return items, skipped
}

// relativeKey strips the prefix and rejects empty paths, directory placeholders
// and traversing paths.
func relativeKey(prefix, key string) (string, bool) {
	relative := strings.TrimPrefix(key, prefix)
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" || strings.HasSuffix(relative, "/") {
		return "", false
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == ".." {
			return "", false
		}
	}
	return relative, true
}

// isExecutableName matches the chmod rules in the Makefile.
func isExecutableName(name string) bool {
	switch name {
	case "adb", "ios":
		return true
	}
	return false
}
