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
load_skill
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
  "action":"run",
  "command":"go",
  "argv":["test","./..."],
  "cwd":"optional/relative/path",
  "timeout_seconds":120
}
```

省略 `action` 等价于兼容的 foreground `run`，返回 `state,exit_code,stdout,stderr,truncated`。`action=start` 启动 persistent process，并立即返回 `state=running,process_id,pid,started_at`；后续 `action=status` / `action=stop` 只需 `repository_url,process_id`，其调用有界且非阻塞。最多 16 个 persistent process；workspace close 与应用退出会停止其进程树。CWapi 将 `repository_url` 规范化后定位当前 active internal session。同一 internal session 同时只允许一个 active operation。`command` 与 `cwd` 的远端 path syntax 只接受 `/`；argv 逐项传递，不做 shell 拼接。foreground 命令默认超时 120 秒，显式 `timeout_seconds` 范围为 1–600 秒；persistent start 不接受 timeout。

调用固定进入 bundled private Codex app-server 的 model-free `command/exec`。它不创建 thread/turn，不调用 Codex agent、auth、account 或 model API。CWapi 在启动前检查解析后的最终 executable/argv/CWD。SAFE 使用 `workspaceWrite`、合成 profile、隔离配置与 workspace-lifetime cache；FULL 使用 `dangerFullAccess` 和剥离内部 secret 后的当前 Windows 用户开发环境。网络能力独立、默认关闭。Remote Git Rewrite 也是独立、默认关闭的高级能力，只放开 direct force/delete remote updates；危险 transport、receive-pack 注入、CWapi safety refs 与内部路径始终拒绝。

源码、Markdown、JSON、日志、配置等 workspace 内容通过 `coding_exec` 的 exact 读取命令交付给 Web GPT，而不是实例化成 ChatGPT 文件资源。

### `coding_status`

```json
{"repository_url":"https://github.com/owner/repo"}
```

返回 `state,repository,target_ref,resolved_commit,current_head,current_branch,detached,tracking_head,tracked_dirty,divergence,last_error`。当 `state=busy` 时额外返回 `active_action,active_command,active_started_at,active_elapsed_seconds`，用于确认当前 foreground operation 是否仍在正常运行。为避免命令参数中的 token、密钥或其它敏感值被状态接口回显，`argv` 不进入 busy metadata。该操作不 fetch，也不返回 Codex transcript。repository 没有 active session 时返回明确的 not-active 错误。

### `coding_close`

```json
{"repository_url":"https://github.com/owner/repo"}
```

关闭该 repository 当前 active internal session owner；active operation 会先被取消并等待收口。该操作不 reset/clean workspace，不删除 durable workspace，也不修改或删除用户未提交内容。没有 active session 时返回幂等友好的 `state=no_active_session`。

### `load_skill`

Coding 与 Agent 共用该工具。输入 Skill ID，返回 CWapi 启动时缓存的全局 Skill 内容；具体 Skill 不会默认塞进 MCP initialization context。修改 `prompts/` 后需要重启 CWapi。

## Agent tools

Agent MCP 对外暴露 4 个工具：`agent_open`、`agent_exchange`、`agent_close`、`load_skill`。`load_skill` 与 Coding 共用启动时缓存的全局 Skills。

### `agent_open`

输入 `{}`，返回 `state,resumed,max_inflight,state_revision`。公共协议不暴露 `bridge_id`。如果 bridge 已存在则续租；如果 bridge 曾断开但仍有 active request，则创建新的内部 generation 并返回 `resumed=true`，随后同一 request 以原 `request_id` 恢复投递。

### `agent_exchange`

输入可同时提交 `responses`、`progress` 与 `capacity`。response 可显式带 `event=tool_call|completion`；省略时 CWapi 根据 canonical completion 推导并保持兼容。

每个 returned request 除兼容 `state=claimed` 外，还带 `lifecycle_state`、`delivery`、`previous_state`、`resume_reason`、`last_activity`、created/claimed/last-delivered/deadline 时间和 `event`。`delivery > 1` 永远表示相同 `request_id` 的 redelivery/resume，不是新任务。

生命周期至少包括：`QUEUED`、`CLAIMED`、`RUNNING`、`WAITING_TOOL`、`COMPLETED`、`FAILED_RETRYABLE`、`FAILED_FINAL`。heartbeat 由 Broker runtime 自主维护，会延长仍在执行 request 的活动期限，但不伪造业务 revision；`progress` 是独立可选事件，也不是 completion。

`results[].state` 保持 `completed/duplicate/rejected` 兼容。可恢复的 response/tool-call decode 错误返回 `error_detail`：`code,message,request_id,tool_call_id,tool_name,retryable`，request 转入 `FAILED_RETRYABLE` 并可用相同 ID 修正重投。一个坏 response 不回滚同批其他成功项。

`function.arguments` 可为 OpenAI JSON string 或 native JSON object。非流式路径 canonicalize 一次；流式 tool-call delta 必须先按 index 完整拼接 arguments，再只做一次 JSON parse，避免 double parse/double escape。Windows 路径、LF/CRLF、Unicode、引号、反斜杠和大文本均属于正式回归范围。

成功 tool call 进入 `WAITING_TOOL` 并立即释放本地 HTTP waiter，使本地软件执行工具并发起下一轮 OpenAI request。工具自身失败作为正常 `tool_result` 回到下一轮，不自动杀死 Agent task。只有结构化 completion 才表示当前 request 完成，不能搜索助手文本中的“完成”等词。

每个 output 带 `activity`，保留 `pending/inflight/active`，并新增 `queued_requests/active_requests/last_heartbeat_at/last_progress`。`no_request` 的唯一含义仍是本次 bounded wait 内没有新的本地 OpenAI request。

bridge 生命周期与 request 生命周期分离。`agent_close` 只 detach 当前 bridge，保留 active request/history/delivery/last activity；重新 `agent_open` 后可继续同一 request。旧 bridge generation 的 operation 仍被拒绝，不能跨 generation 完成 request。新 OpenAI HTTP request 在 bridge 完全离线时仍快速失败 503，不进入等待队列。

### `agent_close`

输入 `{}`。关闭当前 bridge handle，但不删除 active request。没有 active bridge 时返回 `state=no_active_bridge`。

### `load_skill`

输入 `{"name":"debugging"}` 等 Skill ID，返回启动时缓存的 Skill name/description/content。Skill 文件修改后需重启 CWapi。缺失 Skill 返回 `SKILL_NOT_FOUND`。

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

2.0.5 的 Provider 不把外部 JSON 直接交给 broker。正式转换链是：

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

CWapi 2.0.5 不提供文件或图片传输。

### Coding

- 正式 tool catalog 中没有 `coding_attachment`；
- 普通源码、Markdown、JSON、日志等可读内容通过 bounded `coding_exec` 读取；
- Coding MCP 不发 `ImageContent` 或 `EmbeddedResource`。

### Agent

- 顶层 `attachments` 固定返回 `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`；
- Chat Completions message content 中任何非 `text` part，包括 `image_url`，固定返回 `AGENT_MEDIA_INPUT_UNSUPPORTED`；
- 请求在 broker 入队前完成上述拒绝，`agent_exchange` 不携带 attachment metadata，也不发 MCP file/image content。

CWapi 不把 ChatGPT 会话上传反向写入 repository 或本地软件，也不提供任意文件系统传输、批量同步或长期附件存储。
