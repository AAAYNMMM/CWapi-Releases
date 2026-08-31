# CWapi 版本选择指南

[English](VERSION_GUIDE.md) | [简体中文](VERSION_GUIDE.zh-CN.md)

CWapi 1.6.x 和 2.x 的通信、配置与工作流差异已经很大，所以现在按两条独立发行路线维护，不应该再把它们当成“升级后按钮换了个位置”的同一个版本。

| 路线 | 通信方式 | 主要用途 |
| --- | --- | --- |
| **2.x** | MCP + OpenAI Secure MCP Tunnel | 当前 Coding MCP 与本地 OpenAI-compatible provider 架构，详见 `main` |
| **1.6.x** | GitHub + Slack Socket Mode | 旧版 Web GPT 本地开发工作流 |

## 什么时候继续用 1.6.x

- 你已经把 GitHub + Slack 工作流配置好了，希望先保持稳定；
- 你依赖 1.6.x 的 exact-commit Slack 控制流程；
- 暂时不打算迁移，但还需要旧版 persistent workspace/process 工作方式。

当前旧版正式版本：`1.6.3`。

## 什么时候选 2.x

- 你准备使用当前主线；
- 你希望 ChatGPT 走 2.x 的 MCP/Tunnel 工作流，而不是 Slack 控制消息；
- 你需要 `main` 文档中介绍的独立 2.x 本地 OpenAI-compatible provider 能力。

2.x 的具体行为请直接看 [`main` 文档](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main)，不要从当前 1.6.x 分支反推。

## 关键差异

### ChatGPT 怎么连接

**1.6.x：** ChatGPT 连接 GitHub 和 Slack，Slack 承载 CWapi request/response frame。

**2.x：** 使用自己的 MCP/Tunnel 连接模型，Slack 不再是 2.x 控制传输层。

### GitHub 扮演什么角色

**1.6.x：** GitHub 是标准工作流的一部分。Web GPT 从 GitHub 取得 repository URL / exact commit，并把 tracked source 的长期修改提交到 GitHub。

**2.x：** 以 `main` 文档为准，不要把 1.6.x 的 exact-commit Slack 协议套过去。

### 本地 Provider

**1.6.x：** 没有 localhost OpenAI-compatible provider。

**2.x：** 有单独文档说明的本地 provider 架构。

### Slack

**1.6.x：** 正常远程控制/结果返回流程需要 Slack。

**2.x：** 不要复制 1.6.x Slack 配置来“迁移”。

### Persistent workspace

**1.6.x：** 一个 CWapi 进程内，同 repository 复用一个 workspace；tracked source 会同步到 exact commit，兼容的 ignored/untracked 衍生状态可以继续留在 workspace。

**2.x：** 使用不同的 durable Coding workspace 模型，生命周期和 resume 规则看 `main` 的 Coding 文档。

### 配置迁移

两边配置 schema 不兼容。不要直接用 2.x 覆盖旧 1.6.x 目录，再把旧 config 整包复制过去。

建议：

1. 保留现有 1.6.3 目录；
2. 把 2.x 解压到另一个目录；
3. 按 2.x 自己的文档重新配置；
4. 单独验证新工作流；
5. 确认自己要哪条路线后，再决定是否保留 1.6.x。

## 分支

- [`1.6.x`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/1.6.x)：CWapi 1.6.x 旧版发行与文档。
- [`main`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main)：CWapi 2.x 当前主线。
