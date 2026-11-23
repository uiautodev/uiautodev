# AGENTS.md

有几个要注意的事情

1. 修改完go的代码后，需要运行单测保证测试全部通过
2. 添加go依赖后，需要运行 go mod tidy，避免依赖被标记为 indirect
- 代码要放到合适的位置，不要一股脑的塞到一个文件中
- 代码注释一律用英文书写，包括行注释、包注释和文档注释；不要把中文写在注释里


# 前端相关

- 如果修改了前端的代码，一定要 bun run --cwd frontend check 一下，测试也要运行一下
- svelte相关的操作尽量使用svelte mcp，还不清楚的就先网上查看一下
- 优先使用bun，而不是npm，bun指令需要在frontend目录下执行
- 跟插件相关的文件 plugin.md plugin.go Plugin.svelte plugin_runtime.js plugin_runtime.d.ts 关联度很高，修改其中一个文件的时候，需要同时查看其他文件是否需要修改
- 如果代码表达的很清楚了，就不用写多余的注释了
- 涉及到前端请求后端的，优先封装一个函数到api.ts中
- 前端界面的文案要支持多语言：新增或修改可展示文本时，用 `$t("key")` 替换硬编码常量，并在 `frontend/src/lib/i18n/locales/zh-CN.json` 与 `en.json` 中同步新增对应的 key（两个文件都必须加，缺一不可）
- 样式要支持主题自适应（暗黑模式）：用 shadcn token 类（text-foreground、text-muted-foreground、bg-background、bg-card、bg-muted、border-border 等），不要写死浅色（如 text-gray-900、bg-white、text-gray-500）；写死的浅色样式在暗黑模式下会失效（文字隐形/背景仍是白色）
- 所有涉及网页界面的修改，都必须用 Playwright 实际打开页面验证：启动本地服务后走一遍主要交互，并检查暗黑模式和窄屏；不能只依赖 `bun run check` 或构建通过

# 后端 / 接口设计

- 接口要符合 RESTful 风格：用资源路径 + HTTP 方法表达操作（GET 读取 / POST 创建 / PUT 覆盖 / PATCH 局部更新 / DELETE 删除），不要把动作或资源标识塞进 query 参数或请求体里做非标准用法。
- 数据结构设计要考虑后续扩展：优先用对象/嵌套结构而不是扁平值（例如 `{deviceId: {remark: "..."}}` 而非 `{deviceId: "..."}`），后续新增字段时无需破坏兼容。

# 类型与取值

- 取值只有几种的字段一律使用枚举，禁止使用散落的字符串字面量（magic string）。
- Go：定义 `type Xxx string` + `const` 常量 + `Valid()` 校验；持久化、API 和比较都使用该类型，不要到处重复写 `"preempt"` 这类字面量。
- TypeScript：`export const XXX_LIST = [...] as const` + `export type Xxx = (typeof XXX_LIST)[number]`；下拉框选项直接从该数组生成，不要另写一份。
- 前后端的枚举取值必须保持一致；新增、删除或重命名取值时，两边同步修改，并更新设置界面文案、i18n 与相关文档。
- 示例（设备占用相关）：`ConflictPolicy`（preempt | exclusive）、`DeviceAccessMode`（control | view）、`IdleTimeout`（1m | 5m | 10m | 30m | 1h）、`OccupancyState`（free | mine | shared | viewing | busy | preempted）。

# 其他

- 通过命令 kill $(lsof -i:33299 -t) 可以快速的释放33299端口
