---
name: device-control
description: "通过 uiautodev MCP 控制移动设备（Android / iOS / 华为鸿蒙 Harmony）的 UI。当用户要求点击界面元素、截图、滑动、输入文字、按物理键、切换语言/设置等真机操作时触发。核心：以截图为准，tap/swipe 必须带上截图的宽高，坐标即可自动缩放。"
---

# 设备 UI 控制（uiautodev MCP）

用 uiautodev MCP 控制真机（Android / iOS / 华为鸿蒙，平台无关，方法完全一致）。**首选的信息源是截图；核心交互是 tap/swipe/input_text/press_key（dump_xml / find_elements_by_xpath 亦可辅助定位）。**

## 核心原则：坐标必须传截图尺寸

模型拿到的截图可能被缩放，其宽高 ≠ 设备真实分辨率。所以 `tap`/`swipe` 必须传 `width`/`height` = **模型当前截图的实际宽高**，`x`/`y` 直接在该截图上读取。系统按比例自动缩放到真实屏幕，坐标为同一空间、点就准。

```
1. screenshot → 得到截图，记住宽高（例 621×1351）
2. 在截图上读目标元素像素坐标 (x, y)
3. tap(x, y, width=截图宽, height=截图高)   → 系统自动缩放
```

**铁律**：
- 不传 `width`/`height` 时按设备原始像素算，截图与设备分辨率不一致必然点不中。
- 每 tap/swipe 一次，必须再 screenshot 一次确认结果，没跳转就重新截图读数，别原坐标重试。

## 工具速览

| 工具 | 用途 |
|---|---|
| `list_devices` | 列出已连接设备（Android/iOS/鸿蒙） |
| `screenshot` | 截图，首选信息来源 |
| `tap` / `swipe` | 点击 / 滑动（传截图宽高） |
| `input_text` / `clear_text` | 输入 / 清空文本 |
| `press_key` | 物理键（back/home/enter...） |
| `get_device_info` / `get_window_size` | 设备信息（一般不需要窗口尺寸） |
| `dump_xml` / `find_elements_by_xpath` | UI 层级（辅助） |

## 标准流程（每步必截图确认）

```
1. list_devices          → 找设备 id
2. screenshot             → 看界面 + 记截图宽高
3. tap(x, y, width=宽, height=高) → 点击
4. screenshot             → 确认真的跳转了
```

## 示例：切换系统语言为中文

```
Settings → Language & input → Language and region → 简体中文 → 截图确认
```

1. `screen shot` 后界面宽高为 621×1351。
2. 目标「Language and region」一行位于截图 (310, 680)。
3. `tap(310, 680, width=621, height=1351)`。
4. 再次截图：界面已变中文，成功。

## 注意事项

- 卸载 / 恢复出厂等不可逆操作，先向用户确认。
- `swipe` 示例：`swipe(x1, y1, x2, y2, width=621, height=1351)` 滑过某项；按返回后先截图再继续。
