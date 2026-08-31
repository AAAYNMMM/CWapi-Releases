# CWapi 2.0 快速入门

[English](GETTING_STARTED.md) | [简体中文](GETTING_STARTED.zh-CN.md)

这份文档从一台还没配置 CWapi 的 Windows 电脑开始，一直做到第一次 Coding 调用和第一次 Agent 调用真正跑通。

CWapi 2.0 有两条独立链路。只需要其中一条，就只配置那一条。

```text
Coding：ChatGPT Web -> Secure MCP Tunnel -> Coding MCP -> 本地 Git workspace
Agent： 本地 OpenAI-compatible 客户端 -> CWapi /v1 -> Agent MCP -> Secure MCP Tunnel -> ChatGPT Web
```

## 1. 下载 CWapi 2.0.0

下载正式 Windows portable：

[`CWapi-v2.0.0.zip`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/download/v2.0.0/CWapi-v2.0.0.zip)

完整解压到当前 Windows 用户可写目录，然后运行：

```text
CWapi.exe
```

不要在 ZIP 里面直接运行，也不要只复制一个 `CWapi.exe`。portable 旁边还有锁定版本的 Git、Codex toolhost 和 tunnel runtime，人类把便携版拆成零件以后再问为什么不能跑，这种事故能省则省。

## 2. 第一次启动会生成什么

CWapi 把用户/运行数据放在程序旁边的 `CWapi-data`。

首次启动如果没有配置，会生成：

```text
CWapi-data/config/cwapi.json
```

默认配置包括：

- 自动生成的 Coding MCP bearer token；
- 自动生成的 Agent MCP bearer token；
- 自动生成的 Agent Provider API key；
- MCP loopback port，默认 `32124`；
- Agent Provider loopback port，默认 `32123`；
- Agent 默认启用；
- Coding access profile 默认为 `safe`；
- Coding / Agent Tunnel 初始为空且未启用。

真正打开仓库以后才会出现：

```text
CWapi-data/workspaces/<repository-hash>/repo
```

配置对应 Tunnel 后才会生成：

```text
CWapi-data/tunnel/coding/openai-tunnel.yaml
CWapi-data/tunnel/agent/openai-tunnel.yaml
```

Runtime API key 本体不会写进这些 YAML 或 `cwapi.json`。Coding 与 Agent 两枚 Runtime API key 分别保存在 Windows Credential Manager。

## 3. Coding 和 Agent 到底有什么区别

如果你想让 **ChatGPT Web 自己操作本地 Git 项目**，包括看源码、改文件、执行命令、编译、测试、做用户授权的 Git 操作，使用 **Coding**。

如果你有一个本地软件，它会调用 OpenAI-compatible Chat Completions API，希望让这个软件的模型请求由 **Web GPT 回答**，使用 **Agent**。

两条链互不依赖。可以只用 Coding，也可以只开 Agent；二者也可以同时运行。

## 4. 为什么不能把 `127.0.0.1` 填进 ChatGPT

CWapi 故意只把 MCP server 绑定到 loopback：

```text
http://127.0.0.1:<mcp-port>/mcp/coding/<coding-token>
http://127.0.0.1:<mcp-port>/mcp/agent/<agent-token>
```

这些地址只有运行 CWapi 的这台 Windows 电脑自己能访问。ChatGPT 是远程服务，所以把 `127.0.0.1` 填进远程 Server URL 并不会产生某种量子隧穿效果。

OpenAI 当前对“开发者电脑/私有网络中的 MCP server”的官方说明是：不能直接连接，应使用 **Secure MCP Tunnel**，而不是把本地 MCP 暴露到公网。

ChatGPT 的计划、Workspace 权限和 Developer Mode UI 会继续变化，启用自定义 MCP app 时以 OpenAI 当前官方说明为准：

https://help.openai.com/en/articles/12584461

## 5. 创建 Coding Secure MCP Tunnel

在 OpenAI Platform 侧为 Coding 建一个 Secure MCP Tunnel。具体菜单名称以后可能变化，但你最终需要拿到两项：

```text
Tunnel ID       = tunnel_...
Runtime API key = 有权运行/使用该 Tunnel 的 secret key
```

Tunnel ID 表示“是哪条 Tunnel”。Runtime API key 是本机 `tunnel-client` 连接 OpenAI tunnel control plane 的认证凭据。

这枚 key 是**传输认证**，不是 Codex 登录，也不是 CWapi 拿去调用 OpenAI 模型完成 Coding 的 API key。

给 Runtime key 只授予组织实际需要的 Tunnel 权限。不要把它发进 ChatGPT prompt，不要 commit，不要为了图省事塞进 `.env`。

## 6. 在 CWapi 配置 Coding Tunnel

打开 GUI 的 Coding Tunnel 配置，填写：

```text
Tunnel ID
Runtime API key
```

CWapi 会把 Tunnel ID 写入 `CWapi-data/config/cwapi.json`，Runtime API key 保存到 Windows Credential Manager，再生成只属于 Coding 的 `openai-tunnel.yaml`。其中 `main` channel 只转发 Coding MCP endpoint。

配置有效后，portable 内置的 `tunnel-client` 会启动。如果子进程首次启动失败或异常退出，CWapi 会做有界重试，GUI 可能显示“正在重连”。

## 7. 在 ChatGPT 连接 Coding

使用你的 ChatGPT Workspace 当前提供的 custom MCP app / Developer Mode 流程。不要选择直接连接 `127.0.0.1` Server URL，而是走 Secure MCP Tunnel，并选择和 CWapi Coding 面板一致的那条 Tunnel。

不管 OpenAI 以后把按钮挪到哪个角落，真正需要满足的只有这几件事：

1. 当前 ChatGPT 计划/Workspace 允许你使用 Coding 所需的自定义 MCP 能力；
2. ChatGPT app 选择的是和 CWapi 一致的 Coding Tunnel；
3. CWapi 电脑上的 Tunnel 正在运行；
4. ChatGPT 能发现准确的 5 个 Coding tools：

```text
coding_open
coding_exec
coding_status
coding_attachment
coding_close
```

如果这 5 个工具没出现，先查 [故障排查](TROUBLESHOOTING.zh-CN.md)，不要还没通车就开始研究怎么漂移过弯。

## 8. 第一次 Coding 测试

选择一个你愿意让 CWapi clone 到受管 workspace 的仓库。第一次可以给 Web GPT 一个无修改任务，例如：

```text
打开 https://github.com/OWNER/REPO 的 main 分支，先检查当前状态，不要修改文件。
```

合理流程大致是：

```text
coding_open(repository_url, target_ref="main")
        ↓
coding_status(repository_url)
        ↓
必要时只读 coding_exec(...)
```

`target_ref` 是必填 branch。还可以传完整 40 位 `expected_commit`，要求 CWapi fetch 后解析出的 branch commit 必须正好等于这个 SHA。

第一次读/查/测保持 `SAFE`。

## 9. 新 ChatGPT 对话怎么继续旧 Coding 任务

CWapi 不向 Web GPT 暴露随机 Coding session ID。Web GPT 始终用 canonical `repository_url` 定位仓库。

如果同一 repository 已经有兼容的 active session/workspace，新对话调用：

```text
coding_open(repository_url, target_ref, expected_commit?, resume=true)
```

CWapi 复用内部 active session，并返回 `resumed=true`，不会再准备第二份 workspace。

如果 repository 还 active，却用 `resume=false` 再开，会返回 `CODING_WORKSPACE_BUSY`，而不是静默抢走原 workspace。

## 10. 配置 Agent

Agent 有两端：

```text
本地客户端 -> http://127.0.0.1:<agent-port>/v1
ChatGPT Web -> Secure MCP Tunnel -> Agent MCP
```

Agent 启用后 localhost Provider 可以启动，但如果 ChatGPT 侧 Agent MCP bridge 没打开，请求最终无法得到 Web GPT 回答。

为 Agent **另外创建第二条 Secure MCP Tunnel**。不要复用 Coding 的 Tunnel ID 或 Runtime API key。Agent Tunnel 面板填写自己的：

```text
Tunnel ID
Runtime API key
```

CWapi 会为它运行另一份独立 tunnel-client profile/process，而且只指向 Agent MCP。

## 11. 在 ChatGPT 连接 Agent MCP

通过 Agent Tunnel 建立另一个自定义 MCP app。它应该只暴露：

```text
agent_open
agent_exchange
agent_close
```

在 Agent Web GPT 对话中：

1. 连续任务开始时调用一次 `agent_open()`；
2. 调 `agent_exchange(capacity=4)`，如果 `agent_open` 返回更小 `max_inflight` 就用更小值；
3. 处理 batch 中每个 request；
4. 下一次 `agent_exchange` 一次性提交已完成 responses，同时等待下一批 request；
5. 连续任务期间保持 exchange loop；
6. 真正结束后才调用 `agent_close()`。

一次 `no_request` 不代表 bridge 应该关掉重开。

## 12. 配置本地 OpenAI-compatible 客户端

默认 Agent Provider：

```text
Base URL: http://127.0.0.1:32123/v1
Model:    cwapi-web-gpt
API key:  从 CWapi GUI 复制 Agent API key
```

如果改过 Agent port，以 GUI 当前显示的 Base URL 为准。

实际实现 endpoints：

```text
GET  /v1/models
POST /v1/chat/completions
```

Provider 使用 Bearer auth，API key 错误返回 HTTP 401。

Cline、Roo Code 等软件如果支持自定义 OpenAI-compatible provider，可以按上面参数测试；不同版本客户端的具体兼容性不做绝对保证。

## 13. 第一次 Agent 测试

先确认 ChatGPT 里的 Agent bridge 已经 `agent_open`。然后从本地客户端向 `cwapi-web-gpt` 发一条简单聊天请求。

请求路径：

```text
POST /v1/chat/completions
        ↓
CWapi broker 生成并排队 request_<...>
        ↓
agent_exchange 把 request 交给 Web GPT
        ↓
Web GPT 对同一 request_id 返回 content 和/或 tool_calls
        ↓
CWapi 给本地软件返回 Chat Completions response
```

本地客户端收到：

- **503**：Agent MCP bridge 不可用/未打开；
- **429**：有界 queue 已忙；
- **504**：Web GPT 没在默认 180 秒 request timeout 内完成请求。

## 14. 第一次测试不要拿普通文件折腾

Coding 的 `coding_attachment` 只接受经过校验的 workspace raster image。Agent 只接受标准 Chat Completions `image_url` data URI inline raster image。

Agent generic `attachments` 会返回 `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`。上传到 ChatGPT 对话里的文件也不会被自动复制到 Coding workspace 或 Agent 本地软件。

## 下一步

- [Coding 指南](CODING_GUIDE.zh-CN.md)
- [Agent 指南](AGENT_GUIDE.zh-CN.md)
- [常见问题](FAQ.zh-CN.md)
- [故障排查](TROUBLESHOOTING.zh-CN.md)
