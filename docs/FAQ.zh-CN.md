# CWapi 2.0 常见问题

[English](FAQ.md) | [简体中文](FAQ.zh-CN.md)

这份 FAQ 主要回答安装 CWapi 2.0 前、选择版本时，以及第一次连接 Coding / Agent 时最容易遇到的问题。

## CWapi 是什么？

CWapi 是一个 Windows portable 桥接程序，让 ChatGPT Web 能处理本地开发任务，又不需要让 CWapi 自己再运行一个负责推理的模型。

2.0 有两条彼此独立的链路：

- **Coding**：ChatGPT Web 通过 MCP 操作 durable 本地 Git workspace。
- **Agent**：本地软件调用 CWapi 的 localhost OpenAI-compatible API，请求再通过 Agent MCP 交给 Web GPT 回答。

CWapi 本身不运行语言模型。

## 2.0 和 1.6.3 应该选哪个？

想使用当前的 MCP / Secure MCP Tunnel 架构、Coding MCP、2.0 durable workspace，以及 OpenAI-compatible Agent bridge，选 **2.0**。

如果你明确依赖旧的 GitHub + Slack 工作流，而且暂时不想迁移这套通信与配置，继续用 **1.6.3**。

两条路线不是“解压新版覆盖旧版”这么简单。详细比较见 [版本选择指南](VERSION_GUIDE.zh-CN.md) 和 [从 1.6 迁移](MIGRATION_FROM_1.6.zh-CN.md)。

## Coding 和 Agent 有什么区别？

**Coding** 是让 ChatGPT Web 直接操作一个受管 Git workspace，对外只有：

```text
coding_open
coding_exec
coding_status
coding_attachment
coding_close
```

**Agent** 是让本地 OpenAI-compatible 软件把模型请求交给 Web GPT，对外只有：

```text
agent_open
agent_exchange
agent_close
```

Agent 不会自动获得 Coding 工具，Coding 也不会自动变成本地 OpenAI-compatible Provider。

## CWapi 2.0 需要 Slack 吗？

不需要。2.0 的 ChatGPT 连接使用 MCP + OpenAI Secure MCP Tunnel。

Slack 属于 `1.6.x` 旧版工作流。

## 1.6.3 需要 Secure MCP Tunnel 吗？

不需要。1.6.3 使用旧的 GitHub + Slack 工作流，不要把 2.0 的 Tunnel 配置硬套进去。两个版本已经是两套不同路线。

## Coding 需要登录 Codex 吗？

不需要。

2.0 Coding 不通过 Codex 账号/模型完成推理，也不要求 Codex login。

## portable 里那个 Codex 到底做什么？

CWapi 2.0 内置官方 Codex runtime，当前锁定版本是 `0.150.1`，但这里只把它当作 model-free `command/exec` toolhost 和 Windows sandbox 组件。

Coding 不创建 Codex thread，不创建 Codex turn，也不把仓库任务转交给 Codex Agent。

## Coding 会消耗 Codex Agent quota 吗？

不会。Coding 链里负责推理的只有 Web GPT。内置 Codex 只提供 model-free command/exec，所以 CWapi Coding 不通过 Codex Agent quota 跑任务。

## 为什么需要 Secure MCP Tunnel？

CWapi 的本地 MCP server 故意只监听 `127.0.0.1`。ChatGPT 在远端运行，不能直接访问你电脑的 loopback。

Secure MCP Tunnel 提供的是远端 ChatGPT 和本机 CWapi MCP 之间的私有连接，不需要你把本地 MCP 做成一个公网入站服务。

## 为什么不能把 `127.0.0.1` 或 `localhost` 直接填进 ChatGPT？

因为 `127.0.0.1` 对谁发起连接，就指向谁自己。ChatGPT 远端看到的 `127.0.0.1` 并不是你的 Windows 电脑。

要连接本机 CWapi，请使用对应的 Secure MCP Tunnel。

## Coding 和 Agent 能同时运行吗？

可以。两条链本来就是隔离设计，各自拥有 MCP token、本地 endpoint、tool catalog、Tunnel profile 和 runtime process。

## 两条链都接 ChatGPT，需要两个 Tunnel 吗？

需要。

Coding 用一条独立 Secure MCP Tunnel，Agent 再用另一条。不要让两条链共用同一个 Tunnel ID/profile，人类很擅长为了少配一项东西多制造三项排错工作，这里没必要。

## 可以接 Cline / Roo Code 吗？

**如果客户端支持自定义 OpenAI-compatible provider，可能可以接入。**

但具体能不能正常工作，取决于客户端版本、provider 实现和它实际发送的 request shape。文档不会把“可能兼容”写成“所有版本一定支持”。

## SAFE / FULL 有什么区别？

`SAFE` 用于正常的读源码、改文件、build 和 test。它允许 workspace 内的普通开发写入，同时保护 `.git` metadata。

`FULL` 用于用户已经明确授权、而且确实需要 `.git` metadata 写入或更广 host access 的操作，例如：

```text
git commit
git push
```

已经运行中的命令保持启动时 profile；下一条 `coding_exec` 才使用新 profile。

## workspace 在哪里？

在 CWapi portable 目录旁边：

```text
CWapi-data/workspaces/<repository-hash>/repo
```

它是 durable workspace。关闭 Coding session 不会删除它。

## 换一个 ChatGPT 对话还能继续以前的 Coding 任务吗？

可以，只要现有 workspace/session 与目标仓库、branch 等信息兼容。

继续使用同一个 `repository_url`，然后：

```text
coding_open(..., resume=true)
```

公共 MCP 协议不暴露 Coding session ID。CWapi 内部通过 canonical repository 找到当前 active session。

## 同一个仓库不用 resume 再 open 会怎样？

如果这个 repository 仍有 active Coding session，`resume=false` 会返回：

```text
CODING_WORKSPACE_BUSY
```

这是保护机制，不是莫名其妙的脾气。CWapi 不会静默抢占同一仓库的 active state。

## 升级 CWapi 会删除 workspace 吗？

不会因为“升级”这个动作自动删除。

如果你把**整个解压目录**一起移动，包括 `CWapi-data`，现有数据和 workspace 会跟着走。

如果只把新的 `CWapi.exe` / `runtime` 放进另一个干净目录，新目录会创建新的 `CWapi-data`。这时旧 workspace 还在原目录，只是新安装看不到，于是看起来像“丢了”。

升级前最好先关闭 active session，并备份重要但还没 push 的修改。

## 为什么普通源码文件不能用 `coding_attachment`？

因为 `coding_attachment` 明确只支持图片。

源码、Markdown、JSON、日志、PDF、ZIP、DOCX 等普通文件通过 `coding_exec` 读取。非图片会返回：

```text
CODING_ATTACHMENT_IMAGE_ONLY
```

CWapi 2.0 不再把普通文件包装成 MCP `EmbeddedResource`。

## Coding 支持哪些图片？

当前限制：

```text
最多：            16 张
单张最大：        32 MiB
单批最大：        64 MiB
单边最大：        4096 px
SVG：             不支持
```

接受经过校验的 raster image，例如 PNG、JPEG、GIF、WebP。

## Agent 支持哪些图片？

Agent 只接受标准 Chat Completions message content 里的：

```text
type = image_url
url  = data:...
```

当前限制：

```text
最多：            8 张
单张最大：        8 MiB
单批最大：        16 MiB
单边最大：        2048 px
SVG：             不支持
```

generic top-level `attachments` 不支持。

## 在 ChatGPT 对话里上传文件，会自动进入本地 workspace 或 Agent 客户端吗？

不会。

ChatGPT conversation upload 不是 CWapi 到本地 workspace/client 的自动文件传输通道。

## CWapi 会保存完整 prompt / answer 吗？

正常 observability 不会持久化完整 prompt、完整 answer、完整 tool schema、完整 tool arguments 或完整 tool results，也不会创建 conversation transcript store。

`coding_exec` 的 stdout/stderr 作为当前 command 的有界结果返回，不作为完整长期 command-output 历史保存。

## Agent Provider API key 保存在哪里？

Agent Provider API key 生成后保存在：

```text
CWapi-data/config/cwapi.json
```

GUI 会提供/遮罩它，用于配置本地客户端。

## Tunnel Runtime API key 保存在哪里？

Coding 与 Agent 分开保存在 Windows Credential Manager：

```text
CWapi/2.0/OpenAI/Tunnel/APIKey
CWapi/2.0/OpenAI/Tunnel/Agent/APIKey
```

它们不写入 `cwapi.json`。

## MCP tokens 保存在哪里？

Coding / Agent MCP bearer token 都保存在：

```text
CWapi-data/config/cwapi.json
```

Coding / Agent Tunnel ID 和其它非 secret runtime config 也在这里。

## private Git repository 怎么认证？

private repository 的 clone/fetch/push 使用当前 Windows 用户已有的 Git/GitHub credential 环境。

CWapi 不拿 Codex 账号当 Git 身份。认证失败时，修复 Windows 用户自己的 GitHub/Git 凭据。不要把个人 GitHub token 发进 prompt，更不要 commit 到仓库。

## 本地 Agent 应该填什么 API？

使用：

```text
Base URL: http://127.0.0.1:<agent-port>/v1
Model:    cwapi-web-gpt
API key:  CWapi GUI 显示的 Agent API key
```

2.0 实际实现：

```text
GET  /v1/models
POST /v1/chat/completions
```

request、streaming、tool_calls 与错误码见 [Agent 指南](AGENT_GUIDE.zh-CN.md)。

## 相关文档

- [快速入门](GETTING_STARTED.zh-CN.md)
- [Coding 指南](CODING_GUIDE.zh-CN.md)
- [Agent 指南](AGENT_GUIDE.zh-CN.md)
- [故障排查](TROUBLESHOOTING.zh-CN.md)
- [版本选择指南](VERSION_GUIDE.zh-CN.md)
- [从 1.6 迁移](MIGRATION_FROM_1.6.zh-CN.md)
- [安全说明](SECURITY.md)
