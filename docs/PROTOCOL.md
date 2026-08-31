# CWapi 2.0 Protocol

2.0 使用 MCP Streamable HTTP 与一个 localhost OpenAI-compatible Provider。所有 listener 只绑定 `127.0.0.1`。

## MCP routes

```text
http://127.0.0.1:<mcp-port>/mcp/coding/<coding-token>
http://127.0.0.1:<mcp-port>/mcp/agent/<agent-token>
```

- route 必须 exact match；错误、缺失或交叉 token 返回 not found；
- 每条 route 使用独立 stateless `mcp.Server`；
- Coding route 只公布 Coding tools；Agent route 只公布 Agent tools；
- 未启用 Agent surface 时 Agent route 不暴露；
- Coding 与 Agent 只接受本文件定义的当前 2.0 MCP/API surface。

## OpenAI Secure MCP Tunnel

- `tunnel` 配置与 Coding route 绑定；`agent_tunnel` 配置与 Agent route 绑定；
- 两个 Tunnel 各自使用独立 Tunnel ID、Runtime API key、profile 和 `main` channel；
- profile 中的 `main` 只指向对应的 localhost route，不会把 Coding 与 Agent tool catalog 合并；
- ChatGPT Web 应选择 Tunnel 连接方式，不能直接填写 `127.0.0.1` 的 Server URL；
- 两组 Runtime API key 只进入各自 tunnel-client 子进程环境，不进入 config 或 profile 明文。

## Coding tools

Coding MCP 正式 tool catalog 固定为：

```text
coding_open
coding_exec
coding_status
coding_attachment
coding_close
```

### `coding_open`

```json
{
  "repository_url": "https://github.com/owner/repo",
  "target_ref": "refs/heads/feature",
  "expected_commit": "optional-full-40hex",
  "resume": false
}
```

返回 `repository,target_ref,resolved_commit,current_head,tracked_dirty,resumed,state`。MCP 公共协议不暴露 `coding_id`。CWapi 内部仍为每次 active Coding session 生成唯一 internal session ID，并维护 `canonical repository -> active internal session ID` 映射，用于生命周期、并发、取消与 stale-operation 防护。

一个 repository 同时最多一个 active Coding session。ChatGPT conversation 结束不是 CWapi session 终止信号，因此旧 conversation 消失后 active owner 可能继续存在。

当 repository 已有 active session：

- `resume=false` 返回 `CODING_WORKSPACE_BUSY`；
- 兼容的 `resume=true` 复用原 active internal session，不再次 prepare workspace，并返回 `resumed=true`；
- active command 正在运行时可返回 `state=busy`；
- target ref 不兼容返回 `CODING_RESUME_TARGET_MISMATCH`；
- supplied `expected_commit` 与 active session baseline 不一致返回 `CODING_RESUME_COMMIT_MISMATCH`；
- workspace 正在 opening 时返回 opening/busy 对应错误；
- session 正在 closing 时返回 `CODING_SESSION_CLOSING`。

`main` 与 `refs/heads/main` 视为同一 heads target。新 Web GPT conversation 只需要再次用同一 `repository_url` 调用兼容的 `coding_open(..., resume=true)`，无需知道旧随机 ID。

### `coding_exec`

```json
{
  "repository_url":"https://github.com/owner/repo",
  "command":"go",
  "argv":["test","./..."],
  "cwd":"optional/relative/path",
  "timeout_seconds":120
}
```

返回 `state,exit_code,stdout,stderr,truncated`。CWapi 将 `repository_url` 规范化后定位当前 active internal session。同一 internal session 同时只允许一个 active operation。`command` 与 `cwd` 的远端 path syntax 只接受 `/`；argv 逐项传递，不做 shell 拼接。

调用固定进入 bundled private Codex app-server 的 model-free `command/exec`。它不创建 thread/turn，不调用 Codex agent、auth、account 或 model API。SAFE 使用 `workspaceWrite`；FULL 使用 `dangerFullAccess`。

源码、Markdown、JSON、日志、配置等 workspace 内容通过 `coding_exec` 的 exact 读取命令交付给 Web GPT，而不是实例化成 ChatGPT 文件资源。

### `coding_status`

```json
{"repository_url":"https://github.com/owner/repo"}
```

返回 `state,repository,target_ref,resolved_commit,current_head,tracking_head,tracked_dirty,divergence,last_error`。该操作不 fetch，也不返回 Codex transcript。repository 没有 active session 时返回明确的 not-active 错误。

### `coding_attachment`

```json
{
  "repository_url":"https://github.com/owner/repo",
  "paths":["screenshots/result.png"]
}
```

该工具**仅用于 raster image**。成功时返回 repository、image metadata/`total_bytes`，并通过 MCP tool result 追加原生 `ImageContent`。如果任一 path 不是图片，返回 `CODING_ATTACHMENT_IMAGE_ONLY`。attachment label/URI 使用 repository 与 attachment metadata，不依赖公开 session ID。

CWapi 不通过该工具传输 Markdown、源码、JSON、日志、PDF、ZIP、DOCX 或其他普通文件，也不为这些文件生成 `EmbeddedResource`。

### `coding_close`

```json
{"repository_url":"https://github.com/owner/repo"}
```

关闭该 repository 当前 active internal session owner；active operation 会先被取消并等待收口。该操作不 reset/clean workspace，不删除 durable workspace，也不修改或删除用户未提交内容。没有 active session 时返回幂等友好的 `state=no_active_session`。

## Agent tools

### `agent_open`

输入 `{}`，返回 `state,resumed,max_inflight`。MCP 公共协议不暴露 `bridge_id`。CWapi 内部仍创建唯一 bridge generation；已有 active bridge 时续租并返回 `resumed=true`。

### `agent_exchange`

```json
{
  "capacity":4,
  "responses":[{
    "request_id":"request_...",
    "response":{"content":"...","finish_reason":"stop"}
  }]
}
```

一次调用自动绑定调用开始时的 current internal bridge generation，先逐项处理 `responses`，再领取下一批 request；没有 active bridge 时返回 `AGENT_BRIDGE_NOT_ACTIVE`。如果 exchange 期间 bridge 被 close/reopen，旧 operation 不得切换到新 generation。默认和最大 capacity 为 4，单个 request 与总 batch payload 均不得超过 1 MiB。

`request_id` 继续保留且必须精确关联，因为同一 batch 可有多个独立 request。若 request 包含合法 inline raster image，返回项可包含 image metadata；MCP tool result 同时追加按 `request_id` 关联的原生 `ImageContent`。不产生 `EmbeddedResource`。

`results[].state` 为 `completed/duplicate/rejected`；rejected 项带 `error`，不回滚其他项。CLAIMED request 在未完成时以相同 request ID、递增 `delivery` 重投。相同响应的重复提交返回 `duplicate`，不同响应返回 `AGENT_RESPONSE_CONFLICT`。response 可包含合法 `tool_calls`；JSON/JSON Schema response contract 会在完成前校验。

### `agent_close`

输入 `{}`。关闭当前唯一 internal bridge；其 queued/claimed requests 立即失败并释放。没有 active bridge 时返回幂等友好的 `state=no_active_bridge`。

## OpenAI-compatible Provider

鉴权：

```text
Authorization: Bearer <agent-api-key>
```

支持：

```text
GET  /v1/models
POST /v1/chat/completions
```

默认 model 为 `cwapi-web-gpt`。HTTP request body 上限 24 MiB，用于容纳 inline image 的 base64/JSON 开销；图片剥离后的单个 broker request 与每次 exchange 总 JSON payload 上限仍为 1 MiB。最大 active queue 16；默认整体 timeout 180 秒。没有 bridge 返回 503 `AGENT_BRIDGE_UNAVAILABLE`，queue 满返回 429 `AGENT_BUSY`，timeout 返回 504 `AGENT_REQUEST_TIMEOUT`。

`stream=false` 返回 chat completion JSON；`stream=true` 在等待 Web GPT 时发送 SSE comment keepalive，完成后返回协议合法的 chunks，最后为 `data: [DONE]`。它不是 token-level realtime streaming。

## Image transport boundary

CWapi 2.0 只保留**图片传输**，不提供通用文件传输。

### Coding

- `coding_attachment` 只接受 workspace raster image；
- MCP 只发 `ImageContent`；
- non-image attachment 请求返回 `CODING_ATTACHMENT_IMAGE_ONLY`；
- 普通文本/文件通过 `coding_exec` 读取需要的内容。

### Agent

- 只接受 Chat Completions message content 中 `type=image_url` 且 URL 为 `data:` URI 的 inline raster image；
- 顶层 CWapi `attachments` 文件扩展固定返回 `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`；
- MCP 只发 `ImageContent`，不发 `EmbeddedResource`；
- 图片 content part 在 broker 入队前替换为带 name/MIME/size/SHA-256 的文本占位，原始 bytes 仅写入 request-scoped 临时目录。

Agent 每个 request 最多 8 张图片，单张最多 8 MiB、合计最多 16 MiB；图片单边最多 2048 px。WebP 支持 VP8/VP8L/VP8X 尺寸解析，SVG 明确拒绝。请求完成、超时、客户端断开、bridge close、broker shutdown 以及下次 CWapi 启动都会清理临时图片。

CWapi 2.0 不把 ChatGPT 会话上传反向写入 repository 或本地软件，也不提供任意文件系统传输、批量同步或长期附件存储。
