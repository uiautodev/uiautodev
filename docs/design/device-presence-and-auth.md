# 设备占用、观察者与第三方身份接入设计

> 状态：已实现（`webserver/access_config.go`、`webserver/identity.go`、`webserver/presence.go`）
> 日期：2026-10-30
> 范围：`webserver/`、`cmd/uiautodev-server/`、`frontend/`、文档

## 1. 背景

当前工具单用户使用很顺手，但多人共用同一台服务器时：

- 无法知道某台设备正在被谁使用，容易互相抢占。
- 设备资源（adbkit 本地转发端口）按固定 1 小时超时释放，与实际是否在用无关。
- 用户管理不该由本工具承担，应复用 oauth2-proxy / nginx auth_request 等第三方认证。

## 2. 目标与非目标

### 目标

- 复用第三方认证提供的身份；没有第三方时按匿名处理，匿名以**浏览器会话**为单位区分不同的人。
- 能显示每台设备的占用者、观察者，并在导航右上角显示当前用户头像。
- 占用按活跃续期：查看、操作、拉流、心跳都算活跃；空闲 10 分钟后释放。
- 释放时关闭 uiautodev 自己开启的 adbkit 本地转发端口，不执行 `adb disconnect`。
- 支持观察者模式：可以看画面、截图、UI 树，但不能调用任何控制接口。
- 冲突策略可配置：允许抢占（需确认） / 不允许抢占（只能等对方释放或空闲超时）。
- 所有参数放在设置界面，持久化到 `config.json`；也支持提前手写配置文件引导部署。
- 有限取值一律使用枚举。

### 非目标

- 不做用户 CRUD、角色/权限系统、登录页、头像上传。
- 不做排队、顺位、通知。
- 占用状态不持久化，进程重启即清空。
- 不接管 `adb connect` 的状态，不执行 `adb disconnect`。

## 3. 总体结构

```
请求
  │
  ├─ Identity 中间件：解析代理头 / 登录用户 / 匿名会话，得到 Identity + SessionID
  │
  ├─ Presence 中间件：设备维度请求续期、按 mode 做权限判定
  │
  ├─ 设备接口：放行 / 423 device_busy / 403 read_only / 409 preempted
  │
  └─ PresenceManager：会话表 → 占用快照 → 空闲回收 → ReleaseDevice（关 adbkit 端口）
```

## 4. 身份与会话

### 4.1 Identity

```go
type Identity struct {
    ID        string   // proxy:alice | login:alice | anon:3f2a
    Name      string
    Email     string
    Groups    []string
    AvatarURL string   // 来自 X-Forwarded-Avatar，可为空
    Anonymous bool
}
```

### 4.2 身份来源（优先级从高到低）

| 来源 | 条件 | ID / Name |
|---|---|---|
| 反代头 | 直连对端 IP 在 `access.auth.trusted_proxies` 内 | `proxy:<user>`；名字优先 `X-Forwarded-Preferred-Username`，其次 user |
| 内置登录 | 现有 JWT / Basic Auth 通过 | `login:<username>` |
| 匿名 | 以上都没有 | `anon:<hash8(sessionID)>` / `Anonymous` |

支持的请求头：

- `X-Forwarded-User` 或 `X-Auth-Request-User`（必需其一）
- `X-Forwarded-Email` / `X-Auth-Request-Email`
- `X-Forwarded-Preferred-Username`
- `X-Forwarded-Groups`（逗号分隔）
- `X-Forwarded-Avatar`（可选）

**安全约束：对端不在可信 CIDR 内时，所有 `X-Forwarded-*` 一律忽略。**

### 4.3 会话 ID（SessionID）

| 客户端 | SessionID 来源 |
|---|---|
| 浏览器 | `ua_sid` cookie（首次访问生成 32 hex，HttpOnly、SameSite=Lax、HTTPS 时 Secure、1 年） |
| 带 Bearer 的 API/MCP | `sha256(token)` 前 16 位 |
| 无 cookie 的脚本 | `hash(ip + User-Agent + Identity.ID)` 兜底，保证同一客户端身份稳定 |

兜底值包含已解析的身份：同一个反代后面或同一台机器上的两个登录用户往往 IP 和 User-Agent 完全相同，只有把身份算进 hash，两个人才不会共用同一个 presence 会话（否则后到者会直接接管前者的设备）。匿名请求没有身份可用，仍按 `ip + User-Agent` 计算。

匿名身份按 **cookie** 区分，因此：

- 同一浏览器的多个标签页 = 同一身份 = 一起使用，不冲突。
- 不同浏览器 / 无痕窗口 / 不同电脑 = 不同身份 = 按冲突策略处理。
- 清除 cookie 后会变成新的匿名身份。

### 4.4 当前用户接口

`GET /api/v1/me`：

```json
{
  "id": "anon:3f2a",
  "name": "Anonymous",
  "email": "",
  "anonymous": true,
  "auth_source": "anonymous",
  "avatar_url": "",
  "signout_url": "/oauth2/sign_out",
  "presence_enabled": true
}
```

`auth_source` 枚举：`proxy | login | anonymous`。

`signout_url` 来自配置，为空时前端不显示“退出登录”。

## 5. 配置：config.json + 设置界面

### 5.1 不再新增命令行参数

除现有 `--addr`、`--config-dir`、`--auth` 等，本功能不新增 flag。所有开关放在设置页，写入 `config.json`。

### 5.2 配置结构

```go
type appConfig struct {
    // ...
    Access *AccessConfig `json:"access,omitempty"`
}

type AccessConfig struct {
    Auth     AuthAccessConfig     `json:"auth"`
    Presence PresenceAccessConfig `json:"presence"`
}

type AuthAccessConfig struct {
    TrustedProxies  []string `json:"trusted_proxies"`  // CIDR 或 IP
    RequireIdentity bool     `json:"require_identity"` // 无身份头时 401
    SignoutURL      string   `json:"signout_url"`      // 例如 /oauth2/sign_out
}

type PresenceAccessConfig struct {
    Enabled        bool           `json:"enabled"`
    ConflictPolicy ConflictPolicy `json:"conflict_policy"`
    IdleTimeout    IdleTimeout    `json:"idle_timeout"`
    AllowObservers bool           `json:"allow_observers"`
}
```

`AccessConfig` / `PresenceAccessConfig` 实现了 `UnmarshalJSON`：缺失的字段取默认值，只有显式写出的值（包括 `false`）才覆盖默认。否则像 `{"access":{"auth":{...}}}` 这样的配置会让 `enabled` / `allow_observers` 落到 bool 零值 `false`，静默关闭占用功能。

### 5.3 枚举

```go
type ConflictPolicy string

const (
    ConflictPolicyPreempt   ConflictPolicy = "preempt"
    ConflictPolicyExclusive ConflictPolicy = "exclusive"
)

type DeviceAccessMode string

const (
    AccessModeControl DeviceAccessMode = "control"
    AccessModeView    DeviceAccessMode = "view"
)

type IdleTimeout string

const (
    IdleTimeout1m  IdleTimeout = "1m"
    IdleTimeout5m  IdleTimeout = "5m"
    IdleTimeout10m IdleTimeout = "10m"
    IdleTimeout30m IdleTimeout = "30m"
    IdleTimeout1h  IdleTimeout = "1h"
)

type OccupancyState string

const (
    OccupancyFree      OccupancyState = "free"
    OccupancyMine      OccupancyState = "mine"
    OccupancyShared    OccupancyState = "shared"
    OccupancyViewing   OccupancyState = "viewing"
    OccupancyBusy      OccupancyState = "busy"
    OccupancyPreempted OccupancyState = "preempted"
)
```

每个枚举提供 `Valid()`，设置接口保存前校验。前端在 `schemas.ts` 用 `as const` 数组 + union 类型镜像同一组取值。

### 5.4 默认值

| 配置 | 默认 |
|---|---|
| `presence.enabled` | `true` |
| `presence.conflict_policy` | `preempt` |
| `presence.idle_timeout` | `10m` |
| `presence.allow_observers` | `true` |
| `access.auth.trusted_proxies` | `[]`（不信任任何代理头） |
| `access.auth.require_identity` | `false` |
| `access.auth.signout_url` | `""` |

释放宽限期固定 30 秒，不做配置。

### 5.5 配置文件示例

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

支持提前手写该文件完成部署配置；手工修改后需要重启生效。

### 5.6 加载与热更新

- 启动时读取 `config.json`，校验后放入 `atomic.Pointer[AccessConfig]`。
- 设置页保存后立即更新内存缓存，presence 中间件、清扫协程、身份解析都读缓存，无需重启。
- 配置非法（手写错误）时记录警告并回退到默认值，不阻止服务启动。

### 5.7 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/access/config` | 读取配置（管理会话保护） |
| PUT | `/api/v1/access/config` | 覆盖保存（`managementAuthMiddleware`） |

校验规则：

- `trusted_proxies`：每个元素必须是合法 IP 或 CIDR，否则 400。
- `signout_url`：空、站内相对路径或 http(s) URL。
- 枚举值必须在白名单内。
- `require_identity=true` 且 `trusted_proxies` 为空时允许保存，但设置页显示警告（此时只有已登录用户能通过）。
- `require_identity` 只拦截 `/api/`、`/proxy/`、`/stream/` 请求（`/api/v1/me` 除外）：前端静态资源仍可加载，`/mcp/` 由 MCP token 自行认证，否则合法 MCP 客户端会被提前 401。

### 5.8 设置界面

新增 `frontend/src/lib/components/settings/AccessSettings.svelte`，在 `/config` 导航增加「访问与占用」。

表单项：

- 可信代理由多行文本维护（每行一个 CIDR）。
- 要求身份头：Switch。
- 退出登录地址：Input。
- 启用占用管理：Switch。
- 空闲释放：Select（1 / 5 / 10 / 30 / 60 分钟）。
- 冲突策略：Select（允许抢占 / 不允许抢占）。
- 允许观察者：Switch。
- 底部显示当前配置文件路径。

## 6. 设备占用（Presence）

### 6.1 内存结构

```go
type PresenceManager struct {
    mu          sync.Mutex
    devices     map[string]*devicePresence
    emptySince  map[string]time.Time      // 显式离开后的释放宽限
    evicted     map[string]evictionRecord // 被抢占会话，保留 2 分钟
    onIdle      func(deviceID string)     // ReleaseDevice
}

type devicePresence struct {
    Since    time.Time
    Sessions map[string]*presenceSession // SessionID -> 会话
}

type presenceSession struct {
    Identity Identity
    Mode     DeviceAccessMode
    LastSeen time.Time
    Remote   string
    UA       string
    Kick     func(reason string) // WS 流注册的踢出回调
    Streams  int                 // 已注册的 WS 流数量，>0 时不参与空闲回收
}

type evictionRecord struct {
    Reason string
    By     Occupant
    At     time.Time
}
```

- 占用者按 `Identity.ID` 聚合，同一身份多个会话合并显示。
- 匿名 ID 已经按浏览器区分，因此两个匿名浏览器就是两条占用者记录。

### 6.2 占用状态

| state | 含义 |
|---|---|
| `free` | 无人控制、无人观察 |
| `mine` | 我是唯一控制者 |
| `shared` | 我所在的身份在控制，且有多个会话 |
| `viewing` | 我是观察者 |
| `busy` | 他人控制中，我不能直接操作 |
| `preempted` | 我的会话刚被抢占 |

### 6.3 续期来源

1. 设备维度 HTTP 请求：`/api/v1/devices/:id/*`、`/stream/devices/:id/*`、`/proxy/devices/:id/*` 命中后刷新 `LastSeen`。
2. 显式心跳：设备页每 30 秒 `PUT /api/v1/devices/:id/presence`；多设备页用批量 `PUT /api/v1/presence`，不用 N 个请求。
3. WebSocket：流建立和断开时刷新 `LastSeen`；有流注册期间会话不参与空闲回收（持续出画面但没有任何 HTTP 请求也算活跃）。被抢占或 `DELETE .../presence` 释放时通过 `Kick` 主动关闭（关闭码 4001，reason 带原因）。

页面不可见（`visibilitychange`）时停止心跳，让空闲超时自然生效。

### 6.4 释放

| 触发 | 行为 |
|---|---|
| 页面卸载 / `pagehide` | `DELETE .../presence`，最后一个会话离开后进入 30 秒宽限，期间有人进入则取消 |
| 空闲超时（10 分钟） | 清扫会话，设备空了立即释放 |
| `DELETE /api/v1/devices/:id/connect` | 强制释放整台设备（所有会话） |
| 被抢占 | 只移除被抢占的控制会话，资源直接移交新控制者，不关端口 |

`onIdle` 调用现有 `deviceManagerInstance.ReleaseDevice(deviceID)`，只关闭 adbkit 的 tcpusb 本地转发端口，不执行 `adb disconnect`。

`deviceCacheManager` 的固定 1 小时超时：

- `presence.enabled=true`：改为长兜底（8 小时），实际释放由 presence 驱动。
- `presence.enabled=false`：保持现有一小时行为，接口不做任何拦截。
- 顺手修复 `deviceCacheItem.deadline` 无锁读写的竞态。

### 6.5 空闲清理

- 清扫协程每 2 秒运行一次，空闲阈值从 AccessConfig 实时读取。
- 清理顺序：过期会话 → 空设备 → 触发 `onIdle` → 清理过期 `evicted` 记录。
- 回调在锁外执行，避免死锁；调用前会在锁内再次确认设备仍然为空，避免刚被重新获取的会话被这次释放连带断开。

## 7. 冲突策略

| 场景 | 结果 |
|---|---|
| 同一身份（包括同一浏览器多标签） | 允许加入，`mine` / `shared` |
| 不同身份，设备空闲 | 直接获得控制权 |
| 不同身份，已被控制，策略 `preempt` | 返回 423 + `can_takeover=true`；确认后带 `takeover=true` 重新 PUT，踢掉原控制会话 |
| 不同身份，已被控制，策略 `exclusive` | 返回 423 + `can_takeover=false`；前端提示“占用中，请稍后再试” |
| 观察者 | 不参与冲突，任何时候都能加入（开关允许时） |

抢占细节：

- 只驱逐控制会话，观察者保留。
- 被驱逐的会话记录 `evicted`，下一次设备请求返回 409 `preempted` + 抢占者信息。
- 被驱逐会话正在使用的 WS 立即关闭。
- 前端收到 `preempted` 后 toast 提示并返回设备列表。

无排队、无顺位、无通知。

## 8. 观察者模式

### 8.1 进入方式

- 设备被占用时，弹窗提供「抢占设备」（`preempt` 策略）/「仅观察」/「取消」。
- 设备空闲时也可以直接以观察者身份进入（列表上眼睛按钮或设备页 `?mode=view`）。
- 控制者可在设备页切换为只读，交出控制权但不断开画面。

### 8.2 权限矩阵

| 接口 | 观察者 | 控制者 |
|---|---|---|
| `GET /devices/:id`、`GET/PUT/DELETE /meta` | ✅ | ✅ |
| `screenshot`、`screens`、`uitree`、`window/size`、`encoders` | ✅ | ✅ |
| `stream/.../scrcpy`、`mjpeg`、`frames` | ✅ 只出画面，注入的输入事件被丢弃 | ✅ |
| `actions/tap`、`swipe`、`press_key`、`input_text`、`clear_text` | ❌ 403 `read_only` | ✅ |
| `POST /shell` | ❌ 403 | ✅ |
| `GET /connect` | ❌ 403 | ✅ |
| `/proxy/devices/:id/*` | ❌ 403 | ✅ |
| `DELETE /connect` | ❌ 403 | ✅ |

`allow_observers=false` 时，被占用设备的读接口也返回 423，弹窗只剩抢占/取消。

### 8.3 无会话请求的兜底

| 请求 | 设备空闲 | 设备被他人控制 |
|---|---|---|
| 读接口 | 自动建立 control 会话（兼容插件） | 允许观察时自动建立 view 会话，否则 423 |
| 控制接口 | 自动建立 control 会话（兼容旧行为） | 423，抢占必须显式确认 |

## 9. 接口清单

### 9.1 Presence

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/presence` | 全量占用快照 |
| PUT | `/api/v1/presence` | 批量续期/获取，body `{"devices":{"a-1":{"mode":"control"}}}` |
| PUT | `/api/v1/devices/:device_id/presence` | 单设备续期/获取，body `{"mode":"control","takeover":false}` |
| DELETE | `/api/v1/devices/:device_id/presence` | 释放自己的会话 |

响应统一为 `{"devices": {"<id>": Occupancy}}`（单设备接口直接返回 `Occupancy`）。

### 9.2 数据结构

```go
type Occupant struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Email     string    `json:"email,omitempty"`
    AvatarURL string    `json:"avatar_url,omitempty"`
    Anonymous bool      `json:"anonymous"`
    Sessions  int       `json:"sessions"`
    LastSeen  time.Time `json:"last_seen"`
}

type Occupancy struct {
    DeviceID    string         `json:"device_id"`
    State       OccupancyState `json:"state"`
    InUse       bool           `json:"in_use"`
    Mine        bool           `json:"mine"`
    Since       *time.Time     `json:"since,omitempty"`
    Occupants   []Occupant     `json:"occupants,omitempty"`
    Observers   []Occupant     `json:"observers,omitempty"`
    Policy      ConflictPolicy `json:"policy"`
    CanTakeover bool           `json:"can_takeover"`
    CanObserve  bool           `json:"can_observe"`
    Notice      *Notice        `json:"notice,omitempty"`
}

type Notice struct {
    Reason string   `json:"reason"` // preempted
    By     Occupant `json:"by"`
    At     time.Time `json:"at"`
}
```

`GET /api/v1/devices` 的每个 `APIDevice` 内嵌 `occupancy`，沿用现有 3 秒轮询，不新增轮询。

### 9.3 错误码

| HTTP | 场景 |
|---|---|
| 401 | `require_identity=true` 且无身份 |
| 403 | 观察者调用控制接口（`read_only`） |
| 409 | 会话已被抢占（`preempted`，带 notice） |
| 423 | 设备被他人控制（`device_busy`，带 Occupancy） |

## 10. 前端设计

### 10.1 身份与头像

- `UserAvatar.svelte`：确定性 identicon，seed = `identity.id`，不请求网络。
  - 5×5 镜像格子，哈希决定图案与颜色，HSL 取色保证暗色模式可读。
  - 反代提供 `avatar_url` 时优先图片，加载失败回退 identicon。
  - 匿名用户各自不同的头像；同一浏览器 cookie 不变则头像稳定。
- `AvatarStack.svelte`：控制者优先、最多 4 个、其余显示 `+N`；`mine` 排最前。
- 角色边框：
  - 控制中：`ring-2 ring-emerald-500 ring-offset-2 ring-offset-background` + 右下角实心圆点；
  - 控制中且是自己：额外显示「我」角标；
  - 观察者：`ring-1 ring-border` 弱化显示。
- 匿名显示名：`Anonymous`，tooltip / 弹窗使用 `Anonymous · 3f2a`（ID 后 4 位）区分。

### 10.2 导航

`AppHeader.svelte` 右侧增加头像 + 名字（窄屏只显示头像），Tooltip 显示邮箱/登录方式，点击菜单：

- 身份信息；
- 退出登录（仅 `signout_url` 非空）。

### 10.3 设备列表

- 新增「状态」列：空闲 / 我 / 共享 / 观察 / Alice 使用中。
- 占用者头像叠层 + 观察者头像 + 眼睛数量。
- 数据来自 `GET /api/v1/devices` 内嵌的 occupancy，不新增请求。

### 10.4 设备详情页

- `DeviceGuard.svelte`：先 `PUT presence`，拿到 `mine/shared` 后才渲染视频流和操作区；`busy` 弹窗，`viewing` 只读，`preempted` 跳转。
- 心跳：可见时每 30 秒一次，隐藏时停止；卸载 / `pagehide` 用 `fetch keepalive` 调 DELETE。
- 观察者模式：顶部黄色提示条，控制按钮统一禁用；可一键“切换为只读”。
- 被抢占：WS 关闭 + 心跳 409 → toast 并返回列表。

### 10.5 多设备页

- 每路画面标题旁显示头像叠层与角色边框。
- 被占用时显示遮罩（`can_takeover` 时提供抢占按钮），不持续拉流。
- 心跳合并为一次批量 `PUT /api/v1/presence`，按设备记住用户选择的模式：观察中的设备继续用 `view` 续期，其余设备保持 `control`，避免 30 秒心跳把正在观看的画面换回 `busy` 占位；点开设备时会重新申请 `control`。

### 10.6 前端模块

- `lib/stores/identity.svelte.ts`：`/api/v1/me`。
- `lib/api/api.ts`：`getMe`、`putDevicePresence`、`deleteDevicePresence`、`putPresenceBatch`、`getPresence`、`getAccessConfig`、`setAccessConfig`。
- `lib/api/schemas.ts`：枚举数组、`Occupancy`、`Occupant`、`AccessConfig`、`Me`。
- i18n：`en.json` 与 `zh-CN.json` 同步新增 `presence.*`、`access.*`、`me.*`。

## 11. 安全

- 默认不信任任何 `X-Forwarded-*`；只有对端 IP 在 `trusted_proxies` 内才解析身份。
- 建议部署方式：uiautodev 只监听 `127.0.0.1`，由 oauth2-proxy 对外。
- `require_identity=true` 时拒绝匿名的 API / 设备流请求；恢复方式是编辑 `config.json` 后重启。
- `ua_sid` 只用于区分匿名会话，本身不是认证凭证。
- 设置接口受管理会话保护（`managementAuthMiddleware`），桌面版保持现有的免密行为。
- 配置校验失败不覆盖原配置；写入沿用现有 `0600` 权限。

## 12. 文件改动清单

### 后端

| 文件 | 改动 |
|---|---|
| `webserver/identity.go` | 新增：Identity、cookie、代理头解析、`/api/v1/me` |
| `webserver/presence.go` | 新增：PresenceManager、状态机、清扫、kick |
| `webserver/access_config.go` | 新增：配置读写、校验、枚举定义 |
| `webserver/config.go` | `appConfig.Access`、默认值 |
| `webserver/server.go` | 中间件注册、路由、`APIDevice.occupancy`、去掉写死 1h |
| `webserver/manager.go` | 竞态修复、释放回调、presence 开关下的超时策略 |
| `webserver/auth.go` | 复用 username，避免重复解析 |

### 前端

| 文件 | 改动 |
|---|---|
| `lib/components/UserAvatar.svelte` | 新增 |
| `lib/components/AvatarStack.svelte` | 新增 |
| `lib/components/device/DeviceGuard.svelte` | 新增 |
| `lib/components/device/PresenceBadge.svelte` | 新增 |
| `lib/components/settings/AccessSettings.svelte` | 新增 |
| `AppHeader.svelte` | 头像菜单 |
| `routes/+page.svelte` | 状态列 |
| `routes/device/[id]/+page.svelte` | Guard、心跳、观察者、抢占 |
| `routes/multi-device/*` | 批量心跳、占用遮罩 |
| `routes/config/+page.svelte` | 新增设置章节 |
| `lib/api/api.ts`、`schemas.ts` | 新接口与类型 |
| `lib/i18n/locales/{en,zh-CN}.json` | 文案 |

### 文档

- `AGENTS.md`、`Claude.md`：枚举规则（已完成）。
- `README.md`：oauth2-proxy / nginx auth_request 部署示例、`config.json` 引导配置。

## 13. 实施步骤

1. 枚举 + AccessConfig 持久化 + 设置界面。
2. Identity + `ua_sid` + `/api/v1/me` + 导航头像。
3. PresenceManager：control/view 会话、PUT/DELETE 接口、空闲清扫、ReleaseDevice。
4. 设备接口权限矩阵 + `preempt` / `exclusive` + WS 踢出 + preempted 提示。
5. 设备页 DeviceGuard、观察者 UI、列表状态列、多设备页、i18n。
6. README、测试补全、回归检查。

## 14. 测试计划

### 后端

- 身份：
  - 非可信来源的 `X-Forwarded-*` 被忽略；
  - 可信来源正确解析 user/email/groups/avatar；
  - 同一 cookie 匿名身份稳定，不同 cookie 不同身份；
  - 无 cookie 脚本按 IP+UA+Identity 稳定；
  - `require_identity=true` 时匿名 401，MCP token 与静态资源不受影响；
- 占用：
  - 同一身份多会话 `shared`；
  - 不同匿名浏览器冲突；
  - `preempt` 抢占只驱逐控制会话，观察者保留；
  - `exclusive` 下无法抢占；
  - 409 preempted 记录 2 分钟后清理。
- 权限：
  - 观察者读接口通过；
  - 观察者调用 tap/swipe/shell/connect/proxy 全部 403；
  - `allow_observers=false` 时读接口 423。
- 生命周期：
  - 空闲 10 分钟释放，`ReleaseDevice` 只触发一次；
  - 显式离开 30 秒宽限，期间重连取消释放；
  - `presence.enabled=false` 时保持旧行为。
- 配置：
  - 非法 CIDR / 非法枚举返回 400；
  - 保存后立即生效且写入 `config.json`；
  - 手写配置启动加载成功；
  - 损坏配置回退默认值并记录警告。

### 前端

- `bun run --cwd frontend check`。
- 头像：不同 ID 不同图案、同 ID 稳定、控制者绿框、观察者弱化、`mine` 角标、超过 4 人显示 `+N`。
- 设备页：busy 弹窗、观察者只读、抢占后跳转、隐藏标签页停止心跳。
- 多设备页：批量心跳只发一次请求。

## 15. 默认值与后续可选项

已确定默认值：

- `conflict_policy = preempt`
- `idle_timeout = 10m`
- `allow_observers = true`
- `presence.enabled = true`
- 释放宽限 30 秒
- 不做排队

后续可考虑（本次不做）：

- 观察者限制人数；
- 抢占历史审计日志；
- 基于组/角色的权限（例如只允许管理员抢占）；
- 通知渠道接入（邮件 / IM）。

## 16. 实现备注

- 所有新接口沿用现有 `{"code","message","data"}` 响应包裹，`data` 内才是本文档描述的结构。
- `/api/v1/devices/:id/meta` 属于被动请求：只在已有会话时续期，不会因为修改备注而占用设备。
- 浏览器首次导航（`Accept: text/html`）会生成 32 位随机 `ua_sid` 并写入 cookie；无 cookie 的脚本请求继续按 `IP + User-Agent` 哈希兜底，保证同一脚本身份稳定。
- 空闲超时直接释放设备；只有显式 `DELETE .../presence` 才走 30 秒宽限期。
- MCP 使用同一个 `mcp_token` 时身份为 `mcp:admin`（显示名 `MCP`），所有 MCP 客户端共享该身份；新增 `acquire_device` / `release_device` 工具，其他设备工具在读/控制两类上自动申请会话，语义与 HTTP 一致。
