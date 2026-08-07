## 简介

uiautodev 是一款集 UI 自动化、设备管理和 AI 工具于一体的桌面应用，旨在提升开发和测试效率。

![uiautodev](images/uiautodev.jpg)

## 快速开始

无需安装，复制下面的命令即可运行（首次会自动下载对应平台的服务端二进制）：

```bash
npx uiautodev
```

启动后服务端默认监听 `http://127.0.0.1:33299`。

## CLI

| 参数 | 说明 |
|---|---|
| `--version <v>` | 指定版本（默认最新） |
| `--force` | 忽略缓存，强制重新下载 |
| `--install-only` | 只下载，打印二进制路径 |
| `--help` | 显示帮助 |
| `-- <args>` | 分隔符，之后参数原样透传给服务端 |

二进制缓存于 `~/.cache/uiautodev/<version>/`，可用环境变量 `UIAUTODEV_CACHE_DIR` 覆盖。

```bash
npx uiautodev -- -addr :8000    # 透传参数给服务端（监听 :8000）
npx uiautodev --version 0.6.0   # 指定版本
npx uiautodev --install-only    # 只下载，打印二进制路径
npx uiautodev --force           # 忽略缓存，强制重新下载
```

## 下载

编译后的安装包可从以下地址下载：

[https://get.uiauto.dev](https://get.uiauto.dev)

## 文档

https://www.yuque.com/codeskyblue/uiautodev

## 反馈

如有问题或功能需求，请在 [Issues](https://github.com/uiautodev/uiautodev/issues) 中提交。

## 开源说明

本项目为闭源开发，源码未托管于此仓库。但项目所依赖的多个核心库已开源，[开源地址](https://github.com/uiautodev) 欢迎关注。

- [dictlog](https://github.com/uiautodev/dictlog) 结构化的python日志库，兼容标准库logging
- [uiautoagent](https://github.com/uiautodev/uiautoagent) 使用ai控制手机完成任务
