package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Masterminds/semver"
)

// shouldPublishLatest 判断是否要把本次清单发布到 updates/latest.json。
// 线上版本更高时拒绝发布，避免误推旧版本导致客户端收到“降级更新”。
// 读取线上清单失败时不阻塞发布（返回 true），只记录原因。
func shouldPublishLatest(ctx context.Context, client *http.Client, url, newVersion string) (bool, string) {
	if url == "" {
		return true, "未配置线上清单检查，直接发布"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return true, fmt.Sprintf("构造线上清单请求失败，继续发布: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return true, fmt.Sprintf("读取线上清单失败，继续发布: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return true, fmt.Sprintf("线上清单返回 %s，继续发布", resp.Status)
	}

	var current struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&current); err != nil {
		return true, fmt.Sprintf("解析线上清单失败，继续发布: %v", err)
	}

	currentVersion := strings.TrimPrefix(strings.TrimSpace(current.Version), "v")
	if currentVersion == "" {
		return true, "线上清单没有版本号，继续发布"
	}

	online, err := semver.NewVersion(currentVersion)
	if err != nil {
		return true, fmt.Sprintf("线上版本号 %q 无法解析，继续发布", currentVersion)
	}
	release, err := semver.NewVersion(newVersion)
	if err != nil {
		return true, fmt.Sprintf("本次版本号 %q 无法解析，继续发布", newVersion)
	}

	if online.GreaterThan(release) {
		return false, fmt.Sprintf("线上版本 %s 高于本次 %s，跳过更新 latest.json", currentVersion, newVersion)
	}
	return true, fmt.Sprintf("线上版本 %s，本次 %s，更新 latest.json", currentVersion, newVersion)
}
