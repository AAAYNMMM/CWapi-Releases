# 从 CWapi 1.6.3 迁移到 2.0

[English](MIGRATION_FROM_1.6.md) | [简体中文](MIGRATION_FROM_1.6.zh-CN.md)

CWapi 2.0 **不是** 1.6.3 的简单原地升级。通信架构已经重做。更稳妥的做法，是把 2.0 当成一套新的安装来配置，在确认新工作流稳定前保留原来的 1.6.3。

## 最大变化是什么

1.6.x 主要是：

```text
CWapi 1.6.x
Web GPT <-> GitHub + Slack 工作流 <-> 本地开发流程
```

2.0 变成：

```text
CWapi 2.0 Coding
ChatGPT Web -> Secure MCP Tunnel -> Coding MCP -> durable 本地 Git workspace

CWapi 2.0 Agent
本地 OpenAI-compatible 客户端 -> CWapi /v1 -> Agent MCP -> Secure MCP Tunnel -> ChatGPT Web
```

所以旧的 Slack 配置不能直接变成 2.0 连接配置。

## 不要把旧数据目录整套覆盖到新安装

1.6.x 和 2.0 的 config schema 不兼容。

**不要**把旧 `CWapi-data`、旧 config、workspace tree 或 credential material 整套覆盖进干净的 2.0 安装。

建议并排安装，例如：

```text
C:\Tools\CWapi-1.6.3\...
C:\Tools\CWapi-2.0.5\...
```

两个解压目录各自带自己的 data root，可以并存。先把 2.0 跑通，再考虑是否归档旧目录。

## 哪些东西不迁移

### Slack 凭据

这些 1.6.x 配置不迁移到 2.0：

- Slack App Token；
- Slack Bot Token；
- Slack Channel ID；
- 其它只属于 Slack route 的配置。

CWapi 2.0 的 Coding 和 Agent 都不使用 Slack。

### 1.6 的 MCP v2 Slack frame

1.6.x Slack 工作流里的 MCP v2 framing，**不是** 2.0 的 ChatGPT direct MCP surface。

2.0 由 ChatGPT 通过 Secure MCP Tunnel 发现 Coding/Agent 的公开 MCP tools。不要拿旧 Slack frame 或旧 bridge 标识去当 2.0 app 配置。

### 旧 workspace 目录不能当成 2.0 import

两代都有持久化/workspace 概念，但实现和目录 contract 不同。

不要把 1.6 workspace 直接复制到 `CWapi-data/workspaces/<repository-hash>/repo`，然后假设 2.0 会把它当成兼容 workspace。重要本地工作应先通过 Git commit/push 或单独备份保留下来，再让 2.0 自己创建和管理 workspace。

## 2.0 新增、需要重新配置的概念

### Coding MCP

2.0 对外只有这 5 个 Coding tools：

```text
coding_open
coding_exec
coding_status
coding_close
load_skill
```

负责推理的只有 Web GPT。bundled Codex 只是 model-free command/exec toolhost，不是第二个 agent。

### Coding Secure MCP Tunnel

为 Coding 单独创建 Secure MCP Tunnel，配置：

```text
Tunnel ID
Runtime API key
```

Tunnel ID 保存到：

```text
CWapi-data/config/cwapi.json
```

Coding Runtime API key 保存到 Windows Credential Manager：

```text
CWapi/2.0/OpenAI/Tunnel/APIKey
```

### Agent Provider

2.0 新增 localhost OpenAI-compatible Provider：

```text
Base URL: http://127.0.0.1:<agent-port>/v1
Model:    cwapi-web-gpt
```

实际实现：

```text
GET  /v1/models
POST /v1/chat/completions
```

Agent Provider API key 由 CWapi 生成，保存到：

```text
CWapi-data/config/cwapi.json
```

### Agent MCP 与 Agent Tunnel

如果要使用 Agent，还要再创建第二条独立 Secure MCP Tunnel，专门连接 Agent MCP app。

它自己的 Runtime API key 保存到：

```text
CWapi/2.0/OpenAI/Tunnel/Agent/APIKey
```

不要拿 Coding Tunnel ID/profile 给 Agent 共用。

## 凭据保存方式也变了

干净的 2.0 安装把普通配置和 Tunnel Runtime API key 分开保存。

`CWapi-data/config/cwapi.json` 中包括：

- Coding MCP token；
- Agent MCP token；
- Agent Provider API key；
- Coding Tunnel ID；
- Agent Tunnel ID；
- 其它非 secret runtime config。

Windows Credential Manager 中包括：

```text
CWapi/2.0/OpenAI/Tunnel/APIKey
CWapi/2.0/OpenAI/Tunnel/Agent/APIKey
```

不要手工搬运旧 credential file 去覆盖新安装。

## 推荐迁移步骤

1. **先收尾或保护 1.6.3 里的重要工作。** 该 commit/push 的先做完，不能丢的本地内容另做备份。
2. **保留原来的 1.6.3 目录。** 暂时不要改动这套已经能工作的环境。
3. **把 2.0.5 完整解压到另一个目录。** 下载：[`CWapi-v2.0.5.zip`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/download/v2.0.5/CWapi-v2.0.5.zip)。
4. **先运行一次 2.0。** 让它生成自己的 `CWapi-data` 和 v3 config。
5. **需要 Coding 就重新配置 Coding Tunnel。** 新建 Secure MCP Tunnel，通过 Coding 面板保存 Tunnel ID / Runtime API key。
6. **在 ChatGPT 连接 Coding app。** 确认准确出现 4 个 `coding_*` tools。
7. **先用测试仓库验证只读 Coding。** 再验证 SAFE 下的普通 edit/build/test。
8. **需要 Agent 再单独配置。** 建第二条 Agent Tunnel，连接 Agent MCP app，本地客户端填写 CWapi 的 Base URL / model / Agent API key。
9. **用真实工作流验证一遍。** 不要因为 GUI 能打开就宣布迁移成功，人类软件史上已经有太多这种乐观主义。
10. 只有确认 2.0 满足实际需求以后，再归档旧 1.6.3 目录。

## 哪些东西应该重新配置

准备在 2.0 里重新配置：

- Coding Secure MCP Tunnel ID 和 Runtime API key；
- 如果使用 Agent，重新配置 Agent Secure MCP Tunnel ID 和 Runtime API key；
- ChatGPT 里的 Coding / Agent custom MCP app 连接；
- 如果使用 Agent，本地 OpenAI-compatible 客户端的 Base URL / model / API key；
- private Git credential 只有在当前 Windows 用户原本的 Git/GitHub 凭据不可用时才需要重新处理。

Slack App Token / Bot Token / Channel ID 不迁移到 2.0。

## 什么时候应该继续留在 1.6.3

这些情况下，继续用 1.6.3 很合理：

- 现在的 GitHub + Slack 工作流已经稳定、够用；
- 当前不方便或不想使用 Secure MCP Tunnel/custom MCP app 路径；
- 某个本地集成明确依赖 1.6.x Slack 工作流；
- 目前迁移风险比 2.0 新的 Coding/Agent 能力更重要。

2.0 存在，不代表每台已经稳定运行 1.6.3 的机器都必须立刻迁移。`1.6.x` 分支继续作为这个版本的源码和文档路线。

## 迁移之后

配置细节看 [快速入门](GETTING_STARTED.zh-CN.md)，workspace 行为看 [Coding 指南](CODING_GUIDE.zh-CN.md)，本地 Provider 看 [Agent 指南](AGENT_GUIDE.zh-CN.md)，连接错误看 [故障排查](TROUBLESHOOTING.zh-CN.md)。
