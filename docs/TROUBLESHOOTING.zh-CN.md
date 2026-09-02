# CWapi 2.0 故障排查

[English](TROUBLESHOOTING.md) | [简体中文](TROUBLESHOOTING.zh-CN.md)

按你实际看到的现象往下找。如果从来没有成功连接过，先对照 [快速入门](GETTING_STARTED.zh-CN.md) 检查基本配置。

## ChatGPT 看不到 MCP 工具

**现象**

Coding app 没有出现 `coding_open` / `coding_exec` / `coding_status` / `coding_close`，或者 Agent app 没有出现 `agent_open` / `agent_exchange` / `agent_close`。

**可能原因**

连错了 MCP app/Tunnel、Tunnel 没真正运行，或者当前 ChatGPT Workspace 没启用需要的自定义 MCP 能力。

**检查什么**

- Coding 和 Agent 应使用两条不同 Tunnel。
- CWapi 对应 Tunnel 应是已连接/运行，而不是持续“正在重连”。
- ChatGPT 走的是 Secure MCP Tunnel，不是直接填 `127.0.0.1` Server URL。
- 发现的 tool list 必须和上面的公开工具完全对应。

**怎么修**

重新连接正确的 Tunnel/app。工具发现还失败时，先只排查 Tunnel ID、Runtime API key 和 app 对应关系，不要还没通 MCP 就开始折腾 repository state。

## Tunnel 无法连接

**现象**

Coding 或 Agent Tunnel 一直无法进入 connected 状态。

**可能原因**

Tunnel ID 不寻、Runtime API key 缺失/无效、出站网络被拦，或者 bundled tunnel runtime 缺失/损坏。

**检查什么**

- Tunnel ID 确实属于当前这条 Coding 或 Agent Tunnel。
- Runtime API key 对这条 Tunnel 有权限。
- portable 中存在 `runtime/tunnel/current/tunnel-client.exe`。
- 防火墙/代理允许 tunnel-client 建立出站连接。

**怎么修**

在正确的 CWapi Tunnel 面板重新填写匹配的 Tunnel ID 和 Runtime API key。Runtime API key 不应手工写进 `cwapi.json`，CWapi 会把它存到 Windows Credential Manager。

## Tunnel 一直重连

**现象**

GUI 反复显示“正在重连”，或者 tunnel-client 启动后很快退出再拉起。

**可能原因**

进程能启动，但认证/网络维持失败，或者本地 target/profile 配错。

**检查什么**

- 当前重连的是不是你真正配置的那条链。
- Coding Tunnel 只指向 Coding MCP，Agent Tunnel 只指向 Agent MCP。
- 保存的 Tunnel ID 是否正确。
- 对应 Runtime API key 是否还在 Windows Credential Manager。

**怎么修**

修正这条 Tunnel 自己的配置后重新连接，不要把另一条链的 Tunnel 设置复制过来。

## Runtime API key 错误

**现象**

Tunnel ID 看起来正确，但 tunnel-client 认证失败。

**可能原因**

Runtime API key 填错、失效、被撤销，或者权限不足。

**检查什么**

Coding key target：

```text
CWapi/2.0/OpenAI/Tunnel/APIKey
```

Agent key target：

```text
CWapi/2.0/OpenAI/Tunnel/Agent/APIKey
```

**怎么修**

为目标 Tunnel 使用有效 Runtime API key，并通过对应 CWapi Tunnel 面板保存，让它覆盖正确的 Credential Manager 条目。

## `CODING_WORKSPACE_BUSY`

**现象**

`coding_open(..., resume=false)` 返回 `CODING_WORKSPACE_BUSY`。

**可能原因**

这个 canonical repository 已经有 active Coding session。

**检查什么**

调用 `coding_status(repository_url)`，确认当前 active 的确是你想继续的仓库/任务。

**怎么修**

继续同一个兼容任务时使用 `coding_open(..., resume=true)`。旧任务真的结束了，就先 `coding_close`。不要通过改 URL 拼法来绕过 ownership。

## Codex runtime unavailable

**现象**

Coding 可以 open，但 `coding_exec` 因 bundled Codex command toolhost/runtime 不可用而无法启动。

**可能原因**

portable 只复制了一部分、runtime 文件缺失，或者安全软件隔离了 bundled executable。

**检查什么**

- 是否完整解压，而不是只拿了一个 `CWapi.exe`。
- `runtime/codex/current` 下的 pinned Codex runtime 是否还在。
- 安全软件有没有隔离它。

**怎么修**

把正式 `CWapi-v2.0.4.zip` 完整重新解压到一个干净、当前用户可写目录。不要随便拿另一个 Codex 安装去替换 bundled runtime。

## private Git clone/fetch/push 失败

**现象**

公开仓库正常，private repository 报 Git authentication 错误。

**可能原因**

当前 Windows 用户没有可用的 Git/GitHub credential。

**检查什么**

用同一个 Windows 用户在 CWapi 外部测试正常 Git 工具是否能访问这个 private repository。

**怎么修**

修复 Windows 用户自己的 Git/GitHub credential。CWapi 不拿 Codex login 当 Git 认证，也不要把 GitHub token 发进 prompt 或仓库文件。

## SAFE 下 commit/push 失败

**现象**

改文件、build、test 正常，但 `git commit`、`git push` 或其它 Git metadata 写入在 SAFE 下失败。

**可能原因**

SAFE 本来就会保护 `.git` metadata 和更广 host access。

**检查什么**

先确认用户已经明确授权这次 Git 写操作，并检查 staged files 是否正确。

**怎么修**

切到 `FULL` 后执行准确的已授权 Git 命令。普通工作再切回 SAFE。已经运行中的 command 保持启动时 profile。

## 新 conversation 无法恢复 workspace

**现象**

`coding_open(..., resume=true)` 没恢复到预期 workspace。

**可能原因**

repository URL / target ref / expected commit 与现有 workspace metadata 不兼容，移动 portable 时没带 `CWapi-data`，或者请求的 resume 条件已经和旧状态不一致。

**检查什么**

- 使用同一个 canonical repository URL。
- `target_ref` 与原 workspace 兼容。
- 如果传了 `expected_commit`，它应与保存的 resolved commit 一致。
- 当前 CWapi 目录里确实还有原来的 `CWapi-data/workspaces`。

**怎么修**

用匹配参数 resume。真想从新 baseline 开始时，先保存重要本地工作，再通过 CWapi maintenance 重建 workspace。

## 仍然出现旧的 `coding_attachment` 工具

**现象**

ChatGPT Coding app 显示 5 个工具，或仍列出 `coding_attachment`。

**可能原因**

当前连接的是旧版 server/catalog，而不是 CWapi 2.0.4 的 Coding route。

**怎么修**

确认 CWapi 2.0.4 和 Coding Tunnel 正在运行，重新连接 Coding app，并核对准确的 4 工具 catalog。Coding MCP 已不再传输文件或图片。

## `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`

**现象**

本地 Agent 请求返回 `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`。

**可能原因**

客户端发送了 CWapi 不支持的 generic top-level `attachments` 文件扩展。

**检查什么**

看客户端实际 request shape。Agent 只接受文本与 tool JSON。

**怎么修**

去掉顶层 `attachments` 字段。上下文改用文本提供，或由本地软件自己的工具读取本地数据。

## `AGENT_MEDIA_INPUT_UNSUPPORTED`

**现象**

本地 Agent 请求返回 `AGENT_MEDIA_INPUT_UNSUPPORTED`。

**可能原因**

Chat Completions message content 中存在非 `text` part，例如 `image_url`。

**怎么修**

只发送文本 message content 与 tool JSON。CWapi 不会通过 Agent MCP 入队或返回文件/图片 content。

## Agent Provider 401 `invalid_api_key`

**现象**

`GET /v1/models` 或 `POST /v1/chat/completions` 返回 HTTP 401。

**可能原因**

本地客户端没带 Agent Provider API key，或者用的是旧 key/错 key。

**检查什么**

这里需要的是 CWapi GUI 提供、保存在 `CWapi-data/config/cwapi.json` 的 Agent API key，不是 Tunnel Runtime API key。

**怎么修**

把本地客户端的 Bearer/API-key 配置换成 CWapi 当前 Agent Provider API key。

## Agent Provider 429 `AGENT_BUSY`

**现象**

本地客户端收到 HTTP 429 `AGENT_BUSY`。

**可能原因**

有界 Agent broker 已满/忙。当前默认最多 4 个 in-flight，queue 为 16。

**检查什么**

确认 ChatGPT Agent bridge 正在持续跑 `agent_exchange`，之前的 request 也在被完成。

**怎么修**

降低本地并发，让 pending/claimed request 消化掉。不要靠开多条互相竞争的 Agent bridge 来处理 queue pressure。

## Agent Provider 503

**现象**

本地客户端收到 HTTP 503，常见为 `AGENT_BRIDGE_UNAVAILABLE` / unavailable。

**可能原因**

没有可用 active Agent MCP bridge，Agent Tunnel/app 已断开，或者 exchange loop 没在运行。

**检查什么**

- Agent Tunnel 连接的是 Agent MCP。
- ChatGPT 里能看到 `agent_open`、`agent_exchange`、`agent_close`。
- 已调用 `agent_open()` 打开/恢复 bridge。
- 连续任务期间对话仍持续使用 `agent_exchange`。

**怎么修**

恢复 Agent Tunnel/app，调用 `agent_open`，并保持 exchange loop。

## Agent Provider 504 `AGENT_REQUEST_TIMEOUT`

**现象**

本地 Chat Completions request 等待后返回 HTTP 504。

**可能原因**

request 没在当前默认 180 秒 timeout 内得到完整 Web GPT response。

**检查什么**

确认 `agent_exchange` 收到了这条 request，并且 response 用准确 `request_id` 返回。

**怎么修**

保持 Agent exchange loop，确保每条 request 在 deadline 内完成；可以的话减少过大、过于复杂的单次客户端 turn。

## 本地软件找不到 `cwapi-web-gpt`

**现象**

客户端提示 model 不存在，或者 model picker 为空。

**可能原因**

Base URL 填错、客户端实际没使用你配置的 custom provider，或者读取 `/v1/models` 前认证就失败了。

**检查什么**

应使用：

```text
Base URL: http://127.0.0.1:<agent-port>/v1
Model:    cwapi-web-gpt
```

CWapi 实现的是 `GET /v1/models` 与 `POST /v1/chat/completions`。

**怎么修**

修正 custom OpenAI-compatible provider 和 Agent API key。如果客户端强制调用 CWapi 没实现的 endpoint/字段，这个客户端版本可能并不兼容 2.0。

## Coding / Agent Tunnel 配反了

**现象**

连接似乎存在，但 ChatGPT 发现的是另一套 tool catalog，Coding/Agent 行为像被交换了。

**可能原因**

Coding Tunnel 指向了 Agent MCP，或 Agent Tunnel 指向了 Coding MCP。

**检查什么**

Coding 必须恰好暴露 5 个 `coding_*` tool；Agent 必须恰好暴露 3 个 `agent_*` tool。

**怎么修**

让每条 Tunnel 回到自己的 local MCP target，并分别使用 Tunnel ID、Runtime API key 和 profile。

## 移动 portable 后 workspace 像是“不见了”

**现象**

CWapi 在新目录能启动，但以前的 workspace 看不到。

**可能原因**

只移动了干净程序/runtime，新目录因此创建了新的 `CWapi-data`。

**检查什么**

去旧解压目录找：

```text
CWapi-data/workspaces/
```

**怎么修**

先确认旧目录中的 data root 是否包含需要的 workspace。要保持 portable 状态一起搬迁，就移动包含 `CWapi-data` 的整个解压目录。

## `CWapi-data` 是否可以清理？

**现象**

想重置 CWapi 或腾空间，准备处理 `CWapi-data`。

**可能原因**

`CWapi-data` 不只是 cache，里面还有 config、workspace metadata、durable repository 和 runtime state。

**检查什么**

操作前检查是否存在未提交/未 push 的 workspace；同时记住 MCP tokens、Agent API key、Tunnel IDs 也保存在 config 中。

**怎么修**

优先用 CWapi 的定向 maintenance 操作处理单个 workspace。若确实要整体重置数据目录，先备份需要的工作，并准备重新配置 CWapi。

## 还没解决

只收集真正相关的信息：错误码、CWapi/Tunnel 当前状态、适用时的 `coding_status`，以及 Agent 本地 HTTP status。不要公开 MCP token、Agent API key、Tunnel Runtime API key 或 private repository credential。

同时可参考 [常见问题](FAQ.zh-CN.md)、[Coding 指南](CODING_GUIDE.zh-CN.md)、[Agent 指南](AGENT_GUIDE.zh-CN.md) 和 [Operations](OPERATIONS.md)。
