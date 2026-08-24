# CWapi v1.6.1 Slack 配置教程

这份文档是 **CWapi 的 Slack 配置教程**。第一次创建 Slack App、配置 scopes、Socket Mode、token 和 Channel ID，都按这里做。

技术实现见 [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)；Web GPT 执行规则见 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)。

## 1. 先区分两个 Slack 连接

```text
A. CWapi 自己的 Slack App / Bot
   本机 CWapi 用它收请求、发结果、上传文件

B. ChatGPT 中连接的 Slack
   Web GPT 用它向控制频道发请求、读取 CWapi 返回
```

CWapi 的 `xapp-...` / `xoxb-...` 只填进 CWapi，不要发给 Web GPT。

## 2. 推荐 public 控制频道

推荐建立专用 public channel，例如 `#cwapi-control`。

App-Level Token scope：

```text
connections:write
```

Bot Token scopes：

```text
channels:read
channels:history
chat:write
files:write
```

Bot Event：

```text
message.channels
```

CWapi 使用 Socket Mode，不需要公网 Events API Request URL。

## 3. 创建 Slack App

1. 打开 `https://api.slack.com/apps`。
2. **Create New App -> From scratch**。
3. App Name 可填 `CWapi`。
4. 选择目标 Workspace。

## 4. 添加 Bot scopes

进入：

```text
OAuth & Permissions
-> Bot Token Scopes
```

添加：

```text
channels:read
channels:history
chat:write
files:write
```

修改 scopes 后必须 **Reinstall to Workspace**。

## 5. 开启 Event Subscriptions

进入：

```text
Event Subscriptions
-> Enable Events
-> Subscribe to bot events
```

添加：

```text
message.channels
```

## 6. 开启 Socket Mode 并创建 App Token

开启 **Socket Mode**，然后在：

```text
Basic Information
-> App-Level Tokens
-> Generate Token and Scopes
```

添加 `connections:write`，生成：

```text
xapp-...
```

## 7. 安装 App 并取得 Bot Token

进入：

```text
OAuth & Permissions
-> Install to Workspace
```

安装后复制：

```text
Bot User OAuth Token = xoxb-...
```

## 8. 创建控制频道并邀请 Bot

创建专用频道，例如 `#cwapi-control`，然后把 CWapi App / Bot 加入频道。

CWapi 使用 Channel ID，而不是频道显示名。浏览器中的 Slack 频道 URL 通常类似：

```text
https://app.slack.com/client/T01234567/C0123456789
```

其中 `C0123456789` 是 Channel ID。

## 9. 填入 CWapi

v1.6.1 使用固定单页 GUI。打开底部 Slack 卡片的配置 sheet，填写：

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

验证并保存后，Token 写入当前 Windows 用户 Credential Manager；配置文件只保存 Channel ID。

## 10. 判断是否连好

GUI 顶部 Slack 状态应进入 connected / healthy，并显示正确频道。

如果保存成功后很快 degraded，优先检查：

```text
channels:read
channels:history
message.channels
Socket Mode
Bot 是否在频道内
```

## 11. readiness 不会覆盖所有真实操作

配置验证不代表所有 scope 都被实际使用过。正式链路仍可能在以下动作暴露缺失权限：

```text
读取历史       -> channels:history
发送 response  -> chat:write
上传截图/文件  -> files:write
```

所以不要只看一个绿色状态，scope 清单必须完整。

## 12. 最终实际通信测试

v1.6.1 **没有 `projects/list`**。

第一次可以先做 global MCP catalog：

```text
mcpServerStatus/list
params = {}
```

正常链路：

```text
Web GPT
-> Slack 控制频道发送 [CWapi/MCP/2] request
-> CWapi MCP_RESPONSE
-> Web GPT 读取真实 response
```

之后再做 repository-scoped 调用，外层 request 携带：

```text
repository_url
expected_commit  # 完整 40 位 SHA
```

## 13. private channel

如果明确使用 private channel，额外需要：

```text
groups:read
groups:history
```

Bot Event：

```text
message.groups
```

修改后重新安装 App，并确保 Bot 已进入 private channel。

## 14. 常见错误

### `SLACK_APP_TOKEN_INVALID`
App Token 不是 `xapp-...`，或复制时包含空格 / 换行。

### `SLACK_BOT_TOKEN_INVALID`
Bot Token 不是 `xoxb-...`。

### `SLACK_READINESS_BOT_IDENTITY_FAILED`
Bot Token 无效、被撤销、Workspace 不匹配或 Slack API 不可访问。

### `SLACK_READINESS_CHANNEL_FAILED`
Channel ID 错、缺 read scope、Bot 没加入频道。

### `SLACK_BOT_NOT_CHANNEL_MEMBER`
把 CWapi Bot 加入控制频道。

### `SLACK_READINESS_SOCKET_FAILED`
检查 Socket Mode、App Token 与 `connections:write`。

### `SLACK_API_ERROR_missing_scope`
增加缺少的 scope 后重新安装 App。

### 能回文本但截图 / 文件失败
检查 `files:write`。另外 Playwright 截图要真正传回 Slack 时，`browser_take_screenshot` 不要指定 `filename`；详见 [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)。

## 15. Token 安全

`xapp-...` 与 `xoxb-...` 都按密码处理。不要 commit 到 GitHub、发给 Web GPT、放进 MCP command/argv、截图、日志或 issue。

如果 token 泄漏，应在 Slack App 管理页撤销并重新生成，而不是只删消息。删除聊天记录并不会让凭据获得神秘的失忆能力。
