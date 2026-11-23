# 开发

面向二次开发、构建与发布的说明。使用方式见 [README.md](README.md)。

API: https://uiautodev.apifox.cn/

```sh
# 新克隆项目后先安装依赖（Go 模块 + 前端依赖）
make setup

make dev
```

浏览器访问 http://localhost:5173 （后端 33299，改动 Go 代码后自动重启；可用 `DEV_BACKEND_PORT` / `DEV_FRONTEND_PORT` 覆盖端口）

以下需要安装的二进制：

- rclone（可选，仅作为 S3 凭据来源）: https://rclone.org/downloads/
- golangci-lint: `brew install golangci-lint`

## 国内镜像

```sh
# set proxy
git config --global http.https://github.com.proxy http://127.0.0.1:10808

go env -w GOPROXY=https://goproxy.cn,direct
```

## 编译发布

打包由 GitHub Actions 完成：推送一个 `x.y.z` 的 tag 会触发 `.github/workflows/release.yml`，
并行构建 macOS（含签名与公证）、Windows 桌面端与各平台 server 二进制，最后把产物和
`release.json` 挂到对应的 GitHub Release。

```sh
./release.sh
```

脚本会算出推荐版本号、打 tag 并 push，然后等待 Actions 完成；结束时可选是否把产物同步到 R2。

发布前需要在仓库 Settings → Secrets and variables → Actions 里配置：

| Secret | 说明 |
| --- | --- |
| `MACOS_CERTIFICATE` | Developer ID Application 证书 `.p12` 的 base64（`base64 -i cert.p12 \| pbcopy`） |
| `MACOS_CERTIFICATE_PASSWORD` | `.p12` 密码 |
| `CODESIGN_IDENTITY` | 例如 `Developer ID Application: Shengxiang Sun (74Z4C6R8NT)` |
| `APPLE_API_KEY` | App Store Connect API Key `.p8` 的 base64（`base64 -i AuthKey_XXX.p8 \| pbcopy`） |
| `APPLE_API_KEY_ID` | Key ID，例如 `R43ZT2CVS2` |
| `APPLE_API_ISSUER` | Issuer ID（UUID，在 App Store Connect → 用户和访问 → 集成 里查看） |
| `R2_ACCESS_KEY_ID` | S3 兼容存储的 Access Key ID（Cloudflare R2，和 rclone 的 `r2` remote 一致） |
| `R2_SECRET_ACCESS_KEY` | S3 兼容存储的 Secret Access Key |
| `R2_ENDPOINT` | S3 endpoint，`https://<account-id>.r2.cloudflarestorage.com` |

公证也支持用 `APPLE_ID` + `APPLE_APP_PASSWORD` + `APPLE_TEAM_ID`（App 专用密码）代替 API Key，二选一即可。

发布 workflow 会自动把产物上传到 S3 兼容存储（bucket `uiautodev`，`<version>/` + `updates/latest.json`）并刷新下载缓存，
所以确认更新只需要跑 `./release.sh`。需要手动重推时：

```sh
gh release download <version> -D dist --clobber
./upload.sh
curl -X POST https://get.uiauto.dev/api/cache/refresh
```

如果公证凭据还没配好、只想验证构建链路，可以设置仓库变量 `SKIP_NOTARIZATION=1`：此时 macOS 只签名不公证，
且 `Publish release` job 会被跳过（不产生 GitHub Release）。配好 `APPLE_*` secrets 后删掉这个变量即可恢复正常发布。

本机仍然可以全量打包（不依赖 Actions）：`./build.sh`，需要本机安装
[wails3](https://golang.google.cn/)（版本与 `go.mod` 中的 wails v3 一致）、bun、`dmgbuild>=1.6.7`。
上传产物由 `cmd/release upload` 完成（AWS SDK，S3 兼容）：`./upload.sh` 上传，凭据优先读
`S3_*` / `R2_*` 环境变量，没有时从 `~/.config/rclone/rclone.conf` 的 `r2` remote 读取，所以本机不需要装 rclone。
构建所需的 `binaries/` 用 `scripts/download-binaries.sh all` 拉取（本地构建会自动调用，CI 同样使用该脚本），
从 `https://dl.uiauto.dev/binaries` 下载，不接触存储凭据。

对于 Apple Silicon ARM Mac

```sh
softwareupdate --install-rosetta --agree-to-license
```

## DMG 安装界面

DMG 的背景图源文件是 `build/dmg/background.html`（720×440 逻辑像素），
导出的 `background.png` / `background@2x.png` 会在打包时自动合成为 HiDPI 背景。
改完 HTML 后重新生成（需要本机装有 Google Chrome）：

```sh
./build/dmg/make-background.sh
```

窗口大小、图标位置在 `dmg_settings.py` 里调整。

macOS 26.2 起，旧版 dmgbuild 生成的 DMG 背景图不会显示，需要 1.6.7 及以上：

```sh
pip install -U "dmgbuild>=1.6.7"
```

## Code Sign

Mac 的签名与公证由 `scripts/macos-sign.sh` 完成，`build.sh` 打包 DMG 时会自动调用。

正式分发需要钥匙串里有 `Developer ID Application` 证书，并配置公证凭据。
脚本默认使用 `Developer ID Application: Shengxiang Sun (74Z4C6R8NT)` 和
`uiautodev-notary` 这个公证 profile，无需每次设置环境变量；换机器或换账号时再覆盖：

```sh
# 首次配置公证凭据（会安全提示输入 App 专用密码）
xcrun notarytool store-credentials uiautodev-notary \
  --apple-id "you@example.com" --team-id "TEAMID"

# 可选：覆盖默认配置
export CODESIGN_IDENTITY="Developer ID Application: Foo (TEAMID)"
export NOTARY_PROFILE=my-profile
# 或者直接用环境变量提供公证凭据
export APPLE_ID=you@example.com APPLE_APP_PASSWORD=xxx APPLE_TEAM_ID=TEAMID
```

手动签名/公证：

```sh
./scripts/macos-sign.sh sign "build/bin/Uiautodev Desktop.app"
./scripts/macos-sign.sh notarize "dist/UiautodevDesktop-mac-<version>.dmg"
```

没有证书时会退化为 ad-hoc 签名：能修复因为内置 adb/ios 被改动导致的“包已损坏”，
但下载后仍会被 Gatekeeper 拦截，需要公证才能彻底解决。临时绕过可执行：

```sh
xattr -dr com.apple.quarantine "/Applications/Uiautodev Desktop.app"
```

## LINKS

- https://tangoadb.dev/scrcpy/
- map-go: https://github.com/mark3labs/mcp-go
- MCP协议描述: https://modelcontextprotocol.io/
- Android中的ADB下载 https://developer.android.com/tools/releases/platform-tools
- yadb: https://github.com/ysbing/YADB

## Alternatives

- 腾讯大佬surunzi开源的aya，功能非常多，尤其是增加了一个Server可获取图标，logcat也支持package过滤，甚至还有鸿蒙版本 https://github.com/liriliri/aya
- 非常全面的版本，比tango的功能还多，但是找不到联系人 https://github.com/PandaTestGrid/panda-web-scrcpy
- 使用jmuxer的简易版本 https://github.com/baixin1228/web-scrcpy/
- 看着挺牛逼，就是编译不了 https://github.com/NetrisTV/ws-scrcpy
