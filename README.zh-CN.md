# CWapi

[English](README.md) | [简体中文](README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/AAAYNMMM/chatgpt-work-api-Releases?filter=v2.0.0&style=flat-square&label=Release)](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/tag/v2.0.0)
![Windows](https://img.shields.io/badge/Windows-11%20x64-0078d4?style=flat-square)
![MCP](https://img.shields.io/badge/MCP-Coding%20%2B%20Agent-6f42c1?style=flat-square)
![OpenAI compatible](https://img.shields.io/badge/API-OpenAI--compatible-10a37f?style=flat-square)

**让 ChatGPT Web 成为本地 Coding agent，同时把 Web GPT 提供给支持 OpenAI-compatible provider 的本地软件。项目和开发工具仍留在你的 Windows 电脑上。**

CWapi 2.0 提供两条彼此隔离的链路：

- **Coding**：ChatGPT Web 通过 MCP 操作本地 Git workspace，读取、修改、编译、测试和执行开发命令。
- **Agent**：CWapi 在 localhost 提供 OpenAI-compatible API，本地软件把模型请求交给 Web GPT，再取得回答或 `tool_calls`。

当前正式版本：**`2.0.0`**。

## CWapi 是什么？

CWapi 是 Windows portable 桌面桥接程序，本身不运行 AI 模型。负责理解任务、分析源码和决定下一步的是 Web GPT；CWapi 负责本地执行、durable repository workspace、通信和 localhost Provider。

### Coding

```text
ChatGPT Web
    ↓
CWapi Coding MCP
    ↓
本地 durable Git workspace
    ↓
Build / Test / Git / Development Tools
```

Coding 对外只有 5 个工具：

```text
coding_open
coding_exec
coding_status
coding_attachment
coding_close
```

Web GPT 是这条链上**唯一负责推理的 agent**。portable 内置的官方 Codex runtime 只作为 model-free `command/exec` 工具宿主和 Windows sandbox。CWapi 不创建 Codex thread/turn，不把任务交给 Codex Agent 推理，也不要求用户为 Coding 登录 Codex 账号。

### Agent

```text
Cline / Roo Code / 其它 OpenAI-compatible 软件
    ↓
CWapi localhost /v1
    ↓
Agent broker
    ↓
CWapi Agent MCP
    ↓
ChatGPT Web
```

本地 Provider：

```text
Base URL: http://127.0.0.1:<agent-port>/v1
Model:    cwapi-web-gpt
API key:  由 CWapi 生成并在 GUI 中提供
```

实际实现的 endpoints：

```text
GET  /v1/models
POST /v1/chat/completions
```

Cline、Roo Code 等允许自定义 OpenAI-compatible provider 的客户端**可能**可以直接接入，但具体兼容性取决于客户端版本和它发送的请求特性。这里不做“所有版本一定支持”这种勇敢但没必要的承诺。

## 为什么使用 CWapi？

- 让 Web GPT 针对真实本地 Git 项目工作。
- 读取、搜索、修改源码并执行精确命令，不把 Coding 任务再转给第二个 AI agent。
- durable workspace 可以跨 ChatGPT 对话继续同一个仓库任务。
- 普通开发保持 `SAFE`，只有用户明确授权、需要 `.git` metadata 写入或更高本机权限时再切 `FULL`。
- 给本地 OpenAI-compatible 软件提供一个由 Web GPT 驱动的 localhost 模型入口。
- Coding 与 Agent 真正隔离：不同 MCP token、不同 tool catalog、不同 Tunnel 配置和 runtime path。
- 普通文件不混进 MCP attachment；只传经过校验的 raster image。

## 版本怎么选？

| 版本 | 通信方式 | 主要用途 |
| --- | --- | --- |
| **2.x** | MCP + OpenAI Secure MCP Tunnel | Coding MCP + OpenAI-compatible Agent bridge |
| **1.6.x** | GitHub + Slack | 旧版 Web GPT 本地开发工作流 |

两条路线配置不兼容。准备选择或迁移前先看 [版本选择指南](docs/VERSION_GUIDE.zh-CN.md)。

## 核心能力

### Coding

- 通过 GitHub repository URL 和 branch ref 打开或恢复仓库。
- 可选完整 40 位 `expected_commit` 做 exact baseline guard。
- durable workspace 位于 `CWapi-data/workspaces/<repository-hash>/repo`。
- 通过精确命令读取、搜索源码。
- 在受管 workspace 中修改项目文件。
- 运行编译器、测试、脚本、localhost 服务和 Git 命令。
- `coding_status` 查看 HEAD、tracking HEAD、dirty 与 divergence。
- 新 ChatGPT 对话通过兼容的 `coding_open(..., resume=true)` 继续同一 active repository。
- `coding_attachment` 把受支持的 workspace raster image 返回 Web GPT。

### Agent

- `127.0.0.1` 上的 OpenAI-compatible `/v1` Provider。
- Chat Completions 普通 JSON 和 streaming。
- function/tool call 返回本地客户端执行，不由 Agent GPT 擅自执行第三方软件的本地工具。
- 多个独立请求用准确 `request_id` 关联。
- 有界 queue 与 request timeout。
- 标准 Chat Completions `image_url` data URI inline raster image。
- 独立 Agent MCP bridge 与独立 Secure MCP Tunnel 配置。

## 5 分钟快速开始

1. 下载 [`CWapi-v2.0.0.zip`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/download/v2.0.0/CWapi-v2.0.0.zip)。
2. 完整解压到当前用户可写目录，运行 `CWapi.exe`。
3. 要使用 **Coding**，先创建 OpenAI Secure MCP Tunnel，取得 Tunnel ID 与 Runtime API key，再填入 CWapi 的 Coding Tunnel 面板。
4. 在 ChatGPT 使用支持你所需 MCP 能力的计划/Workspace，按当前 Developer Mode / custom app 流程连接同一个 Tunnel。ChatGPT 不能直接访问 CWapi 的 `127.0.0.1` MCP 地址。
5. 确认 5 个 Coding 工具出现，再用 `coding_open` + `coding_status` 或只读 `coding_exec` 做第一次仓库测试。
6. 要使用 **Agent**，再创建第二个独立 Secure MCP Tunnel，填入 Agent Tunnel 面板，并在 ChatGPT 把它作为另一个 MCP app 连接。
7. 在 Agent 对话中调用 `agent_open`，连续任务期间持续使用 `agent_exchange`。
8. 本地 OpenAI-compatible 软件填写上面的 Base URL、model 和 GUI 给出的 Agent API key。

从零配置 Tunnel、第一次 Coding/Agent 测试的完整步骤见 [快速入门](docs/GETTING_STARTED.zh-CN.md)。

## SAFE 与 FULL

`SAFE` 用于日常源码读取、修改、build 和 test，底层使用 Codex sandbox 的 workspace-write profile，并保护 `.git` metadata。

`FULL` 只用于用户已经明确授权、确实需要 Git metadata 写入或更广本机权限的操作，例如本机 commit/push。权限切换影响下一条 `coding_exec`；已经运行中的命令继续使用启动时的 profile。永久 execution policy 仍然有效。

## Durable workspace

`coding_close` 只关闭当前 active session handle，**不会删除 workspace**。workspace 保存在 portable 旁的 `CWapi-data`，以后可以继续。

新的 non-resume open 遇到 tracked dirty、local commits 或 divergence 会拒绝，而不是偷偷覆盖。`resume=true` 才表示显式继续兼容的现有 workspace/session。

整体移动解压目录时，旁边的 `CWapi-data` 会一起移动；只移动干净程序/runtime 到新位置，则新位置会创建新的 data root。

## 图片与普通文件

`coding_attachment` 只支持 raster image，并以原生 MCP `ImageContent` 返回。源码、Markdown、JSON、日志、PDF、压缩包、DOCX 等普通文件不是通用 MCP attachment。可读取文本直接用 `coding_exec`。

Agent 也是同一原则：本地客户端可以通过 `image_url` data URI 发送有界 inline raster image；顶层 generic `attachments` 会返回 `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`。

用户上传到 ChatGPT 对话里的文件不会自动写入 Coding workspace，也不会自动进入 Agent 本地软件。

## 常见使用场景

- 一个真实仓库任务跨多个 ChatGPT 对话继续，不必每次重新准备 workspace。
- 让 Web GPT 查看、修改、编译和测试本地 Git 项目，同时把推理留在 ChatGPT Web。
- 把支持 OpenAI-compatible provider 的本地客户端当作界面，由 Web GPT 通过 Agent 回答模型请求。
- Coding 和 Agent 分开运行，一个负责仓库开发，一个负责本地 Provider，不互相混 tool catalog。
- private Git repository 继续使用当前 Windows 用户已有的 Git/GitHub credential。

## 安全与隐私

CWapi 的本地 MCP 与 Agent Provider 都只绑定 loopback。Secure MCP Tunnel 通过 outbound 私有连接把本地 MCP 接到支持的 OpenAI 产品，不要求把 CWapi 暴露成公网入站服务。

本地 secret 分开保存：

- Coding / Agent MCP bearer token：`CWapi-data/config/cwapi.json`。
- Agent Provider API key：`CWapi-data/config/cwapi.json`。
- Coding / Agent Tunnel ID：`CWapi-data/config/cwapi.json`。
- Coding / Agent Tunnel Runtime API key：Windows Credential Manager 中两个独立条目。

CWapi 不建立持久化完整对话 transcript，也不保存完整 command-output 历史。正常 observability 只保留有界 metadata/error，不持久化完整 prompt、answer、tool schema、tool arguments 或 tool results。

详细边界见 [安全说明](docs/SECURITY.md)。

## 文档

- [快速入门](docs/GETTING_STARTED.zh-CN.md)
- [Coding 指南](docs/CODING_GUIDE.zh-CN.md)
- [Agent 指南](docs/AGENT_GUIDE.zh-CN.md)
- [常见问题](docs/FAQ.zh-CN.md)
- [故障排查](docs/TROUBLESHOOTING.zh-CN.md)
- [版本选择指南](docs/VERSION_GUIDE.zh-CN.md)
- [从 1.6 迁移](docs/MIGRATION_FROM_1.6.zh-CN.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Protocol](docs/PROTOCOL.md)
- [Security](docs/SECURITY.md)
- [Operations](docs/OPERATIONS.md)
- [Codex Toolhost](docs/CODEX_TOOLHOST.md)

## 发行路线

- [`main`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main)：CWapi 2.x，当前正式版本 `2.0.0`。
- [`1.6.x`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/1.6.x)：CWapi 1.6.x 旧版路线，当前正式版本 `1.6.3`。

## 开发仓库与发行仓库

当前仓库只放面向发行的干净源码快照、portable Release 和用户文档。完整开发历史、测试、验证/打包自动化和发行工程位于 [`AAAYNMMM/CWapi`](https://github.com/AAAYNMMM/CWapi)。

CWapi 2.0.0 对应开发源码 commit：

```text
d904ae80428c90717e050a151c65fa35b6b83c63
```
