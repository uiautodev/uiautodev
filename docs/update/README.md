# 应用内更新

桌面端支持在应用内检查、下载、校验并安装新版本，安装完成后自动重启。

- 检查更新：macOS 菜单栏 **文件 → 检查更新...**
- 更新详情：配置页的“应用更新”区块显示当前版本、最新版本和发布说明
- 下载安装：显示百分比进度，校验 `release.json` 中的 SHA256 后替换应用并自动重启

[查看界面截图](screenshots.md)

## 版本清单

发布时会在版本目录生成 `release.json`，并同步到：

- `https://dl.uiauto.dev/updates/latest.json`
- `https://dl.uiauto.dev/<version>/release.json`

客户端只读取 `updates/latest.json`，安装包和 SHA256 都来自同一份清单。
