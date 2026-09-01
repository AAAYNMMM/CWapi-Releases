# CWapi 2.0 Agent 指南

[English](AGENT_GUIDE.md) | [简体中文](AGENT_GUIDE.zh-CN.md)

CWapi Agent 让支持 OpenAI-compatible Chat Completions API 的本地软件，把模型侧交给 Web GPT。

它不是“把请求代理到 OpenAI API”的普通反向代理。本地请求先进入 CWapi broker，再通过 Agent MCP 送到 Web GPT；Web GPT 返回和 `request_id` 对应的结果后，CWapi 才把 OpenAI-compatible response 交还本地软件。

## 架构

```text
本地 OpenAI-compatible 软件
        ↓
POST /v1/chat/completions
        ↓
CWapi localhost Provider
        ↓
有界 Agent broker
        ↓
Agent MCP: agent_exchange
        ↓
ChatGPT Web / Web GPT
        ↓
content 和/或 tool_calls
        ↓
CWapi Provider response
        ↓
本地软件
```

本地 Provider 只绑定 `127.0.0.1`。

Web GPT 是唯一规划者和任务控制器。CWapi 只维护 bridge/OpenAI request 状态；本地软件执行自己的工具，并拥有 command/process session。MCP `request_id` 只是通信事务 ID，绝不是本地进程 session ID。

## 本地 Provider 配置

默认：

```text
Base URL: http://127.0.0.1:32123/v1
Model:    cwapi-web-gpt
API key:  CWapi 自动生成，在 GUI 中提供
```

如果改过 Agent port：

```text
http://127.0.0.1:<agent-port>/v1
```

实际 endpoints：

```text
GET  /v1/models
POST /v1/chat/completions
```

Provider 使用 `Authorization: Bearer <Agent API key>`。没有 key 或 key 不匹配会返回 HTTP 401 `invalid_api_key`。

## 当前支持的 request shape

Chat Completions 当前接受这些顶层字段：

```text
model
messages
tools
tool_choice
response_format
stream
```

`messages` 必填。`model` 为空时会规范化为：

```text
cwapi-web-gpt
```

未知顶层字段会被拒绝，而不是假装 CWapi 已经实现整个 OpenAI API 宇宙。因此某个具体版本的 Cline、Roo Code 或其它客户端能否完全兼容，应该实际测试，不能靠品牌名推理。

## Agent MCP 工具

Agent MCP 只暴露：

```text
agent_open
agent_exchange
agent_close
```

Web GPT 不接收公开 `bridge_id`。内部 bridge generation 由 CWapi 自己维护，exchange 和 close 会自动作用在当前 bridge。

## `agent_open`

连续 Agent 任务开始时调用一次 `agent_open()`。

没有 active bridge 时就打开一条；已经 active 时会恢复，并返回 `resumed=true`。

结果里包含 `max_inflight`，当前默认值是 4。

不要每来一个本地 API request 就开一条新 bridge。一条 active bridge 本来就是用来连续处理多个独立 request 的。

## `agent_exchange`

标准循环：

```text
agent_open()
        ↓
agent_exchange(capacity=4)
        ↓
拿到 0 个或多个 request
        ↓
Web GPT 针对准确 request_id 生成 response
        ↓
agent_exchange(responses=[...], capacity=4)
        ↓
CWapi 接收 responses，同时返回下一批 request
        ↓
继续循环
```

如果 `agent_open` 返回的 `max_inflight` 小于 4，用那个更小值。

`agent_exchange` 故意把“提交已完成 response + 等下一批 request”合成一个原子操作，减少没有意义的 MCP 往返。

不含 tool call 的 response 会立即以顶层 `state=responses` 确认。提交的 response 含 `tool_calls` 时，同一次 exchange 会继续 bounded wait，以便直接返回本地软件随后发出的 tool-result OpenAI request。本地 HTTP waiter 在 response 被接受时立即释放。

每个成功 exchange 都带结构化 `activity`：单调递增的状态 revision、changed/pending/inflight/active、连续不变 idle 次数、等待时长、最后 broker 状态/错误和 next-action 提示。`state=no_request` 只用于真实 bounded wait 超时且没有新 OpenAI request 的情况。

## `request_id`

每个本地模型 request 都有准确 `request_id`，它是公开关联键，必须原样保留。

因为可以同时存在多个 request，不能按“谁先来”“prompt 看起来像哪条”“我觉得大概是这个”来关联。软件工程已经够多玄学了，这里至少有 ID 可用。

`delivery > 1` 表示**同一个 request_id 被重新投递**，不是新任务。如果 Web GPT 已经完成它，就安全重发同一个完成 response，不要给同一个 request 编一个新答案。

returned request 还带 created/claimed/last-delivered/deadline 时间。本地 client 可以提供有界顶层 `metadata.task_id` / `metadata.correlation_id`；CWapi 保留这些 client-supplied 标识，但不会拿无关 request ID 猜测 task ID。

## 多请求与限制

当前 broker 默认：

```text
最大 active queue：16
最大 in-flight：    4
request timeout：   180 秒
exchange JSON batch：1 MiB
```

batch 里的 request 彼此独立时，Web GPT 可以一起分析提高效率，但返回时每条都必须绑定自己的准确 `request_id`。

某一个 response item 被拒绝，不会自动回滚同批已经成功接受的 sibling。修正被拒项以后继续即可。

## Function tools 与 `tool_calls`

本地 coding agent 往往会在 OpenAI-compatible request 中带一组 `tools`，期待模型决定调用哪些函数。

CWapi 保持标准循环：

```text
本地软件发送 tools
        ↓
Web GPT 决定调用哪些 function
        ↓
Web GPT response 返回 assistant tool_calls
        ↓
CWapi 把 tool_calls 返回本地软件
        ↓
本地软件执行自己的本地 tool
        ↓
本地软件在下一条 Chat Completions request 中发送 tool result
```

**Agent GPT 不直接执行第三方软件自己的本地工具。** 它负责返回 `tool_calls`；真正执行的是本地客户端，再由客户端把 tool message/result 发回模型循环。

### 多个 function 能不能一起返回？

如果同一个 request 里需要多个互不依赖的函数，而且参数都已经知道，就放进同一个 assistant `tool_calls` 数组一起返回。

只有后一个调用真的依赖前一个结果时才串行。把互不依赖的工具硬拆成很多模型轮次，只会反复重发 prompt 和 tool schema，机器和人类一起浪费时间。

Function `arguments` 可以使用 OpenAI JSON string，也可以直接传原生 JSON object；JSON response `content` 也可以是原生值。CWapi 会把它们规范化为 OpenAI-compatible string，减少 JSON 套 JSON 和 Windows 路径转义失败，同时保持等价 duplicate response 的幂等性。

## 文本回答

Web GPT 可以返回普通 assistant content。CWapi 会把 Agent response 规范化为本地客户端期待的 OpenAI-compatible Chat Completions message / finish_reason。

支持非 streaming 和 streaming。streaming 会输出 Chat Completions SSE chunks，并以 `[DONE]` 结束。

## 文件和媒体不支持

Agent 只接受文本与 tool JSON。如果本地软件发送这种顶层 attachment 扩展：

```json
{"attachments": [...]}
```

CWapi 返回：

```text
AGENT_FILE_ATTACHMENTS_UNSUPPORTED
```

文本文件、PDF、压缩包、Office 文档等普通文件不会被转换成 Agent MCP resource。

任何非文本 Chat Completions message content（包括 `image_url`）都会在进入 broker 前返回：

```text
AGENT_MEDIA_INPUT_UNSUPPORTED
```

`agent_exchange` 只输出 JSON request/result，不返回 MCP file、resource 或 image content。

ChatGPT 对话里手动上传的图片/文件也**没有反向通道**自动进入本地 OpenAI-compatible 客户端。本地客户端需要什么数据，就通过自己的 messages/tools 流程提供。

## `agent_close`

连续 Agent 任务真正结束时才调用 `agent_close()`。

某次 `agent_exchange` 返回 `no_request` 时不要因此关掉重开 bridge。它只表示该 wait window 内没有新的本地 OpenAI request，不表示任何本地命令仍在运行，也不单独构成再次等待的理由。只有另有已知 active process、外部条件或用户明确要求监控时才继续等待；连续两次 unchanged idle exchange 后，必须重新检查任务、真实进程状态和预期产物。

本地进程 exit 或明确报告 PASS/FAIL 后，该 process session 就是终态；停止 poll，并推进下一项验证或收口步骤。

bridge 被关闭后，在重新 `agent_open` 以前，本地新 request 没有可用的 Web GPT exchange 路径。

## Agent 的 Secure MCP Tunnel

Agent MCP 与 Coding MCP 是两条不同 surface。

本地 endpoint 类似：

```text
Coding MCP: http://127.0.0.1:<mcp-port>/mcp/coding/<coding-token>
Agent MCP:  http://127.0.0.1:<mcp-port>/mcp/agent/<agent-token>
```

两条链都要接 ChatGPT 时，配置**两条独立 OpenAI Secure MCP Tunnel**：

```text
Coding Tunnel -> 只转发 Coding MCP
Agent Tunnel  -> 只转发 Agent MCP
```

每条都有独立：

- Tunnel ID；
- Runtime API key；
- tunnel profile；
- `tunnel-client` process；
- 本地 MCP target。

不要把 Coding Tunnel 指向 Agent MCP，也不要在两个 GUI 面板共用一套 Tunnel credential。

## 本地 Provider 常见 HTTP 错误

### 401 `invalid_api_key`

本地客户端没发 Bearer key，或者 key 和 CWapi 当前 Agent API key 不一致。

### 400

request body 或受支持的 Chat Completions shape 无效，例如 JSON、messages、tools、tool choice、response format 或字段不合法。

### 413 `request_too_large`

HTTP request body 超过 24 MiB hard limit。

### 429 `AGENT_BUSY`

有界 broker queue 已满/忙。等 pending/claimed request 消化以后再送更多请求。

### 503 `AGENT_BRIDGE_UNAVAILABLE`

没有可用 active Agent MCP bridge。在 ChatGPT 用 `agent_open` 打开/恢复，并保持 exchange loop。

其它 broker unavailable 状态也可能映射成 503。

### 504 `AGENT_REQUEST_TIMEOUT`

Web GPT 没在 deadline 之前返回完整 response。当前默认 request timeout 是 180 秒。

## 本地 request 示例

```http
POST /v1/chat/completions
Authorization: Bearer <CWapi Agent API key>
Content-Type: application/json

{
  "model": "cwapi-web-gpt",
  "messages": [
    {"role": "user", "content": "Explain this error and propose a fix."}
  ]
}
```

这条 HTTP request 等待期间，ChatGPT 的 Agent 对话要保持 `agent_exchange`，broker 才能把它交给 Web GPT。

## Function tool 完整循环示例

```text
1. 本地 coding client 发送 messages + filesystem/search/edit tools。
2. CWapi 分配 request_id，agent_exchange 把 request 交给 Web GPT。
3. Web GPT 决定调用 search_files 和 read_file，返回 assistant tool_calls。
4. CWapi 把 tool_calls 返回本地 coding client。
5. 本地 client 自己执行这些函数。
6. client 发送新的 /v1/chat/completions，其中带 tool results。
7. Web GPT 通过 agent_exchange 收到新 request，继续推理。
```

具体本地工具属于客户端，不属于 CWapi Agent MCP。

## Agent 会自动调用 Coding 吗？

不会。Coding 与 Agent 可以同时运行，但它们是隔离 surface。Agent Web GPT bridge 不会偷偷获得 Coding tool catalog，本地 Agent request 也不会自动操作某个 CWapi Coding workspace。

如果一个 ChatGPT 工作流确实需要两个 app，就分别连接，并明确各自职责。

## Agent API key 保存在哪里

Agent Provider API key 是 `cwapi.config.v3` 的一部分，生成后保存在：

```text
CWapi-data/config/cwapi.json
```

GUI 会遮罩/提供它，用于本地客户端配置。

这和 **Agent Tunnel Runtime API key** 不一样。后者保存在 Windows Credential Manager。

## 相关文档

- [快速入门](GETTING_STARTED.zh-CN.md)
- [Coding 指南](CODING_GUIDE.zh-CN.md)
- [故障排查](TROUBLESHOOTING.zh-CN.md)
- [Protocol](PROTOCOL.md)
- [Security](SECURITY.md)
