# CWapi v1.6.1 MCP Protocol

v1.6.1 只接受 MCP v2，不兼容 v1。

## Slack frame

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{...JSON...}
+++
```

response/event 使用同一前缀及 `MCP_RESPONSE` / `MCP_EVENT`。Subject 与 JSON `request_id` 必须一致。旧 `[CWapi/MCP/1]` 或 v1 schema/version 会收到迁移到 v2 的明确 guidance。

## Request

```json
{
  "schema": "cwapi.mcp.request.v2",
  "protocol_version": "cwapi-mcp/2",
  "request_id": "REQ-001",
  "repository_url": "https://github.com/owner/repo",
  "expected_commit": "0123456789abcdef0123456789abcdef01234567",
  "method": "mcpServer/tool/call",
  "params": {},
  "system_token": "optional-64-lowercase-hex"
}
```

允许的方法只有：

- `mcpServerStatus/list`
- `mcpServer/resource/read`
- `mcpServer/tool/call`

`repository_url` 与 `expected_commit` 必须成对出现。URL 只允许 ASCII `https://github.com/owner/repo[.git]`，不得有 userinfo、port、query、fragment、escape、反斜杠或额外 path。commit 必须是 mirror 中存在的完整 40hex commit object。

`system_token` 只能位于 request 顶层。非空值必须是 64 位 lowercase hex；错误类型、格式或嵌套位置统一在 request claim 前返回 `MCP_SYSTEM_TOKEN_INVALID`。

## Route 与 scope

| 调用 | scope | 约束 |
| --- | --- | --- |
| status-list | global | params 必须 `{}` |
| stock resource/tool | global 或 repository | caller 不得提供 `threadId` |
| `cwapi/process_start` | repository | 必须 repo + commit |
| `cwapi/process_status` | global | 只含 process_id |
| `cwapi/process_stop` | global | 只含 process_id |

tool-call outer params 必须且只能包含：

```json
{"server":"cwapi","tool":"process_start","arguments":{}}
```

未知 cwapi tool/resource 不会落入 stock relay；status-list 不伪造 cwapi server；`projects/list` 已删除。

## Process tools

`process_start` arguments：

```json
{
  "command": "cmd.exe",
  "argv": ["/d", "/c", "echo", "ok"],
  "cwd": "optional/repository/relative/path"
}
```

- `command` 必填，最多 32768 bytes；只接受 remote `/` path 表示。
- `argv` 可选，最多 256 项；单项 32768 bytes，总计 131072 bytes；不得含 NUL。
- `cwd` 可选，最多 512 字符，只能位于当前 repository tree，不能 traversal/reparse escape。
- PATH 在 Core 启动时固定快照，扩展只认 `.com/.exe/.cmd/.bat`。
- absolute/CWD-relative target 必须解析为 regular file；drive-relative path 拒绝。
- `.cmd/.bat` 使用验证过的 `%SystemRoot%/System32/cmd.exe` 与 request-private 固定 bridge，不信任 ComSpec。
- `.cmd/.bat` 参数中的 `"`、CR、LF 无法由 Windows batch 无损表示，会以 `INVOCATION_BATCH_ARGUMENT_UNREPRESENTABLE` 明确拒绝；其它已支持的空格与 cmd 元字符不得被改写。

status/stop arguments 必须严格为：

```json
{"process_id":"proc-0123456789abcdef01234567"}
```

公开 process record 只含 `process_id,state,backend,repository,expected_commit,working_directory,started_at,updated_at,exit_code?,stdout_tail,stderr_tail,error?`。状态只可能是 `starting/running/completed/failed/stopped`。

## Response 与 Token

```json
{
  "schema": "cwapi.mcp.response.v2",
  "protocol_version": "cwapi-mcp/2",
  "request_id": "REQ-001",
  "status": "completed",
  "result": {}
}
```

只有 `blocked + PERMISSION_DENIED` response 可以携带新 `system_token`。Token gate code 只有：

- `SYSTEM_TOKEN_INVALID_OR_EXPIRED`
- `SYSTEM_TOKEN_BINDING_MISMATCH`
- `SYSTEM_TOKEN_LIMIT_REACHED`

Token 最多同时 3 个、签发后 60 秒过期、一次性消费，绑定 repository identity、commit 和最终 executable+argv+real cwd。fallback 必须使用新 `request_id`、相同 repo/commit/params；CWapi 复用拒绝时的 dirty tree，不重新 Prepare。错误不回显 Token。

## 幂等与会话

fingerprint 为 `lowercase owner/repo + commit + method + canonical params`；global repository 为空，不含 `request_id/system_token`。同 request_id + 同 fingerprint 重投已保存 response；冲突返回 `MCP_REQUEST_ID_CONFLICT`。

status/stop 的 request_id 是一次快照，刷新必须换新 id。Token-bearing response 只在当前进程会话 durable；Slack 重连可重投但不延长 TTL。应用重启清空 request/delivery/cursor，不恢复 Token 或 process registry。
