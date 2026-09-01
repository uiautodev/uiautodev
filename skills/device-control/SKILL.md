---
name: device-control
description: "通过 uiautodev MCP 控制移动设备（Android/iOS）的 UI。当用户要求点击界面元素、截图、滑动、输入文字、按物理键、切换语言/设置等真机操作时触发。核心：以截图为准，tap 用截图尺寸作坐标空间自动缩放。"
---

# 设备 UI 控制（uiautodev MCP）

用 uiautodev MCP 控制真机。**唯一的信息源是截图，唯一的操作是 tap/swipe/input_text/press_key。**

## 工具速览

| 工具 | 用途 |
|---|---|
| `list_devices` | 列出所有已连接设备（按平台分组） |
| `get_device_info` | 设备信息（model/serial/state），确认可用 |
| `get_window_size` | 获取设备真实分辨率（**一般不需要**） |
| `screenshot` | 截图，**唯一的信息来源** |
| `tap` | 点击坐标 |
| `swipe` | 滑动 |
| `input_text` / `clear_text` | 输入/清空文本 |
| `press_key` | 物理键（back/home/enter/...） |
| `dump_xml` / `find_elements_by_xpath` | UI 层级（辅助，非首选） |

## ✅ 核心：坐标不用换算，直接用截图尺寸

`tap`/`swipe` 的 `width`、`height` 是**参考坐标空间的尺寸**。当传入 `width`/`height` 时，`x`/`y` 在该空间内取值，系统会自动按比例缩放到设备真实屏幕。

所以**不用先 `get_window_size`，不用比例换算**：

```
1. screenshot  → 拿到截图，记住它的实际宽高（如 621×1351）
2. 在截图上读目标元素的像素坐标 (x, y)
3. tap(x, y, width=截图宽度, height=截图高度)  → 系统自动缩放
```

`x`/`y` 直接取截图上的像素点即可，`width`/`height` 填截图的尺寸。坐标空间对上了，点就准。

- 横向点**文字标签处**（约 40% 宽度），不是右侧箭头。
- 竖向点**行的中心比例**处。

## 标准流程（每步必截图确认）

```
1. list_devices          → 找到设备 id
2. screenshot             → 看当前界面，定位目标元素 + 记截图尺寸
3. tap(x, y, w=截图宽, h=截图高)   → 点击
4. 再次 screenshot        → 确认真的跳转了（别想当然）
```

**铁律：每 tap 一次，必须再截图一次确认结果。** 没跳转就重新截图定位，别原坐标重试。

## 导航

- `press_key` 按 `back` 返回上级、`home` 回桌面、`enter` 确认、`tab` 切换焦点。
- 返回后**先截图**再继续（退回的位置可能和上次不同）。

## 常见任务示例

### 进入某设置项（如「关于手机」/「语言和地区」）
```
tap(系统设置入口 → 截图确认 → 定位目标行 → tap → 截图确认进入)
```
靠右上角标题判断当前页，再按截图读该行坐标。

### 切换系统语言为中文
```
Settings → Language & input → Language and region → 点「简体中文/Simplified Chinese」→ 截图确认
```
切换后界面立即变中文，以此作为成功信号。

## 注意事项

- **禁用盲目试坐标**：同一处点不中，重新截图、重新读数，不要原坐标重试。
- `tap` 不传 `width`/`height` 时坐标是设备原始像素——截图尺寸和设备分辨率不一致会导致点不中，**务必带上截图的宽高**。
- 卸载/恢复出厂等不可逆操作，先向用户确认。
