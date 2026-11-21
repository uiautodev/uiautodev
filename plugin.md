# UIAutoDev Plugin 开发指南

## 目录结构

```
plugins/
  my-plugin/
    plugin.json    # 插件清单
    index.html     # 入口页面
    ...            # 其他静态资源
```

默认从以下目录加载插件（按优先级从高到低，同名插件高优先级覆盖低优先级）：

- 用户目录：`<config-dir>/plugins`（默认为 `~/.config/uiautodev/plugins`，可卸载；可用 `uiautodev-server -config-dir <dir>` 整体切换配置目录，或用 `-plugin-dir <dir>` 单独覆盖插件目录，部署多个 server 时用不同目录隔离插件）
- 二进制所在的`plugins/`目录（内置插件，仅可禁用，不可卸载）

## 官方插件

插件 ID（即插件目录名）以 `uiautodev-` 开头的会被识别为官方插件，在「设置 → 插件」列表中显示「官方」徽标，并可用「全部 / 官方 / 第三方」过滤。该前缀由前端 `frontend/src/lib/plugin.ts` 的 `OFFICIAL_PLUGIN_PREFIX` 定义。内置插件单独标记为「内置」，不算官方。

## plugin.json

```jsonc
{
  "$schema": "http://localhost:33299/plugin.schema.json",
  "id": "my-plugin",            // 可选，插件唯一ID；zip安装时按 id → zip顶层插件目录名 → name 的顺序决定目录名，同名覆盖安装
  "name": "My Plugin",          // 可选，默认目录名称
  "version": "1.0.0",           // 可选，版本号
  "description": "插件描述",     // 可选，在卡片中显示
  "homepage": "https://...",    // 可选，插件主页链接
  "main": "index.html",         // 可选，入口文件，默认 index.html
  "height": 0,                  // 可选，iframe 初始高度（px）
  "platforms": ["android"],     // 可选，支持的平台，空数组或不填表示全平台
}
```

插件会出现在设备页面的「插件」标签页中，以 iframe 形式加载。

## $u API 参考

插件 iframe 中可通过全局对象 `$u` 调用宿主能力。所有方法均为异步，返回 Promise。

### 属性

| 属性 | 类型 | 说明 |
|------|------|------|
| `$u.deviceId` | `string` | 当前连接设备的 ID |
| `$u.plugin` | `object` | 当前插件信息（来自 plugin.json） |
| `$u.plugin.id` | `string` | 插件唯一 ID |
| `$u.plugin.name` | `string` | 插件名称 |
| `$u.plugin.version` | `string` | 插件版本 |
| `$u.plugin.description` | `string` | 插件描述 |

### 方法

#### `$u.shell(cmd): Promise<{ output: string }>`

在设备上执行 shell 命令。`cmd` 可以是字符串，或 `{ command, timeoutMs? }` 对象，单位毫秒，默认 30000（30s）。

```js
// 简单形式
const result = await $u.shell("pm list packages");
console.log(result.output);

// 带超时
const result = await $u.shell({ command: "sleep 60", timeoutMs: 120000 });
```

#### `$u.screenshotAsBase64(): Promise<string>`

截取设备屏幕，返回 `data:image/png;base64,...` 字符串，适合持久化或传输。

```js
const base64 = await $u.screenshotAsBase64();
localStorage.setItem("last-screenshot", base64);
```

#### `$u.setScreenImage(image: Blob | null): Promise<void>`

将指定图片显示为 screen 上的静止画面（覆盖在实时画面上，作用同 Freeze 冻结）。传入 `Blob` 对象；传 `null` 清除静止画面。

```js
// 上传本地图片作为静止画面
const input = document.createElement("input");
input.type = "file";
input.accept = "image/*";
input.onchange = async () => {
  const file = input.files[0];
  await $u.setScreenImage(file);
};
input.click();

// 清除静止画面
await $u.setScreenImage(null);
```

#### `$u.inputText(text: string): Promise<void>`

向设备当前焦点输入文本。

```js
await $u.inputText("hello world");
```

#### `$u.pressKey(key: string, metaState?: number): Promise<void>`

按下设备按键。`key` 支持 `home`、`back`、`enter`、`tab`、`volume_up` 等；`metaState` 为组合键修饰符位掩码（可选）。

```js
await $u.pressKey("enter");
await $u.pressKey("a", 0x1000); // Ctrl + A
```

#### `$u.tap(x, y, screenWidth, screenHeight): Promise<void>`

在设备屏幕上执行一次点击。`x`/`y` 与 `screenWidth`/`screenHeight` 使用同一坐标系（即视频坐标系，通常等于设备分辨率；开启裁剪时为裁剪后尺寸）。后端据此按比例缩放到真实设备分辨率。与 `$u.onScreenTap` 回调返回的字段一致，可直接用于回放。

```js
await $u.tap(540, 1200, 1080, 2400);
```

#### `$u.appList(scope?): Promise<string[]>`

列出设备上已安装的应用 ID。`scope` 取值：

| 取值 | 说明 |
|------|------|
| `user` | 用户应用（默认） |
| `system` | 系统应用 |
| `all` | 全部应用 |

返回 app ID 数组（Android 包名 / iOS bundle ID / HarmonyOS bundle name）。

```js
const apps = await $u.appList("user");
const all = await $u.appList("all");
```

#### `$u.appStart(appId, activity?): Promise<{ app_id, activity? }>`

启动应用。Android 未传 `activity` 时会自动解析启动入口，解析失败直接报错；iOS/HarmonyOS 会自行解析。返回实际启动的应用信息。

```js
await $u.appStart("com.example.app");
await $u.appStart("com.example.app", "com.example.app.MainActivity");
```

#### `$u.appStop(appId): Promise<void>`

停止应用。

```js
await $u.appStop("com.example.app");
```

#### `$u.appCurrent(): Promise<{ app_id, activity? }>`

获取当前前台应用（Android/HarmonyOS 同时返回 activity/ability）。

```js
const current = await $u.appCurrent();
console.log(current.app_id);
```

#### `$u.onScreenTap(callback): () => void`

订阅屏幕模式为 `recording` 时捕获的点击事件（通过 `$u.setMode("recording")` 开启录制）。该事件不会直接操作设备，而是交给插件，可用于实现操作录制。返回取消订阅函数；请在不再需要时调用它，避免监听器持续累积。

事件对象：

| 字段 | 类型 | 说明 |
|------|------|------|
| `x` | `number` | 点击位置 X（视频坐标系，与 `screenWidth` 对应） |
| `y` | `number` | 点击位置 Y（视频坐标系，与 `screenHeight` 对应） |
| `screenWidth` | `number` | 采集时的视频宽度（设备分辨率，开启裁剪时为裁剪后宽度） |
| `screenHeight` | `number` | 采集时的视频高度（设备分辨率，开启裁剪时为裁剪后高度） |
| `timestamp` | `number` | 事件时间戳（`Date.now()`） |

```js
const taps = [];
const unsubscribe = $u.onScreenTap((e) => {
  taps.push(e);
  console.log("tap", e.x, e.y);
});

// 回放录制结果，按时间戳差值还原点击节奏（taps 按发生顺序排列）
async function replay() {
  let prev = taps[0]?.timestamp ?? 0;
  for (const e of taps) {
    await new Promise((r) => setTimeout(r, e.timestamp - prev));
    prev = e.timestamp;
    await $u.tap(e.x, e.y, e.screenWidth, e.screenHeight);
  }
}

// 结束时取消订阅
// unsubscribe();
```

#### `$u.setMode(mode: "control" | "readonly" | "recording"): Promise<void>`

设置当前设备的屏幕模式：

- `control`：控制模式（默认），触摸/按键可操作设备。
- `readonly`：只读模式，禁用触摸/按键注入，仅查看画面。
- `recording`：录制模式，点击会被捕获为 `onScreenTap` 事件，不操作设备；屏幕边框与光标变红。

```js
await $u.setMode("recording"); // 开始录制
await $u.setMode("readonly");   // 只读查看
await $u.setMode("control");    // 恢复控制
```

#### `$u.onModeChange(callback): () => void`

订阅屏幕模式变化。`callback` 接收当前模式（`"control"` / `"readonly"` / `"recording"`）。当用户点击屏幕顶部状态图标切换或结束模式时，插件可通过此事件收到通知以完成收尾（例如录制结束后保存已录制的操作序列）。返回取消订阅函数。

```js
const unsubscribe = $u.onModeChange((mode) => {
  if (mode !== "recording") {
    console.log("录制已结束");
    save(taps);
  }
});
```

#### `$u.openai(params): Promise<OpenAIChatCompletion | AsyncIterable<OpenAIChatCompletionChunk>>`

调用「设置 → OpenAI 配置」中已配置的 OpenAI 兼容接口。入参与 OpenAI SDK 的 `client.chat.completions.create()` 一致：`messages` 必填，`model` 缺省时使用已配置的模型。密钥由宿主注入，插件无需也无法接触 api_key。

```js
// 非流式：直接返回完整回复
const res = await $u.openai({
  model: "gpt-4o", // 可选，缺省用配置的模型
  messages: [{ role: "user", content: "hello" }],
});
console.log(res.choices[0].message.content);

// 流式：返回异步可迭代的 chunk，逐块消费
const stream = await $u.openai({
  messages: [{ role: "user", content: "写出1到5" }],
  stream: true,
});
for await (const chunk of stream) {
  const delta = chunk.choices?.[0]?.delta?.content || "";
  process.stdout.write(delta); // 逐步打印
}
```

> 说明：`stream:true` 时返回的是可被 `for await` 迭代的流（SSE 解析后的 chunk 对象），使用方法与 OpenAI SDK 相同。

#### `$u.jev(params): Promise<JevDecision>`

调用「设置 → Jev 配置」中已配置的 Jev（TypeSafe System One）决策接口。入参为决策请求体：`state`（待判断的材料）、`questions`（按问题 id 组织的类型化问题集合），`model` 缺省时使用已配置的模型。密钥由宿主注入，插件无需也无法接触 api_key。

返回上游原始响应，可直接读取 `res.answers[questionId]` 拿到类型化决策（`noul` 返回 0~1 的概率，`choice` 返回选中项与各选项概率，`score` 返回分值与分布）。Jev 不支持流式。

```js
const res = await $u.jev({
  model: "jev-latest", // 可选，缺省用配置的模型
  state: "生产环境接口开始返回 500，订单无法处理。",
  questions: {
    needs_human: {
      type: "noul",
      instructions: "这条工单是否需要人工回复？",
    },
    queue: {
      type: "choice",
      instructions: "应转给哪个团队处理？",
      criteria: {
        billing: "发票、退款、重复扣费、套餐变更、税务。",
        technical: "接口报错、集成、故障、SDK。",
      },
    },
  },
});

if (res.answers.needs_human.noul > 0.8) {
  console.log("需要人工介入");
}
console.log(res.answers.queue.choice, res.answers.queue.confidence);
```

## 自动高度

插件 iframe 会自动根据内容高度调整大小，无需手动设置。使用 ResizeObserver 监听 `document.body` 变化。

## TypeScript 支持

在你的 JS/TS 项目中添加类型引用即可获得自动补全：

```ts
/// <reference types="http://localhost:33299/plugin-runtime.d.ts" />
```

## 完整示例

```html
<!DOCTYPE html>
<html>
<head><meta charset="utf-8"></head>
<body>
  <h1 id="name"></h1>
  <button onclick="run()">列出包名</button>
  <pre id="output"></pre>

  <script>
    document.getElementById("name").textContent = $u.plugin.name;

    async function run() {
      const result = await $u.shell("pm list packages -3");
      document.getElementById("output").textContent = result.output;
    }
  </script>
</body>
</html>
```
