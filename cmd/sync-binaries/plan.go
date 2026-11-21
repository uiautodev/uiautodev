package main

import (
	"path/filepath"
	"strings"
)

// remoteObject 是远端 listing 里的一条记录。
type remoteObject struct {
	Key  string
	Size int64
}

// downloadItem 描述一次下载：远端 key 和本地路径。
type downloadItem struct {
	Key  string
	Path string
	Size int64
}

// planDownloads 过滤目录占位对象，并跳过本地大小一致的已存在文件。
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

// relativeKey 去掉前缀并拒绝空路径、绝对路径和穿越路径。
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

// isExecutableName 与 Makefile 里的 chmod 规则保持一致。
func isExecutableName(name string) bool {
	switch name {
	case "adb", "ios":
		return true
	}
	return false
}
