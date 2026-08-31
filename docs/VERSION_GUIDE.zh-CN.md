# CWapi 版本选择指南

[English](VERSION_GUIDE.md) | [简体中文](VERSION_GUIDE.zh-CN.md)

CWapi 现在有两条彼此独立的发行路线。它们都在解决“让 Web GPT 参与本地开发”这个大方向的问题，但通信、配置和实际工作流已经差得足够远，最好把它们当成两套产品看。

## 先看结论

| 版本 | 通信方式 | 主要用途 |
| --- | --- | --- |
| **2.x** | MCP + OpenAI Secure MCP Tunnel | Coding MCP + OpenAI-compatible Agent bridge |
| **1.6.x** | GitHub + Slack | 旧版 Web GPT 本地开发工作流 |

新安装默认推荐 **2.x**。只有你明确依赖现有 1.6.x Slack 工作流时，才更适合继续留在 1.6.3。

## 详细比较

| 项目 | CWapi 2.x | CWapi 1.6.x |
| --- | --- | --- |
| 当前版本 | `2.0.0` | `1.6.3` |
| 发行分支 | [`main`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main) | [`1.6.x`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/1.6.x) |
| ChatGPT 连接 | 通过 OpenAI Secure MCP Tunnel 连接 MCP | 旧版 Slack 中转工作流 |
| Coding 通信 | 直接 Coding MCP tool surface | GitHub + Slack 工作流 |
| 本地 Provider | 有：localhost OpenAI-compatible `/v1` Agent Provider | 1.6.3 发行路线没有 2.0 这套 Agent Provider contract |
| Slack | 不需要 | 旧版工作流需要 |
| Durable workspace | 有，位于 `CWapi-data/workspaces/<repository-hash>/repo` | 1.6.x 有自己的持久化/workspace 模型，不要当成 2.0 兼容目录 |
| GitHub 的作用 | Coding 的 repository source/remote；private auth 使用当前 Windows 用户 Git 凭据 | 旧版 GitHub + Slack 开发工作流的核心组成部分 |
| Coding 推理 | 只有 Web GPT；bundled Codex 只是 model-free command/exec toolhost | 按 1.6.3 分支自己的文档工作 |
| OpenAI-compatible Agent bridge | 有 | 1.6.3 没有等价的 2.0 Agent Provider contract |
| Secure MCP Tunnel | Coding / Agent 都接 ChatGPT 时各用一条 | 1.6.3 Slack 路线不需要 |
| 迁移难度 | 建议重新配置；config schema 不兼容 | 留在 1.6.3 无需迁移，但继续使用旧 transport |
| 更适合谁 | 新用户、MCP 用户、需要本地 Coding 或 OpenAI-compatible Agent bridge 的用户 | 已经稳定依赖 1.6.3 Slack 工作流的现有用户 |

## 这些情况选 2.x

- 想让 ChatGPT Web 通过很小的一组 MCP tools 操作本地 Git workspace；
- 想让 Coding workspace 跨 ChatGPT 对话继续；
- 想在 localhost 提供由 Web GPT 驱动的 OpenAI-compatible Provider；
- 想让 Coding 与 Agent 分开连接、分开 Tunnel；
- 不想在 2.0 链路里继续依赖 Slack。

从 [快速入门](GETTING_STARTED.zh-CN.md) 开始。

## 这些情况继续留在 1.6.3

如果你当前生产/日常工作已经稳定依赖 `1.6.x` 的 GitHub + Slack transport，而且暂时不值得为了 2.0 架构重新配置，那么继续留在 1.6.3 很合理。

`1.6.x` 分支自己的 1.6.3 文档才是这个版本的依据。不要拿 2.0 Coding/Agent 配置去套 1.6.3。

## 几个不能混用的边界

### Slack 不是 2.0 的通信方式

CWapi 2.0 的 Coding / Agent 不使用 Slack。2.0 文档里出现 Slack，只应该用于版本比较或迁移说明。

### Secure MCP Tunnel 不是 1.6.3 的要求

1.6.3 使用的是旧版 Slack route。给旧版旁边再配一条 2.0 Tunnel，并不会让它变成 2.0。

### 配置不能整套搬过去

不要把旧 `CWapi-data`、旧 config、Slack credential 或 workspace 目录整套覆盖到 2.0。2.0 应重新配置。

### workspace 不是可以直接搬运的升级包

两代都有持久化 workspace 的概念，但实现、目录和配置 contract 不同。重要工作应该通过 Git/备份保留，而不是把旧 workspace 目录当成官方支持的 2.0 import 格式。

## 发行链接

- CWapi 2.0.0 portable：[`CWapi-v2.0.0.zip`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/download/v2.0.0/CWapi-v2.0.0.zip)
- 2.x 源码与文档：[`main`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main)
- 1.6.x 源码与文档：[`1.6.x`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/1.6.x)

已有 1.6.3 安装准备迁移时，继续看 [从 1.6 迁移](MIGRATION_FROM_1.6.zh-CN.md)。
