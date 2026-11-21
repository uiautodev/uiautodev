# uiautodev

[中文](README.md) | [English](README.en.md)

使用电脑方便地控制安卓（Android）、iOS、鸿蒙（HarmonyOS）手机的桌面应用与 Web 服务端，支持**批量控制**多台设备。

主要特性：

- 在电脑上实时查看手机屏幕，并用鼠标键盘远程操作：常用按键、旋转、裁剪画面、复制截图、屏幕录制与画面冻结
- 支持 Android（scrcpy）、iOS（WDA，Beta）与鸿蒙多平台设备，并内置无需真机的演示设备
- 群控：批量查看并操作多台设备，可同时打开多块屏幕
- 设备剪贴板：在本机与设备之间双向复制文本（Android / HarmonyOS）和图片（Android 10+）
- 元素查看：UI 层级树与节点属性、XPath 查询，支持布局与截图导出/导入
- 应用管理：查看当前前台应用，并通过接口或 MCP 列出、启动、停止应用
- MCP Server：把设备操作能力暴露给 AI / 自动化客户端，支持 Streamable HTTP 与 SSE，并提供独立 Token
- 插件扩展：内置插件、手动安装与插件市场，插件可注入设备页卡片与标签页
- Web 服务端：浏览器直接使用，支持 Basic Auth、HTTPS、ngrok 与多实例隔离
- 集群：Master / Slave 级联聚合多机设备，支持主动连接与 Master 直连，并提供远程 adb connect
- 中英文界面、暗黑模式，以及在应用内检查更新并安装

![Android、iOS、鸿蒙三端演示](docs/assets/uiautodev-demo.gif)

## 下载与使用

桌面端（macOS / Windows）以及各平台服务端二进制都可以从 https://get.uiauto.dev 下载。
服务端也可以直接用 `npx uiautodev run` 启动。

## 仓库结构

| 目录 | 说明 |
|------|------|
| 根目录 | 桌面端与服务端源码：`main.go`、`webserver/`、`cmd/`、`internal/`、`frontend/`、`build/` |
| `npm/` | `uiautodev` npm 包：下载并启动对应平台的服务端二进制，即 `npx uiautodev run` |
| `images/` | 文档图片 |
| `docs/` | 文档资源、更新说明与设计文档 |

桌面端与服务端推送 `x.y.z` tag 后由 `.github/workflows/release.yml` 构建发布；npm 包版本独立维护，由 `.github/workflows/npm-release.yml` 自动升版本并发布。

## iOS 辅助程序

`ios` 体积较大，不再打进安装包。首次使用 iOS 功能时会从
`https://dl.uiauto.dev/binaries/<os>/ios` 下载，并缓存到
`~/Library/Application Support/uiautodev/bin/`（Windows 为
`%LOCALAPPDATA%\uiautodev\bin`）。如果系统 PATH 里已经有 `ios`，会优先使用它。

## uiautodev-server 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `localhost:33299` | HTTP 监听地址 |
| `-open` | | 服务启动后自动在浏览器中打开首页（当 `-addr` 为 `:port` 形式时会打开 `http://localhost:port/`） |
| `-auth <user:pass>` | | 基础认证账号（可多次指定） |
| `-proxy-log <file>` | | 代理日志路径（留空则不记录） |
| `-config-dir <dir>` | `~/.config/uiautodev` | 配置目录，存放 `config.json` 及默认插件目录 `<config-dir>/plugins`；部署多个 server 时用它隔离各自的状态 |
| `-plugin-dir <dir>` | `<config-dir>/plugins` | 用户插件的安装与加载目录（最高优先级，同名插件覆盖旧版/内置目录） |
| `-tls-addr <addr>` | | HTTPS 监听地址（同时在 `-addr` 上启动 HTTP 跳转） |
| `-tls-cert <file>` | | TLS 证书文件路径（PEM 格式） |
| `-tls-key <file>` | | TLS 私钥文件路径（PEM 格式） |
| `-ngrok` | | 通过 ngrok 将服务公开暴露 |
| `-ngrok-authtoken <token>` | | ngrok authtoken（也可通过 `NGROK_AUTHTOKEN` 环境变量设置） |
| `-master-url <url>` | | 上游 master 的 HTTP(S) 地址，例如 `https://master.example.com`；指定后当前节点会接入为它的从节点，但自身仍是 master，只需填 origin，路径由程序补全 |
| `-token <token>` | | master 生成的从节点 Token；也可用 `UIAUTODEV_CLUSTER_TOKEN` 环境变量 |
| `-name <name>` | 机器 hostname | 从节点显示名称（可选） |
| `-master-insecure` | | 跳过 master 证书校验（master 使用自签名证书时） |
| `-disable-device-listing` | | 禁用设备列表接口（安全加固） |
| `-verbose` | | 开启 debug 日志 |
| `-version` | | 打印版本号并退出 |

```sh
# 启动服务并自动打开浏览器
uiautodev-server -open

# 指定端口
uiautodev-server -addr :8080 -open
```

## 集群模式（Master / Slave）

每个节点（`uiautodev-server` 和桌面版）默认都是 Master，会聚合所有在线 Slave 的设备，并在自己的界面、API、MCP 和 `adb connect` 中统一暴露。节点同时也可以作为 Slave 接入另一个 Master，因此可以级联组网。

支持两种连接方式：

- **传统模式**：Slave 主动出站连接 Master，适合 Slave 在内网、Master 有公网地址的场景。
- **直连模式**：Master 主动通过 HTTP(S) + Basic Auth 连接公网 Slave，适合 Slave 有公网地址、Master 在内网的场景。直连模式不需要给 Slave 增加任何启动参数，旧版本 Slave 也可直接接入。

### Master

所有节点默认开启集群功能，不需要额外参数：

```sh
uiautodev-server -addr :33299 -auth admin:pass
```

桌面版同样内置 Master，但只监听 `localhost`，因此只能接收同机 Slave 或通过隧道/端口映射接入的 Slave。

在 Web 设置页「远程节点」中添加 Slave。传统模式会生成一次性 Token（只显示一次，Master 只保存哈希）；直连模式填写 Slave URL 和 Basic Auth 凭据。

默认情况下，Master 只展示 Slave 本机的设备；Slave 自己接的下级节点的设备不会向上聚合（避免重复和环路）。需要级联展示时，在从节点条目上打开「包含下级设备」，Master 就会把该 Slave 聚合到的设备一并展示。级联可以多层，但每一跳都需要在对应从节点上开启；聚合时用节点身份（`config.json` 的 `cluster.node_id`）组成链路做环路检测，互相连接或成环的 Master 不会产生重复设备。

ADB relay 默认使用：

- 监听 `0.0.0.0`
- public host 自动取请求的 Host
- 端口范围 `20000-20100`
- TTL 1 小时（滑动续期，活跃期间每 5 分钟刷新时自动续期），空闲 30 分钟（仅在没有任何活跃 adb 连接时生效）

需要覆盖时只修改 `config.json`，没有对应的命令行参数：

```json
{
  "cluster": {
    "adb_relay": {
      "bind": "0.0.0.0",
      "public_host": "",
      "port_range": [20000, 20100],
      "ttl": "1h",
      "idle_timeout": "30m",
      "allow_same_ip_only": true,
      "allowed_cidrs": []
    }
  }
}
```

部署时需要在防火墙/安全组放行 `port_range` 对应的 TCP 端口，否则远程 `adb connect` 无法连接。

### Slave（传统模式）

```sh
# 仅隧道（推荐生产使用，不开放本地 HTTP 端口）
npx uiautodev run -master-url https://master.example.com -token u-xxxxxx-xxxxxxxxxxxxxxxxxxxx -addr ""

# 同时保留本地 UI，便于调试
npx uiautodev run -master-url https://master.example.com -token u-xxxxxx-xxxxxxxxxxxxxxxxxxxx -addr 127.0.0.1:33299
```

- 指定 `-master-url` 即把当前节点接入为某个 Master 的 Slave，不需要额外的 `-slave` 开关。
- 接入上游之后，当前节点仍然是 Master，可以在本地「远程节点」里继续添加自己的 Slave（级联）。
- `-master-url` 只接受 `http://` 或 `https://` 的 origin；程序自动拼接 `/api/v1/cluster/connect`，并转换成 `ws/wss`。
- `-name` 可省略，默认使用机器 hostname。
- `-addr ""` 表示不启动本地 HTTP listener，只通过隧道服务。
- Token 建议使用 `UIAUTODEV_CLUSTER_TOKEN` 环境变量，避免出现在 `ps` 中。
- 接入上游时不启用 `-auth`（隧道里的请求由 Master 端认证），本地监听如需鉴权请另起实例或去掉 `-master-url`。
- Master 使用自签名证书时，Slave 需要加 `-master-insecure`。

### 直连模式（Master 主动连接公网 Slave）

Slave 按普通服务方式启动，暴露默认 `33299` 端口，并用 `-auth` 开启 Basic Auth：

```sh
uiautodev-server -addr 0.0.0.0:33299 -auth user:pass
```

Master 在 Web 设置页「远程节点」中选择「Master 直连」，填写：

- Slave 地址，例如 `https://slave.example.com:33299`
- Basic Auth 用户名 / 密码
- 自签名证书时可勾选「跳过 TLS 证书校验」

注意事项：

- 直连模式的 Slave 按普通服务启动即可，不需要 `-master-url` 或任何集群参数；旧版本 Slave 只要开放标准 API 和 Basic Auth 即可。
- Master 需要能访问 Slave 的 `33299` 端口；Slave 防火墙只需放行 Master IP。
- 直连模式下所有设备流量（设备列表、操作、截图、屏幕流、`/proxy/devices/...`、`adb connect`）都由 Master 转发。
- 直连 ADB 时，Master 会用 Slave URL 的 host 加上 adbkit 端口去连接，因此 Slave 防火墙需要对 Master IP 放行 adbkit 端口范围（adbkit 监听 `:<port>`）。如果 Slave URL 经过反向代理，请确保该 host 的 adbkit 端口同样可达，或改用能直达 Slave 的地址。
- `HTTP` 会以明文传输 Basic Auth 凭据；生产环境建议使用 HTTPS（`-tls-addr` / `-tls-cert` / `-tls-key`）或 TLS 反向代理。
- Slave 不能使用 `-disable-device-listing`，否则 Master 无法聚合设备。

### 远程 adb connect

Master 上调用 `GET /api/v1/devices/<remote-id>/connect` 会返回：

```json
{
  "type": "tcp",
  "transport": "adb",
  "endpoint": "master.example.com:20000"
}
```

直接连接 Master 返回的地址即可：

```sh
adb connect master.example.com:20000
adb devices
```

注意：

- ADB over TCP 是明文协议；请通过防火墙、`allowed_cidrs`、短 TTL 控制暴露面。
- 默认 `allow_same_ip_only` 只允许发起 `/connect` 请求的同一来源 IP 连接 relay；如果浏览器与 adb 客户端出口 IP 不同，请配置 `allowed_cidrs` 或关闭该限制。
- Master 如果部署在 Nginx/Caddy 后面，反向代理只负责 HTTPS/WSS；ADB relay 的原始 TCP 端口需要单独放行，不能只开 443。
- Nginx 代理隧道时需支持 WebSocket Upgrade、较长的 `proxy_read_timeout`，并关闭 buffering。

### 说明

- 桌面端内置 Master，但只监听 `localhost`，因此只能通过直连模式接入 Slave，不能作为 Slave；要让某个节点接入上游 Master，需要使用 `uiautodev-server -master-url`。
- Master 轮换 Token 或删除 Slave 会立即断开旧连接；Slave 会自动重连（使用新 Token 时需要更新配置）。
- Slave 掉线后，其设备会在 Master 上标记为不可用，恢复连接后自动刷新。

## MCP 认证

MCP（Model Context Protocol）使用一个**独立的长期 token**，与登录凭据（`-auth` 的账号密码 / JWT）无关。

- 在 Web 设置页「MCP 配置」中填写或随机生成，也可直接编辑 `config.json` 的 `mcp_token` 字段。
- 启用了 `-auth` 时，若 `mcp_token` 为空会在启动时**自动随机生成**（`ua-` 前缀）。
- 客户端通过 `?token=<mcp_token>` 或 `Authorization: Bearer <mcp_token>` 访问 `/mcp/*`。
- 为空时：若启用了 `-auth`，`/mcp/*` 回退为登录鉴权（JWT / Basic）；否则 `/mcp/*` 完全开放。
- 修改后立即生效，无需重启；需要轮换时在设置页重新生成并保存即可。

## 打赏支持

如果这个项目对你有帮助，欢迎请我喝杯咖啡 ☕

- [爱发电](https://afdian.com/a/codeskyblue)
- 微信赞赏码：

<img src="docs/assets/wechat-reward.jpg" width="220" alt="微信赞赏码">

## 开发

编译、调试、打包签名与发布流程见 [DEVELOP.md](DEVELOP.md)。

## LICENSE

本项目采用 [MIT License](LICENSE)。
