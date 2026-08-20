# CWapi v1.6.0 Slack 配置教程

这份文档是 **CWapi 的 Slack 配置唯一权威教程**。如果你只是想把 CWapi 第一次连上 Slack，按这里一步一步做即可。

技术实现、消息协议、重连和文件上传机制见 [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)；Web GPT 的执行规则见 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)。

## 1. 先理解两个不同的 Slack 连接

CWapi 使用 Slack 时有两件彼此独立的事：

```text
A. CWapi 自己的 Slack App / Bot
   本机 CWapi 用它接收请求、发送结果和上传文件

B. ChatGPT 中连接的 Slack
   Web GPT 用它向控制频道发送请求、读取 CWapi 返回结果
```

CWapi 的 `xapp-...` / `xoxb-...` token 只填进 CWapi，不要发给 Web GPT，也不要放进普通 Slack 消息。

## 2. 推荐的最简单配置

推荐为 CWapi 建一个专用 **公共频道**，例如：

```text
#cwapi-control
```

公共频道需要的 Slack Bot scopes 最少、配置也最直观。CWapi 仍要求自己的 bot 已经加入这个频道；它不会因为频道是 public 就自动把自己当成员。

推荐配置：

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

这四个 Bot scopes 分别对应 CWapi 当前真实调用：

```text
channels:read      -> conversations.info
channels:history   -> conversations.history + 公共频道 message event
chat:write         -> chat.postMessage / chat.delete
files:write        -> files.getUploadURLExternal / files.completeUploadExternal
```

CWapi 使用 Socket Mode，因此不需要为 Events API 准备公网 Request URL。

## 3. 创建 Slack App

1. 打开 `https://api.slack.com/apps`。
2. 点击 **Create New App**。
3. 选择 **From scratch**。
4. App Name 可以填写：

```text
CWapi
```

5. 选择你准备给 CWapi 使用的 Slack Workspace。
6. 创建后进入这个 App 的管理页面。

如果你的 Workspace 禁止普通成员创建或安装 App，需要 Workspace Owner / Admin 授权。这是 Slack Workspace 自己的策略，不是 CWapi 限制。

## 4. 添加 Bot Token Scopes

左侧进入：

```text
Features
└─ OAuth & Permissions
```

找到：

```text
Scopes
└─ Bot Token Scopes
```

依次点击 **Add an OAuth Scope**，添加：

```text
channels:read
channels:history
chat:write
files:write
```

不需要为了让 CWapi 在专用频道发消息额外添加 `chat:write.public`。正确做法是把 CWapi Bot 加入控制频道。

如果后面新增或修改了 scope，记得重新 **Reinstall to Workspace**，否则旧 `xoxb-...` token 可能还没有新权限。

## 5. 打开 Event Subscriptions

左侧进入：

```text
Features
└─ Event Subscriptions
```

1. 打开 **Enable Events**。
2. 找到 **Subscribe to bot events**。
3. 点击 **Add Bot User Event**。
4. 添加：

```text
message.channels
```

因为 CWapi 使用 Socket Mode，所以这里不需要填写公网 Request URL。

CWapi 当前只处理目标控制频道里的 `message` event，并会过滤自己的 bot 消息，避免自己回复自己然后陷入数字版回音室。

## 6. 打开 Socket Mode

左侧进入：

```text
Settings
└─ Socket Mode
```

打开：

```text
Enable Socket Mode
```

Socket Mode 让本机 CWapi 主动通过 WebSocket 连接 Slack，因此不需要让你的电脑暴露公网 HTTP 服务。

## 7. 创建 App Token（xapp-...）

进入：

```text
Settings
└─ Basic Information
```

找到：

```text
App-Level Tokens
```

然后：

1. 点击 **Generate Token and Scopes**。
2. Token Name 可以写：

```text
CWapi Socket
```

3. 点击 **Add Scope**。
4. 添加：

```text
connections:write
```

5. 点击 **Generate**。
6. 复制生成的 token。

它应当以：

```text
xapp-
```

开头。

这个就是 CWapi 设置页面里的 **App Token**。

## 8. 安装 App 并取得 Bot Token（xoxb-...）

回到：

```text
OAuth & Permissions
```

点击：

```text
Install to Workspace
```

然后在 Slack 授权页面点击 **Allow**。

安装成功后，在同一个页面会出现：

```text
Bot User OAuth Token
```

它应当以：

```text
xoxb-
```

开头。

复制它，这就是 CWapi 设置页面里的 **Bot Token**。

### 修改 scopes 后

如果你后来又增加了 `files:write`、`channels:history` 等权限，回到这个页面点击：

```text
Reinstall to Workspace
```

否则 Slack 很可能继续使用旧授权。权限页面看起来全对、程序却报 `missing_scope`，这种人类熟悉的快乐通常就来自这里。

## 9. 创建控制频道

在 Slack 中创建一个专用频道，例如：

```text
#cwapi-control
```

推荐使用 public channel，这样只需要前面的 `channels:*` scopes。

然后把刚才创建的 **CWapi App / Bot 加入频道**。

可以在频道详情里的 Integrations / Apps 中添加，也可以使用 Slack 提供的邀请 App 入口。最终目标只有一个：

> CWapi Bot 必须是这个频道的成员。

CWapi 在保存 Slack 配置时会调用 `conversations.info`，并明确检查 `is_member`。Bot 没进频道时配置不会被视为正常。

## 10. 找到 Channel ID

CWapi 要的是：

```text
C0123456789
```

而不是：

```text
#cwapi-control
```

最稳定的找法：

1. 在浏览器打开目标 Slack 频道。
2. 看地址栏。
3. URL 通常类似：

```text
https://app.slack.com/client/T01234567/C0123456789
```

其中：

```text
T01234567    = Workspace ID
C0123456789  = Channel ID
```

复制 `C...` 这一段。

如果是 private channel，频道 ID 通常仍然是 conversation ID，但权限配置要按下面的“私有频道”章节额外调整。

## 11. 把三个值填进 CWapi

打开 CWapi：

```text
设置
└─ Slack
└─ 更换 Slack 配置
```

填写：

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

然后点击：

```text
验证并保存
```

当前 CWapi 保存前会实际验证：

```text
Bot Token 身份       -> auth.test
目标频道和成员状态   -> conversations.info
Socket Mode          -> apps.connections.open + WebSocket hello
```

验证成功后 token 写入当前 Windows 用户的 **Credential Manager**，不会写进普通配置文件。

## 12. 怎么判断 Slack 已经连好

保存后至少检查：

### 设置页

应该能看到正确的：

```text
Workspace
控制频道名称
Channel ID
```

### 控制台

重点看：

```text
Slack Transport = connected / healthy
```

### 诊断页

不应持续出现：

```text
SLACK_READINESS_...
SLACK_API_ERROR_...
SLACK_SOCKET_...
```

如果 Socket 连上后马上变成 degraded，优先检查 `channels:history` 和 Event Subscription，因为 CWapi reconnect / recovery 会读取频道历史。

## 13. “验证并保存”通过，不代表所有 scopes 都已经测试过

当前保存时的 readiness probe 会验证身份、频道和 Socket Mode，但不会真的：

```text
发一条正式 MCP response
读取一次完整 history recovery
上传一个 Slack File
```

因此以下 scope 缺失时，最初的“验证并保存”仍可能通过，随后正式使用才报错：

```text
channels:history
chat:write
files:write
```

所以不要只看绿色连接状态，确保前面列出的 scopes 一个不少。

## 14. 最终实际通信测试

在 CWapi 已经添加至少一个项目后，让 Web GPT 先做最简单的 discovery：

```text
projects/list
```

正常链路应当是：

```text
Web GPT
→ Slack 控制频道
→ CWapi 收到 MCP_REQUEST
→ CWapi 返回 MCP_RESPONSE
→ Web GPT 读取结果
```

然后再测试：

```text
mcpServerStatus/list
```

最后才进入项目级：

```text
project_id + expected_commit + mcpServer/tool/call
```

这样一层一层测，比一上来扔一个半小时构建任务然后猜到底哪儿坏了文明很多。

## 15. 使用 private channel 时要额外配置什么

如果你明确要把控制频道设成 private channel，需要在 Bot Token Scopes 额外加入：

```text
groups:read
groups:history
```

并在 Event Subscriptions 的 bot events 加入：

```text
message.groups
```

然后 **Reinstall to Workspace**，并确保 CWapi Bot 已经被邀请进这个 private channel。

如果只是个人使用，专用 public channel 通常更省事。

## 16. 常见 Slack 配置错误

### `SLACK_APP_TOKEN_INVALID`

App Token 不是 `xapp-...`，或者复制时带了空格 / 换行。

### `SLACK_BOT_TOKEN_INVALID`

Bot Token 不是 `xoxb-...`。

### `SLACK_READINESS_BOT_IDENTITY_FAILED`

Bot Token 无效、被撤销、Workspace 不匹配或 Slack API 无法访问。

### `SLACK_READINESS_CHANNEL_FAILED`

常见原因：

```text
Channel ID 填错
缺少 channels:read / groups:read
Bot 没加入目标频道
```

### `SLACK_BOT_NOT_CHANNEL_MEMBER`

把 CWapi Bot 加入控制频道。

### `SLACK_READINESS_SOCKET_FAILED`

检查：

```text
Socket Mode 是否启用
App Token 是否为 xapp-...
App Token 是否有 connections:write
```

### `SLACK_API_ERROR_missing_scope`

回到 OAuth & Permissions 增加缺少的 scope，然后 **Reinstall to Workspace**。

### 能收消息但不能返回文本

检查：

```text
chat:write
```

### 文本正常但截图 / 文件失败

检查：

```text
files:write
```

更多故障见 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。

## 17. Token 安全

下面这些值都按密码处理：

```text
xapp-...
xoxb-...
```

不要：

- commit 到 GitHub；
- 发给 Web GPT；
- 放在 MCP `command` / `argv`；
- 贴进普通 Slack 消息；
- 放进截图、日志或 issue。

CWapi 成功验证后会把 token 保存到 Windows Credential Manager。

如果 token 已经泄漏，应去 Slack App 管理页面撤销 / 重新生成，而不是只从聊天记录里删除后假装宇宙没有看见。

## 18. 官方 Slack 文档

Slack 界面名称偶尔会变化，遇到菜单位置变化时可参考：

- Socket Mode: `https://docs.slack.dev/apis/events-api/using-socket-mode/`
- App quickstart: `https://docs.slack.dev/quickstart/`
- `connections:write`: `https://docs.slack.dev/reference/scopes/connections.write/`
- `chat:write`: `https://docs.slack.dev/reference/scopes/chat.write/`
- `files:write`: `https://docs.slack.dev/reference/scopes/files.write/`
- `conversations.info`: `https://docs.slack.dev/reference/methods/conversations.info/`
- `conversations.history`: `https://docs.slack.dev/reference/methods/conversations.history/`
