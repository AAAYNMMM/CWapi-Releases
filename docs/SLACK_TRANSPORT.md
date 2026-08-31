# CWapi v1.6.3 Slack Transport

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

### MCP 大结果与文件

CWapi 已实现 Slack external upload flow：`files.getUploadURLExternal` -> 上传 bytes -> `files.completeUploadExternal`。文件完成后会共享到 configured channel，并在有 request thread 时挂到该 thread。

MCP delivery 会外置以下已有 MCP 内容：

- 超过 inline budget 的长文本；
- `content` 中 `type=image` 或 `type=audio` 的 base64 data；
- resource/resource contents 中已经由 MCP 返回的 blob 或 text；
- 超过 inline result budget 的完整 JSON result。

单个 Slack artifact 最大 8 MiB，每个 MCP response 最多 16 个 artifact。上传后的文件会追加到 MCP response `resources`，包含 URI、media type、SHA-256 与 size。

### Playwright 截图

需要通过 Slack 把 Playwright 截图交给 ChatGPT 时，优先调用 `browser_take_screenshot` 且**不传 `filename`**。不指定文件名时，Playwright 可以在 MCP result 中返回真正的 image content；CWapi 随后把 image bytes 自动外置为 Slack File。

如果指定 `filename`，Playwright 可能只在文本中返回本地保存路径。该路径不会自动触发 Slack 上传，因为 CWapi 不根据普通文本中的 path/URI 打开本地文件。这是安全边界，不是上传器故障。

因此调用方应区分：

```text
MCP image/blob bytes -> CWapi 可以上传 Slack File
普通文本中的 ./image.png -> CWapi 不读取、不上传
```

不要为绕过这个边界把任意本地路径改造成自动上传命令。真正需要传输的二进制内容应由对应 MCP tool 自己返回 bytes/image/resource content。

## Token exposure

Token-bearing response 可以存在 raw Slack/当前会话 SQLite。GUI/Wails snapshot 只把 v2 顶层 `system_token` 替换为 `[REDACTED]`。频道成员均被视为可信；Token 不按 Slack user/bot 身份二次绑定。

## 消息边界

- 单条 MCP envelope 有严格大小限制；
- 错误文本有界且不回显 Token；
- Slack 文件只用于已有 MCP 大结果，不因日志中出现 path/URI 自动读取本地文件；
- transport 不解释命令危险性，最终安全检查属于 Go Core/Codex/System 边界。
