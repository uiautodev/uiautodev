package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// manifestAsset 是 release.json 里的一条资产记录。
type manifestAsset struct {
	Name string `json:"name"`
}

// releaseManifest 是应用内更新读取的 release.json 结构中上传需要的部分。
type releaseManifest struct {
	Version string          `json:"version"`
	Assets  []manifestAsset `json:"assets"`
}

// uploadItem 描述一个待上传的本地文件和它在 bucket 中的 key。
type uploadItem struct {
	Path string
	Key  string
	Size int64
}

func readManifest(path string) (releaseManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return releaseManifest{}, err
	}
	var manifest releaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return releaseManifest{}, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return manifest, nil
}

// planUploads 按 manifest 里的资产列表生成上传计划：<prefix><version>/<name>。
// 只上传清单里列出的文件，避免把目录里的临时产物也推到线上。
func planUploads(manifest releaseManifest, dir, prefix, version, manifestName string) ([]uploadItem, error) {
	names := make([]string, 0, len(manifest.Assets)+1)
	for _, asset := range manifest.Assets {
		names = append(names, asset.Name)
	}
	names = append(names, manifestName)

	items := make([]uploadItem, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if skipAsset(name) || seen[name] {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("清单里的 %s 在 %s 中不存在: %w", name, dir, err)
		}
		seen[name] = true
		items = append(items, uploadItem{
			Path: filepath.Join(dir, name),
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

// skipAsset 跳过 macOS 元数据和本地构建日志，与 upload.sh 的排除规则保持一致。
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
