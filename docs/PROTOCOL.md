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

返回 `state,exit_code,stdout,stderr,truncated`。CWapi 将 `repository_url` 规范化后定位当前 active internal session。同一 internal session 同时只允许一个 active operation。`command` 与 `cwd` 的远端 path syntax 只接受 `/`；argv 逐项传递，不做 shell 拼接。命令默认超时 120 秒，显式 `timeout_seconds` 范围为 1–600 秒，以容纳 clean-install/build gate。

调用固定进入 bundled private Codex app-server 的 model-free `command/exec`。它不创建 thread/turn，不调用 Codex agent、auth、account 或 model API。CWapi 在启动前检查解析后的最终 executable/argv/CWD。SAFE 使用 `workspaceWrite` 与命令级隔离环境；FULL 对所有通过永久策略的命令使用 `dangerFullAccess` 并恢复当前 Windows 用户 profile/AppData。网络能力独立、默认关闭；直接 FULL push 必须显式启用网络，且只有该路径恢复宿主 Git config/credential helper。

源码、Markdown、JSON、日志、配置等 workspace 内容通过 `coding_exec` 的 exact 读取命令交付给 Web GPT，而不是实例化成 ChatGPT 文件资源。

### `coding_status`

```json
{"repository_url":"https://github.com/owner/repo"}
```

返回 `state,repository,target_ref,resolved_commit,current_head,tracking_head,tracked_dirty,divergence,last_error`。该操作不 fetch，也不返回 Codex transcript。repository 没有 active session 时返回明确的 not-active 错误。

### `coding_close`

```json
{"repository_url":"https://github.com/owner/repo"}
```

关闭该 repository 当前 active internal session owner；active operation 会先被取消并等待收口。该操作不 reset/clean workspace，不删除 durable workspace，也不修改或删除用户未提交内容。没有 active session 时返回幂等友好的 `state=no_active_session`。

## Agent tools

### `agent_open`

输入 `{}`，返回 `state,resumed,max_inflight,state_revision`。MCP 公共协议不暴露 `bridge_id`。CWapi 内部仍创建唯一 bridge generation；已有 active bridge 时续租并返回 `resumed=true`。`state_revision` 是 broker 内单调递增的真实状态修订号，不是 task、request 或 command session ID。

### `agent_exchange`

```json
{
  "capacity":4,
  "responses":[{
    "request_id":"request_...",
    "response":{
      "tool_calls":[{
        "id":"call_1",
        "type":"function",
        "function":{"name":"inspect","arguments":{"path":"C:\\work\\repo"}}
      }],
      "finish_reason":"tool_calls"
    }
  }]
}
```

一次调用自动绑定调用开始时的 current internal bridge generation，并先逐项处理 `responses`；没有 active bridge 时返回 `AGENT_BRIDGE_NOT_ACTIVE`。已完成的 response 会立即释放对应本地 HTTP waiter，不会等到 exchange 返回。若所有成功 response 都是非-tool-call终态（`stop/length/content_filter`），exchange 立即返回 `state=responses` acknowledgement；若任一成功或 duplicate response 为 `finish_reason=tool_calls`，当前 exchange 继续 bounded-wait，本地软件执行工具后产生的下一轮 OpenAI request 可被直接返回。只有该 follow-up 等待超时且没有下一批时才返回 `state=no_request`。如果 exchange 期间 bridge 被 close/reopen，旧 operation 不得切换到新 generation。默认和最大 capacity 为 4，单个 request 与总 batch payload 均不得超过 1 MiB。

Top-level `state` 只有三种正常值：`requests` 表示同时返回了待处理 batch；`responses` 表示本次提交的 response 已处理且没有进入 follow-up wait；`no_request` 表示一次真实 bounded wait 已超时且没有新 request。三者都可同时携带逐项 `results` acknowledgement。

`request_id` 继续保留且必须精确关联，因为同一 batch 可有多个独立 request。`agent_exchange` 的 request 项包含 transaction ID、可选 client-supplied task/correlation ID、claimed 状态、delivery 与 created/claimed/last-delivered/deadline 时间，以及原始 OpenAI-compatible request；不会附带文件、resource 或 image content。

每个成功 output 都带结构化 `activity`：

```json
{
  "state":"no_request",
  "activity":{
    "revision":12,
    "changed":false,
    "pending":0,
    "inflight":0,
    "active":0,
    "idle_count":2,
    "waited_millis":45001,
    "last_state":"COMPLETED",
    "next_action":"reassess_before_waiting"
  }
}
```

- `revision` 在 bridge/open、enqueue、claim、terminal completion/failure 与 close 等真实 broker transition 时递增；redelivery 本身不伪造新状态；
- `changed` 表示相对该 bridge 上一次已返回 exchange，revision 是否变化；
- `pending/inflight/active` 是返回时真实 OpenAI request 数量，不代表第三方进程数；
- `idle_count` 只在连续 `no_request` 且 revision 未变化时递增，有 request 或真实 transition 时清零；
- `next_action` 为 `process_requests`、`advance_or_finish` 或 `reassess_before_waiting`，防止模型把消息时序误当成本地进度。

每个 returned request 带 `state=claimed`、`created_at`、`claimed_at`、`last_delivered_at` 与 `deadline_at`。`request_id` 继续保留且必须精确关联，因为同一 batch 可有多个独立 OpenAI request；它绝不是第三方 command/session ID。本地 client 如在 Chat Completions body 的 `metadata` 中提供字符串 `task_id` / `correlation_id`，CWapi 会保留 metadata，并把这两个稳定标识提升到 returned request；未提供时 CWapi 不猜测或伪造 task identity。request 交付只包含 JSON，不附带 MCP file、resource 或 image content。

`results[].state` 为 `completed/duplicate/rejected`；rejected 项带 `error`，不回滚其他项。CLAIMED request 在未完成时以相同 request ID、递增 `delivery` 重投。相同响应的重复提交返回 `duplicate`，不同响应返回 `AGENT_RESPONSE_CONFLICT`。response 可包含合法 `tool_calls`；`function.arguments` 可传 OpenAI 形式的 JSON string，也可直接传 native JSON object，CWapi 会 canonicalize 为 string。`content` 也可传 native JSON value 并序列化为 string。JSON/JSON Schema response contract 会在完成前校验；native/string 两种等价 arguments 使用 canonical fingerprint，避免仅因键顺序或转义形式不同产生假冲突。

`no_request` 的唯一语义是：本次 bounded wait 期间没有新的本地 OpenAI request。它不表示第三方 command 正在运行、不改变任何 command session 状态，也不单独构成再次等待的理由。Web GPT 必须从第三方工具的 process exit/status/output 判断 command 是否终态；明确 completed/failed/PASS/FAIL 后停止 poll 该 command session，并推进下一 pending/verification step。连续两个 unchanged idle output 后，在任何进一步 wait 前必须重新核对 task、process 与 expected artifact 状态。

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

默认 model 为 `cwapi-web-gpt`。HTTP request body 上限 1 MiB；单个 broker request 与每次 exchange 总 JSON payload 上限为 1 MiB。最大 active queue 16；默认整体 timeout 180 秒。没有 bridge 返回 503 `AGENT_BRIDGE_UNAVAILABLE`，queue 满返回 429 `AGENT_BUSY`，timeout 返回 504 `AGENT_REQUEST_TIMEOUT`。

2.0.3 的 Provider 不再把外部 JSON 直接交给 broker。正式转换链是：

```text
local Agent software
-> OpenAI Compatible Adapter
-> CWapi Canonical Format
-> Context Optimizer
-> MCP bridge
-> Web GPT
```

返回沿相反方向转换。Web GPT 是本地 Agent 软件实际使用且唯一负责推理的模型；本地软件只执行工具和本地操作，CWapi 不运行第二个 AI。

Canonical Format 只表示模型通信所需的 conversation/message/tool definition/tool call/tool result/completion/error/stream chunk。`system/developer/user/assistant/tool` role、`tool_call_id`、task/correlation metadata 会稳定保留；未识别的客户端私有字段不会进入 MCP context。Context Optimizer 是确定性代码，只规范化 metadata、JSON tool result 和可证明相同的重复 system/developer/tool 状态，不压缩或删除用户任务语义。

`GET /v1/models` 的 model item 额外描述当前 adapter 名称与能力：`streaming/tools/parallel_tools=true`，`images/files=false`。这是能力声明，不会恢复文件或图片实现。错误边界使用稳定代码区分 request JSON/role/content、capability、canonical conversion、tool mapping、Web GPT response 和 stream conversion；普通客户端不会收到 Go stack。

Provider 接受标准顶层 `metadata` object（最多 32 项；key 最长 64 字符；string value 最长 512 字符，也允许 number/bool/null）并原样交付 Web GPT。建议长任务由本地 client 提供稳定 `task_id` 与 `correlation_id`；CWapi 仍以每个 HTTP request 的随机 `request_id` 做精确事务关联，不从消息文本推断 task 或 command lifecycle。

`stream=false` 返回 chat completion JSON；`stream=true` 在等待 Web GPT 时发送 SSE comment keepalive，完成后由 canonical `StreamChunk` 经 adapter 返回协议合法的 buffered chunks，最后为 `data: [DONE]`。它不是 token-level realtime streaming，也不会伪造 token delta；adapter 同时保留双向 stream-chunk 转换接口供未来真实流式链路使用。

## File and media transport boundary

CWapi 2.0.3 不提供文件或图片传输。

### Coding

- 正式 tool catalog 中没有 `coding_attachment`；
- 普通源码、Markdown、JSON、日志等可读内容通过 bounded `coding_exec` 读取；
- Coding MCP 不发 `ImageContent` 或 `EmbeddedResource`。

### Agent

- 顶层 `attachments` 固定返回 `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`；
- Chat Completions message content 中任何非 `text` part，包括 `image_url`，固定返回 `AGENT_MEDIA_INPUT_UNSUPPORTED`；
- 请求在 broker 入队前完成上述拒绝，`agent_exchange` 不携带 attachment metadata，也不发 MCP file/image content。

CWapi 不把 ChatGPT 会话上传反向写入 repository 或本地软件，也不提供任意文件系统传输、批量同步或长期附件存储。
