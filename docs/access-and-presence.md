# 访问控制与设备占用

多人共用一台服务器时，uiautodev 可以复用第三方认证提供的身份，并显示每台设备的占用者与观察者。设计背景见 [设计文档](design/device-presence-and-auth.md)。

## 身份

身份来源优先级从高到低：

| 来源 | 条件 | 身份 |
|---|---|---|
| 可信反代头 | 直连对端 IP 在 `access.auth.trusted_proxies` 内 | `proxy:<user>` |
| 内置登录 | `-auth` 的 JWT / Basic Auth 通过 | `login:<username>` |
| 匿名 | 以上都没有 | `anon:<sessionID 前 8 位>` |

匿名身份的 `sessionID` 来源：

| 客户端 | 来源 |
|---|---|
| 浏览器首次导航 | 随机 32 位 `ua_sid` cookie（HttpOnly、SameSite=Lax、HTTPS 时 Secure、1 年） |
| 带 Bearer 的脚本 | `sha256(token)` 前 16 位 |
| 无 cookie 的脚本 | `sha256(IP + User-Agent + 身份 ID)` 前 16 位（匿名请求没有身份，只用 IP + User-Agent） |

- 同一浏览器的多个标签页视为同一个身份。
- 不同浏览器、无痕窗口、清除 cookie 后都会变成新的匿名身份。
- 只有直连对端 IP 在可信代理列表内时才会解析 `X-Forwarded-*` / `X-Auth-Request-*` 头，默认不信任任何代理。
- 兜底会话把身份算进 hash：经同一反代或同一台机器访问的两个登录用户即使 IP 和 User-Agent 相同，也不会共用 presence 会话。
- `require_identity=true` 时匿名的 API / 设备流请求返回 401；`/api/v1/me`、前端静态资源与 `/mcp/`（单独用 MCP token 认证）不受影响。

支持的请求头：`X-Forwarded-User` / `X-Auth-Request-User`（必需其一）、`X-Forwarded-Email` / `X-Auth-Request-Email`、`X-Forwarded-Preferred-Username`、`X-Forwarded-Groups`、`X-Forwarded-Avatar`。

## 配置

设置页「访问与占用」可直接修改，也可以提前手写 `config.json`（手工修改后需要重启）：

```json
{
  "access": {
    "auth": {
      "trusted_proxies": ["127.0.0.1/32", "::1/128"],
      "require_identity": false,
      "signout_url": "/oauth2/sign_out"
    },
    "presence": {
      "enabled": true,
      "conflict_policy": "preempt",
      "idle_timeout": "10m",
      "allow_observers": true
    }
  }
}
```

| 字段 | 默认值 | 说明 |
|---|---|---|
| `trusted_proxies` | `[]` | 可信代理 IP 或 CIDR，每项一个 |
| `require_identity` | `false` | 无身份时拒绝 API / 设备流请求 |
| `signout_url` | `""` | 非空时导航头像菜单显示「退出登录」 |
| `presence.enabled` | `true` | 是否启用设备占用管理 |
| `presence.conflict_policy` | `preempt` | `preempt` 允许抢占，`exclusive` 只能等待 |
| `presence.idle_timeout` | `10m` | 空闲释放时间：`1m` / `5m` / `10m` / `30m` / `1h` |
| `presence.allow_observers` | `true` | 是否允许其他用户以只读方式加入 |

缺失的字段取默认值，显式写出的值（包括 `false`）优先。因此只写 `access.auth`（上面的 oauth2-proxy 示例）时占用功能仍然开启。

## 设备占用

| 状态 | 含义 |
|---|---|
| `free` | 无人控制、无人观察 |
| `mine` | 我是唯一控制者 |
| `shared` | 我所在的身份有多个控制会话 |
| `viewing` | 我是观察者 |
| `busy` | 他人控制中 |
| `preempted` | 我的会话刚被抢占 |

续期与释放：

- HTTP 请求、心跳 `PUT /api/v1/devices/:id/presence` 都会刷新 `LastSeen`。
- 正在传输的 WebSocket 流（`scrcpy` / `frames`）注册期间不参与空闲回收：持续出画面但没有 HTTP 请求也算活跃；流断开时刷新 `LastSeen`。
- 空闲超过 `idle_timeout` 后自动释放，并关闭 uiautodev 自己开启的 adbkit 本地转发端口（不执行 `adb disconnect`）。
- 页面卸载时 `DELETE .../presence` 释放会话，最后一个会话离开后有 30 秒宽限期；释放会同时关闭该会话的 WebSocket 流。
- `DELETE /api/v1/devices/:id/connect` 会强制释放整台设备。
- `/api/v1/devices/:id/meta` 属于被动请求：只续期已有会话，不会因为修改备注而占用设备。

冲突策略：

| 场景 | 结果 |
|---|---|
| 同一身份 | 直接加入，`mine` / `shared` |
| 设备空闲 | 直接获得控制权 |
| 已被控制，`preempt` 且未确认抢占 | 423 `device_busy`，`can_takeover=true` |
| 已被控制，`preempt` 且 `takeover=true` | 驱逐其他身份的控制会话，观察者保留 |
| 已被控制，`exclusive` | 423，`can_takeover=false` |
| 观察者 | 不参与冲突，`allow_observers=true` 时随时加入 |

抢占后，被驱逐的会话下一次请求返回 409 `preempted`（保留 2 分钟），其 WebSocket 会以 4001 关闭码断开。

## 观察者权限

| 接口 | 观察者 | 控制者 |
|---|---|---|
| 设备信息、`/meta`、截图、screens、uitree、window/size、encoders、apps 列表、clipboard GET | ✅ | ✅ |
| `stream/.../scrcpy`、`mjpeg`、`frames` | ✅ 只出画面，注入的输入事件被服务端丢弃 | ✅ |
| `actions/*`、`shell`、`GET/DELETE connect`、clipboard PUT、apps start/stop | ❌ 403 `read_only` | ✅ |
| `/proxy/devices/:id/*` | ❌ 403 `read_only` | ✅ |

`allow_observers=false` 时，被占用设备的读接口也返回 423。

观察者的只读在服务端生效，而不只依赖前端 `control=false`：`scrcpy` 与 `frames` 是双向 WebSocket，服务端会丢弃观察者发来的输入消息，所以观察者无法通过该连接操作设备。

## MCP

MCP 使用 `mcp_token` 认证，请求身份固定为 `mcp:admin`（显示名 `MCP`），占用与冲突规则和 Web 端一致。

- 读类工具（`screenshot`、`dump_xml`、`get_device_info` 等）自动申请：空闲时控制，被占用时观察。
- 控制类工具（`tap`、`swipe`、`shell`、`app_start` 等）必须有控制权；观察者返回 `read_only`，他人控制返回 `device_busy`。
- `acquire_device(device_id, mode=control|view, takeover=false)`：显式申请设备。
- `release_device(device_id)`：释放当前 MCP 会话对设备的占用。
- 只有一个共享 token，所有 MCP 客户端共用 `mcp:admin` 身份；需要按人区分时应使用不同实例。

`mcp_token` 在设置页「MCP 配置」中管理。`GET /api/v1/mcp/token` 受管理密码保护，MCP 页面需要先在设置页解锁才能显示 token；`/mcp/tools` 的工具元数据本身是公开的。

## 接口与错误码

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/me` | 当前身份 |
| GET/PUT | `/api/v1/access/config` | 读取/保存配置（管理密码保护） |
| GET/PUT | `/api/v1/presence` | 全量占用快照 / 批量续期 |
| PUT/DELETE | `/api/v1/devices/:id/presence` | 单设备续期 / 释放 |

| HTTP | 场景 |
|---|---|
| 401 | `require_identity=true` 且匿名的 API / 设备流请求 |
| 403 | 观察者调用控制接口（`read_only`） |
| 409 | 会话已被抢占（`preempted`，带 notice） |
| 423 | 设备被他人控制（`device_busy`，带 Occupancy） |

## oauth2-proxy 部署示例

建议 uiautodev 只监听 `127.0.0.1`，由 oauth2-proxy 对外提供认证并转发身份头：

```nginx
location / {
    auth_request /oauth2/auth;
    auth_request_set $user   $upstream_http_x_auth_request_user;
    auth_request_set $email  $upstream_http_x_auth_request_email;
    auth_request_set $name   $upstream_http_x_auth_request_preferred_username;
    proxy_set_header X-Forwarded-User  $user;
    proxy_set_header X-Forwarded-Email $email;
    proxy_set_header X-Forwarded-Preferred-Username $name;
    proxy_pass http://127.0.0.1:33299;
}
```

对应配置：

```json
{ "access": { "auth": { "trusted_proxies": ["127.0.0.1/32"] } } }
```
