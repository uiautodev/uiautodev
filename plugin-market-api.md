# UIAutoDev 插件市场 API

本文档定义插件市场（Plugin Market）的接口契约，分为两层：

- **A. Registry API**：自建插件仓库服务对外提供的接口，App 作为客户端消费。
- **B. App Market API**：App 后端向自身前端暴露的市场接口（前端不直连 Registry）。

Registry 由独立仓库实现，本文档只约束接口，不约束实现。

## 通用约定

- 传输：HTTPS（回环地址允许 HTTP）。
- 编码：UTF-8，JSON 字段使用 `snake_case`。
- 响应信封（与 App 现有 `APIResponse` 一致）：

```json
{ "code": 0, "message": "ok", "data": {} }
```

- `code = 0` 表示成功，非 0 为业务错误，`message` 为可展示的错误信息。
- HTTP 状态码为准，必须与 `code` 保持一致：
  - `200 OK`：读取、更新成功。
  - `201 Created`：创建成功，必须携带 `Location` 头指向新资源。
  - `204 No Content`：删除成功，响应体为空（不套用信封）。
  - `400`：参数错误；`401/403`：鉴权失败；`404`：未找到；`409`：资源冲突；`500`：服务错误。
- 时间戳统一 RFC3339（如 `2026-09-16T10:00:00Z`）。
- 版本号遵循 semver（`MAJOR.MINOR.PATCH`）。
- 资源路径以 `/api/v1/market` 为市场根：
  - `/api/v1/market/plugins`（插件集合）
  - `/api/v1/market/plugins/{id}/versions`（版本子集合）
  - `/api/v1/market/tags`（标签集合）
  - `/api/v1/market/source`（市场源单例资源）

  与插件管理接口 `/api/v1/plugins/:id`、静态资源 `/plugins/:id/*` 完全隔离，避免路由歧义。

### ID 规则

- 插件全局唯一 ID 为 `id`，**必须等于插件 `plugin.json` 中的 `id`**。
- App 安装时以 `id` 作为插件目录名，运行时 ID 即目录名，三者必须一致。
- 允许字符：`[a-z0-9_-]` 开头，其余为 `[a-z0-9._-]`；不允许以 `.` 开头（点前缀目录保留给
  内部使用，如安装备份，且不会被插件扫描加载），建议反向域名风格（如 `com.example.foo`）。
- Registry 与 App 都要校验该字符集：App 安装时若 `id` 含非法字符必须直接拒绝
  （返回 `400`），不得静默改名（`sanitizePluginDirName` 仅作为兜底，不改变校验语义）。

---

## A. Registry API

基础路径：`{base}/api/v1/market`

### A1. 插件列表 / 搜索

```
GET {base}/api/v1/market/plugins
```

查询参数：

| 参数 | 类型 | 说明 |
|---|---|---|
| `q` | string? | 关键词，匹配 id/name/description |
| `tag` | string? | 按标签过滤 |
| `platform` | string? | 按平台过滤（`android` / `ios` / `harmony`） |
| `author` | string? | 按作者过滤 |
| `sort` | string? | `updated`(默认) / `downloads` / `name` |
| `page` | int? | 页码，从 1 开始，默认 1 |
| `page_size` | int? | 每页数量，默认 20，上限 100 |

响应 `data`：

```json
{
  "total": 42,
  "page": 1,
  "page_size": 20,
  "items": [ PluginSummary ]
}
```

`PluginSummary`：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | string | 全局唯一 ID |
| `name` | string | 显示名称 |
| `author` | string | 作者 |
| `description` | string | 描述 |
| `icon_url` | string? | 图标 URL |
| `homepage` | string? | 主页 |
| `repository` | string? | 源码仓库 |
| `license` | string? | 许可证 |
| `tags` | string[] | 标签 |
| `platforms` | string[] | 支持平台，空表示全平台 |
| `latest_version` | string | 最新版本号 |
| `downloads` | int | 累计下载次数 |
| `updated_at` | string | 最近更新时间 |

### A2. 插件详情

```
GET {base}/api/v1/market/plugins/{id}
```

响应 `data` 为 `PluginSummary` 全部字段，外加内联的 `versions`（版本子集合的展开形式，
规范入口见 A3 / A4）：

```json
{
  "...": "PluginSummary 字段",
  "versions": [ PluginVersion ]
}
```

`PluginVersion`：

| 字段 | 类型 | 说明 |
|---|---|---|
| `version` | string | 版本号 |
| `sha256` | string | zip 的 SHA-256（十六进制小写） |
| `size` | int | zip 字节数 |
| `min_app_version` | string? | 所需最低 App 版本 |
| `changelog` | string? | 更新日志 |
| `published_at` | string | 发布时间 |
| `download_url` | string | 下载地址（绝对 URL） |

`versions` 按版本号从新到旧排列。

### A3. 版本列表

```
GET {base}/api/v1/market/plugins/{id}/versions
```

响应 `data`：

```json
{ "total": 3, "items": [ PluginVersion ] }
```

`items` 按版本号从新到旧排列。

### A4. 版本详情

```
GET {base}/api/v1/market/plugins/{id}/versions/{version}
```

响应 `data` 为单个 `PluginVersion`；`{id}` 或 `{version}` 不存在时返回 `404`。

### A5. 下载插件包

```
GET {base}/api/v1/market/plugins/{id}/versions/{version}/download
```

- `{version}` 为 semver 或字面量 `latest`（返回最新版本）。
- 成功返回 `200`，`Content-Type: application/zip`，响应体为 zip 字节流。

响应头：

| 头 | 说明 |
|---|---|
| `Content-Length` | zip 字节数，用于校验 |
| `X-Checksum-Sha256` | zip 的 SHA-256，客户端须与详情中的 `sha256` 比对 |
| `Content-Disposition` | 建议为 `attachment; filename="{id}-{version}.zip"` |

### A6. 标签列表（用于前端筛选）

```
GET {base}/api/v1/market/tags
```

响应 `data`：

```json
{ "tags": [ { "name": "automation", "count": 12 } ] }
```

### A7. 健康检查

```
GET {base}/healthz
```

返回 `200` 与 `{"status":"ok"}`（不套用业务信封）。

### A8. 发布版本（管理端）

```
POST {base}/api/v1/market/plugins/{id}/versions
Authorization: Bearer {admin_token}
Content-Type: multipart/form-data
```

| 表单字段 | 类型 | 说明 |
|---|---|---|
| `file` | file | 插件 zip，根目录须含 `plugin.json` |
| `changelog` | string? | 该版本更新日志 |

服务端职责：解析 `plugin.json`、校验其 `id` 与路径 `{id}` 一致、校验 `id` 合法性与
zip-slip、计算 `sha256`、写入存储。插件记录在首个版本发布时隐式创建。

- 成功返回 `201 Created`，`Location: {base}/api/v1/market/plugins/{id}/versions/{version}`。
- 已存在的 `id+version` 默认返回 `409 Conflict`，除非显式允许覆盖。

响应 `data`：`{ "id": "...", "version": "...", "sha256": "..." }`

### A9. 下架版本（管理端）

```
DELETE {base}/api/v1/market/plugins/{id}/versions/{version}
Authorization: Bearer {admin_token}
```

- 成功返回 `204 No Content`，无响应体。
- `{id}` 或 `{version}` 不存在时返回 `404`。

### A10. 错误响应示例

```json
{ "code": 404, "message": "plugin not found" }
```

`APIResponse.Data` 带 `omitempty`，失败时 `data` 字段会被省略（不是 `null`）。

---

## B. App Market API

基础路径：`/api/v1/market`

除标注外均需管理鉴权（`managementAuthMiddleware`）。

### B1. 市场列表

```
GET /api/v1/market/plugins?q=&tag=&platform=&page=&page_size=
```

- 透传 A1 的查询参数。
- 不缓存，每次请求都实时回源 Registry。
- 后端将 Registry 结果与本地已安装插件合并。

响应 `data`：

```json
{
  "source": "https://get.uiauto.dev",
  "total": 42,
  "page": 1,
  "page_size": 20,
  "items": [ MarketItem ]
}
```

`MarketItem` = `PluginSummary` 全部字段，外加：

| 字段 | 类型 | 说明 |
|---|---|---|
| `installed` | bool | 是否已安装 |
| `installed_version` | string? | 已安装版本，未安装时为空 |
| `update_available` | bool | 是否有可用更新 |

### B2. 一键安装

```
POST /api/v1/market/plugins/{id}/install
Authorization: Bearer {token}
Content-Type: application/json
```

请求体：

```json
{ "version": "1.2.0" }
```

`version` 省略时安装最新版本。后端职责：

1. 请求 A4 获取该版本 `download_url`、`sha256` 与 `min_app_version`。
2. 通过 `download_url` 下载 zip 到临时文件。
3. 校验大小（上限 100 MB）与 SHA-256。
4. 校验 `min_app_version` 不高于当前 App 版本，否则返回 `400`（不能只靠前端拦截）。
5. 复用 `extractPluginZip` 解压到临时目录，校验 `plugin.json` 合法（`id` 与路径 `{id}`
   一致、字符集符合 `[a-z0-9._-]`）后，再原子替换到 `<config-dir>/plugins/{id}`；
   解压或校验任一步失败都必须保留原有已安装版本，不得先删旧目录。
6. 记录安装来源与版本到 `config.json` 的 `plugins[<id>]`（`PluginRecord`）。
7. `LoadAll` 重新加载插件。

- 成功返回 `201 Created`，`Location: /api/v1/plugins/{id}`（指向已安装的插件资源）。
- 目标版本已安装且无需变更时返回 `200`。

响应 `data`：`{ "name": "Foo", "id": "com.example.foo", "version": "1.2.0" }`

### B3. 读取市场源

```
GET /api/v1/market/source
```

响应 `data`：

```json
{
  "url": "https://get.uiauto.dev",
  "default_url": "https://get.uiauto.dev",
  "is_custom": false
}
```

- `is_custom`：当前 `url` 是否与 `default_url` 不同。

### B4. 设置市场源

```
PUT /api/v1/market/source
Authorization: Bearer {token}
Content-Type: application/json
```

请求体：`{ "url": "https://my-mirror.example.com" }`

校验规则（按顺序）：

| # | 条件 | 结果 |
|---|---|---|
| 1 | `url` 为空 | 恢复默认源，允许 |
| 2 | `url == default_url` | 允许 |
| 3 | 非回环且非 `https` | `400` `custom plugin source must use https` |
| 4 | 其余 | 允许并持久化 |

回环地址（`localhost` / `127.0.0.0/8` / `::1`）允许使用 `http`。

响应 `data`：同 B3。

---

## 安全要求

- Registry 读接口公开，发布接口使用单一 admin token。
- App 侧配置的市场源必须 HTTPS（回环除外）；下载包强制校验 `sha256` 与大小上限。
- Registry 是信任根，索引/包被篡改等同于任意代码执行；签名校验列为后续增强项。
- `min_app_version` 高于当前 App 版本时，App 后端拒绝安装并返回 `400`；前端不额外拦截。

## 版本与更新语义

- `latest_version` 为 Registry 中最高 semver 版本。
- `update_available` 在 `installed_version < latest_version` 时为 `true`。
- 安装目录名与运行时 ID 恒等于 `id`；更换 `id` 视为新插件，不覆盖旧插件。

## 实现要点

- `ServerConfig` 增加 `Audience` 字段（Wails 桌面端为 `desktop`，独立服务端默认
  `server`）。
- `config.go`：`appConfig` 增加 `PluginMarketSource string`，并以
  `Plugins map[string]PluginRecord` 统一保存每个插件的启用状态与安装信息
  （`enabled` / `version` / `source` / `sha256` / `installed_at`）。`PluginRecord`
  兼容旧配置里的 `plugins` bool 写法；市场安装与本地上传 zip 都写入该记录，卸载时整条删除。
- 新增 `webserver/plugin_market.go`：Registry 客户端（list / versions / download）+
  App 端 handler（list / install / source），并提供 `resolveMarketSource()` 解析市场源；App 在 `server.go` 以 `/api/v1/market` 为根注册
  `GET /plugins`、`POST /plugins/:id/install`、`GET|PUT /source`（均需管理鉴权）。
- 市场的展示元数据（`author` / `tags` / `license` 等）全部来自 Registry 的
  `PluginSummary`，不写入本地 `plugin.json`；`PluginManifest` 字段保持不变。
- 前端 `api.ts` 增加市场函数（列表 / 安装 / 源读写）；`PluginMarketSettings.svelte` 市场
  区块负责源读写；i18n 中英文件同步新增 key。
