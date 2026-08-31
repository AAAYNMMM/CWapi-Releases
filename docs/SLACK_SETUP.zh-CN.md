# CWapi 1.6.3 Slack 配置

[English](SLACK_SETUP.md) | [简体中文](SLACK_SETUP.zh-CN.md)

CWapi 1.6.3 通过 Slack Socket Mode 收控制请求、发 response 和上传文件，不需要公网 Events API Request URL。

## 先分清两个 Slack 连接

```text
A. CWapi 自己的 Slack App / Bot
   本机 CWapi 用它收 request、发 response、上传文件。

B. ChatGPT 里的 Slack 连接
   Web GPT 用它向控制频道发 request，并读取 CWapi 返回。
```

CWapi 使用的 `xapp-...` 和 `xoxb-...` 只放在本机 CWapi 里，不要发给 Web GPT。

## 推荐 public 控制频道

建议单独建一个 public channel，例如 `#cwapi-control`。

### App-Level Token scope

```text
connections:write
```

### Bot Token scopes

```text
channels:read
channels:history
chat:write
files:write
```

### Bot Event

```text
message.channels
```

这些权限分别负责：

```text
Socket Mode 连接 -> connections:write
识别/读取频道    -> channels:read
读取历史         -> channels:history
发送 response    -> chat:write
上传截图/文件    -> files:write
接收 public 消息 -> message.channels
```

## 创建 Slack App

1. 打开 `https://api.slack.com/apps`。
2. 选择 **Create New App -> From scratch**。
3. App Name 可以填 `CWapi`。
4. 选择目标 Workspace。

## 添加 Bot Token Scopes

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

以后只要修改 scopes，就记得重新安装 App 到 Workspace，否则 token 不会自动学会新权限。

## 开启 Event Subscriptions

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

## 开启 Socket Mode 并生成 App Token

开启 **Socket Mode**，再进入：

```text
Basic Information
-> App-Level Tokens
-> Generate Token and Scopes
```

添加：

```text
connections:write
```

生成 `xapp-...` App Token。

## 安装 App 并取得 Bot Token

进入：

```text
OAuth & Permissions
-> Install to Workspace
```

安装后复制：

```text
Bot User OAuth Token = xoxb-...
```

## 创建控制频道并邀请 Bot

建立专用 channel，并把 CWapi Bot 加进去。

CWapi 使用 Channel ID，不使用频道显示名。Slack 浏览器地址通常类似：

```text
https://app.slack.com/client/T01234567/C0123456789
```

最后的 `C0123456789` 就是 Channel ID。

## 在 CWapi 保存凭据

Slack 配置区域填写：

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

CWapi 会先验证候选凭据。App/Bot Token 保存到当前 Windows 用户 Credential Manager；Channel ID 是写入 JSON config 的 Slack 字段。

## 验证是否连接成功

Slack 状态应进入 healthy/connected，并显示正确频道。

不过一个绿色状态并不能证明所有真实动作都试过了。第一次端到端测试最好同时确认：

- 能接收到控制频道消息；
- 能按需读取 request/history；
- 能发送 response；
- 如果要传文件，能正常上传文件。

## 第一次协议测试

1.6.3 没有 project registry，也没有 `projects/list`。

可以先发 `mcpServerStatus/list`，params 为空。Web GPT 通过配置的 Slack channel 发送 `[CWapi/MCP/2]` request，CWapi 应在对应 thread 返回 `MCP_RESPONSE`。

之后 repository request 再在外层带上 GitHub `repository_url` 和完整 40 位 `expected_commit`。

## Private channel

如果明确使用 private channel，还需要：

```text
groups:read
groups:history
```

并订阅：

```text
message.groups
```

修改后重新安装 App，并确认 Bot 已加入这个 private channel。

## 常见 Slack 错误

### `SLACK_APP_TOKEN_INVALID`

不是有效的 `xapp-...`，或者复制时带了空格/换行。

### `SLACK_BOT_TOKEN_INVALID`

不是有效的 `xoxb-...` Bot User OAuth Token。

### `SLACK_READINESS_BOT_IDENTITY_FAILED`

Bot Token 无效、已撤销、属于别的 Workspace，或者 Slack API 无法访问。

### `SLACK_READINESS_CHANNEL_FAILED`

检查 Channel ID、频道读取 scope，以及 Bot 是否真的在频道里。

### `SLACK_BOT_NOT_CHANNEL_MEMBER`

把 Bot 加入当前配置的控制频道。

### `SLACK_READINESS_SOCKET_FAILED`

检查 Socket Mode、App Token 与 `connections:write`。

### `SLACK_API_ERROR_missing_scope`

补上缺少的 scope，然后重新安装 App 到 Workspace。

### 文本能回，截图/文件上传失败

检查 `files:write`。Playwright 截图要通过 Slack 返回时，`browser_take_screenshot` 不要传 `filename`，让它返回真实 image bytes 给 CWapi。

## Token 安全

把 `xapp-...` 和 `xoxb-...` 当密码处理。不要 commit，不要发给 ChatGPT，不要放进 CWapi command/argv，不要贴到 issue，也不要出现在截图或日志里。

如果 token 泄漏，去 Slack 管理页撤销/轮换。删掉那条消息并不会让 token 自动失效。
