package main

import (
	"strings"

	"github.com/uiautodev/uiautodev/webserver"
)

type nativeLabels struct {
	File         string
	OpenLog      string
	OpenPlugin   string
	OpenConfig   string
	Settings     string
	CheckUpdates string
	UpdateTitle  string
	AvailableFmt string
	UpToDateFmt  string
	CheckFailed  string
	UpdateFailed string
	UpdateButton string
	ViewDetails  string
	Later        string
}

func currentNativeLabels() nativeLabels {
	return nativeLabelsFor(webserver.GetLocale())
}

func nativeLabelsFor(locale string) nativeLabels {
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		return nativeLabels{
			File:         "File",
			OpenLog:      "Open Log Directory",
			OpenPlugin:   "Open Plugin Directory",
			OpenConfig:   "Open Config Directory",
			Settings:     "Settings...",
			CheckUpdates: "Check for Updates...",
			UpdateTitle:  "Update Available",
			AvailableFmt: "Version %s is available (current v%s).",
			UpToDateFmt:  "You are up to date (v%s).",
			CheckFailed:  "Failed to check for updates:\n",
			UpdateFailed: "Update Failed",
			UpdateButton: "Update",
			ViewDetails:  "View Details",
			Later:        "Later",
		}
	}
	return nativeLabels{
		File:         "文件",
		OpenLog:      "打开日志目录",
		OpenPlugin:   "打开插件目录",
		OpenConfig:   "打开配置目录",
		Settings:     "设置...",
		CheckUpdates: "检查更新...",
		UpdateTitle:  "发现新版本",
		AvailableFmt: "新版本 v%s 可用（当前 v%s）。",
		UpToDateFmt:  "已是最新版本（v%s）。",
		CheckFailed:  "检查更新失败：\n",
		UpdateFailed: "更新失败",
		UpdateButton: "更新",
		ViewDetails:  "查看详情",
		Later:        "稍后",
	}
}
