# CWapi v1.6.1 Slack Transport

Slack 是 transport 与信任边界，不是 scheduler、权限数据库或 shell parser。

## 配置

- App Token 与 Bot Token：当前 Windows 用户 Credential Manager。
- Channel ID：`cwapi.config.v2` 的 `slack.channel_id`。
- Socket Mode 只接受 configured channel 的消息。

启动时 credential/channel 缺失只把 Slack 标记为 setup_required，不影响 GUI、global Core 或本地状态。Socket 断线由 supervisor 重连。

## Frame

```text
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
+++
{"schema":"cwapi.mcp.request.v2",...}
+++
```

closing frame 后的 Slack attribution metadata 不进入 JSON body。普通频道对话被忽略；看起来像 CWapi 协议但不完整/旧版的消息会收到 v2 guidance。

## Recovery

同一进程内：

- bounded in-memory index 保存已收/已发 protocol message；
- SQLite 保存 request terminal truth 与 delivery state；
- reconnect 从当前会话 cursor 后恢复新消息；
- duplicate response 可重投，但 System Token 原 expiry 不变。

进程重启：

- `ResetRuntimeSession` 清 request、delivery、execution/log/error 与 Slack cursor；
- 不恢复旧 response、Token 或 process registry；
- config、Credential Manager 和 Git mirror 保留；ephemeral worktree 在 startup sweep 清理。

## Delivery

CWapi 先 durable terminal response，再向 Slack post。post 失败把 delivery 标记 attention，不自动重跑可能有副作用的 MCP tool。调用方可重发相同 request_id 取得同一 response。

## Token exposure

Token-bearing response 可以存在 raw Slack/当前会话 SQLite。GUI/Wails snapshot 只把 v2 顶层 `system_token` 替换为 `[REDACTED]`。频道成员均被视为可信；Token 不按 Slack user/bot 身份二次绑定。

## 消息边界

- 单条 MCP envelope 有严格大小限制；
- 错误文本有界且不回显 Token；
- Slack 文件只用于已有 MCP 大结果，不因日志中出现 path/URI 自动读取本地文件；
- transport 不解释命令危险性，最终安全检查属于 Go Core/Codex/System 边界。
