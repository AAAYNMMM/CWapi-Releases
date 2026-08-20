# CWapi v1.6.0 Slack Transport

Slack 只负责远程传输，不负责本地执行权限判定。

## Runtime boundary

```text
Web GPT
 ↕
Slack Web API / Socket Mode
 ↕
CWapi Go Core
 ↕
stock Codex app-server
```

React 不直接访问 Slack；Codex 不拥有 Slack transport。

## Credentials

- App Token `xapp-...`；
- Bot Token `xoxb-...`；
- Channel ID `C...`。

Token 验证后存 Windows Credential Manager，不进入普通 config、SQLite、Git、argv、MCP body、artifact 或日志。

## Inbound

只做 transport/access 校验：

- configured team/channel；
- self-event filtering；
- envelope/message size；
- MCP schema/request_id；
- project_id/expected_commit 组合；
- duplicate/idempotency。

**不解析具体 MCP tool 参数来实施 filesystem/command 权限。** 这些权限由 Codex context/profile 执行。

## MCP envelope

```text
MCP_REQUEST
MCP_RESPONSE
MCP_EVENT
```

允许 request method：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

## Duplicate / current-session recovery

- CWapi 每次启动建立新运行会话，不读取或重放启动前的 channel history；
- 同一进程内 Socket reconnect 只补 durable cursor 之后的新消息；
- duplicate same request 在当前运行会话内不启动第二次调用；
- fingerprint 包含 project/commit/method/params；
- terminal duplicate 返回已有 compact response；
- 已上传 artifact 使用 response 中已有 Slack file 引用，不重新执行 MCP tool；
- ambiguous side-effect call 不自动 replay；应用重启直接丢弃上一会话未完成记录；
- Slack timestamp 不是业务幂等键。

## Size / files

Slack 不是 stdout 流，也不是 CWapi 的无限制网盘。

当前 outbound policy：

- 短文本直接进入 MCP response；
- 长文本/日志、图片、resource text/blob 使用 Slack external file upload；
- `files.getUploadURLExternal -> upload URL -> files.completeUploadExternal`；
- 单个 artifact 最大 8 MiB；
- 单次 MCP response 最多 16 个 artifact；
- 超限明确失败，不截断；
- CWapi 只上传 MCP 已经返回的 bytes/text，不根据 path/URI 私自读取本地文件。

因此 filesystem read permission 与 Slack upload permission 是两个独立层次。

## Logging / retention

Slack 只发送请求所需结果，不要求“最近 10 条”这种固定产品规则。GUI/SQLite 使用各自有界 retention；需要排障时按请求选择必要的小范围日志或由 MCP 返回的日志内容，再按 outbound policy 发送。

## Real gate

真实验收链路：

```text
external Web GPT / Slack identity
 -> Slack MCP_REQUEST(project_id + expected_commit)
 -> CWapi exact-commit context
 -> stock Codex app-server
 -> configured MCP server
 -> result
 -> Slack MCP_RESPONSE / Slack File
```

不得用 CWapi bot 自发自收冒充真实入站链路。
