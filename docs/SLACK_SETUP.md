# CWapi v1.6.0 Slack 配置教程

这份文档是 **CWapi 的 Slack 配置唯一权威教程**。第一次创建 Slack App、配置 scopes、Socket Mode、token 和 Channel ID，都按这里做。

技术实现见 [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)；Web GPT 执行规则见 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)。

## 1. 先区分两个 Slack 连接

```text
A. CWapi 自己的 Slack App / Bot
   本机 CWapi 用它收请求、发结果、上传文件

B. ChatGPT 中连接的 Slack
   Web GPT 用它向控制频道发请求、读取 CWapi 返回
```

CWapi 的 `xapp-...` / `xoxb-...` 只填进 CWapi，不要发给 Web GPT。

## 2. 推荐配置

推荐建立专用 **public channel**，例如 `#cwapi-control`。这样权限最少、配置最简单。

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

当前代码用途：

```text
channels:read      -> conversations.info
channels:history   -> conversations.history + message event
chat:write         -> chat.postMessage / chat.delete
files:write        -> external file upload
connections:write  -> apps.connections.open / Socket Mode
```

CWapi 使用 Socket Mode，不需要公网 Events API Request URL。

## 3. 创建 Slack App

1. 打开 `https://api.slack.com/apps`。
2. 点击 **Create New App** → **From scratch**。
3. App Name 可填 `CWapi`。
4. 选择要给 CWapi 使用的 Workspace 并创建。

如果 Workspace 禁止普通成员创建 / 安装 App，需要 Owner / Admin 授权，这是 Slack 自己的 Workspace 策略。

## 4. 添加 Bot scopes

进入：

```text
OAuth & Permissions
→ Bot Token Scopes
```

依次添加：

```text
channels:read
channels:history
chat:write
files:write
```

专用频道里不需要 `chat:write.public`，把 Bot 正常加入控制频道即可。

以后修改 scopes 后，必须 **Reinstall to Workspace**，否则旧授权可能没有新权限。

## 5. 开启 Event Subscriptions

进入：

```text
Event Subscriptions
→ Enable Events
→ Subscribe to bot events
→ Add Bot User Event
```

添加：

```text
message.channels
```

Socket Mode 下不需要填写公网 Request URL。

## 6. 开启 Socket Mode

进入：

```text
Socket Mode
→ Enable Socket Mode
```

它让本机 CWapi 主动通过 WebSocket 连接 Slack，不要求你的电脑暴露公网 HTTP 服务。

## 7. 创建 App Token（xapp-...）

进入：

```text
Basic Information
→ App-Level Tokens
→ Generate Token and Scopes
```

1. Token Name 可写 `CWapi Socket`。
2. Add Scope：`connections:write`。
3. Generate。
4. 复制生成的 `xapp-...`。

这个值填入 CWapi 的 **App Token**。

## 8. 安装 App 并取得 Bot Token（xoxb-...）

进入：

```text
OAuth & Permissions
→ Install to Workspace
→ Allow
```

安装后复制：

```text
Bot User OAuth Token = xoxb-...
```

这个值填入 CWapi 的 **Bot Token**。

如果后来新增 `files:write`、`channels:history` 等 scope，回到这里 **Reinstall to Workspace**。否则权限页面看起来都对，程序却继续 `missing_scope`，这种古老仪式 Slack 也没有免俗。

## 9. 创建控制频道并邀请 Bot

在 Slack 创建专用频道，例如：

```text
#cwapi-control
```

推荐 public channel。

然后把 **CWapi App / Bot 加入频道**。可以从频道详情的 Integrations / Apps 添加，也可以使用 Slack 的邀请 App 入口。

CWapi 保存配置时会调用 `conversations.info` 并检查 `is_member`，所以 Bot 必须真的是频道成员。

## 10. 找到 Channel ID

CWapi 要的是：

```text
C0123456789
```

不是 `#cwapi-control`。

在浏览器打开目标频道，URL 通常类似：

```text
https://app.slack.com/client/T01234567/C0123456789
```

其中 `C0123456789` 就是 Channel ID。

## 11. 把三个值填进 CWapi

打开：

```text
CWapi
→ 设置
→ Slack
→ 更换 Slack 配置
```

填写：

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

点击 **验证并保存**。

当前 CWapi 保存前会实际验证：

```text
Bot 身份      -> auth.test
频道 / 成员   -> conversations.info
Socket Mode   -> apps.connections.open + WebSocket hello
```

成功后 token 写入当前 Windows 用户的 **Credential Manager**，不进入普通配置文件。

## 12. 怎么判断已经连好

设置页应显示正确的 Workspace、控制频道名称和 Channel ID。

控制台重点看：

```text
Slack Transport = connected / healthy
```

诊断页不应持续出现：

```text
SLACK_READINESS_...
SLACK_API_ERROR_...
SLACK_SOCKET_...
```

如果保存成功后 Socket 很快变 degraded，优先检查 `channels:history` 和 Event Subscription，因为 reconnect / recovery 会读取频道历史。

## 13. “验证并保存”不会测完所有 scopes

readiness probe 会验证身份、频道和 Socket Mode，但不会实际执行一次完整：

```text
MCP response 回帖
history recovery
Slack File 上传
```

所以这些 scope 缺失时，初次验证仍可能通过，正式使用才报错：

```text
channels:history
chat:write
files:write
```

因此不要只看绿色状态，确保 scope 清单一个不少。

## 14. 最终实际通信测试

在 CWapi 已经添加项目后，让 Web GPT 先做：

```text
projects/list
```

正常链路：

```text
Web GPT
→ Slack 控制频道
→ CWapi MCP_REQUEST
→ CWapi MCP_RESPONSE
→ Web GPT 读取结果
```

然后再测试 `mcpServerStatus/list`，最后才进入 `project_id + expected_commit + mcpServer/tool/call` 的项目级调用。

## 15. private channel 额外配置

如果控制频道明确使用 private channel，Bot Token Scopes 额外加入：

```text
groups:read
groups:history
```

Event Subscriptions 额外加入：

```text
message.groups
```

然后 **Reinstall to Workspace**，并确保 CWapi Bot 已被邀请进 private channel。

个人使用通常推荐专用 public channel，少两层权限就少两层人类娱乐项目。

## 16. 常见错误

### `SLACK_APP_TOKEN_INVALID`
App Token 不是 `xapp-...`，或复制时带空格 / 换行。

### `SLACK_BOT_TOKEN_INVALID`
Bot Token 不是 `xoxb-...`。

### `SLACK_READINESS_BOT_IDENTITY_FAILED`
Bot Token 无效、被撤销、Workspace 不匹配或 Slack API 无法访问。

### `SLACK_READINESS_CHANNEL_FAILED`
常见原因：Channel ID 错、缺少 `channels:read/groups:read`、Bot 没加入频道。

### `SLACK_BOT_NOT_CHANNEL_MEMBER`
把 CWapi Bot 加入控制频道。

### `SLACK_READINESS_SOCKET_FAILED`
检查 Socket Mode、`xapp-...` 和 `connections:write`。

### `SLACK_API_ERROR_missing_scope`
添加缺少的 scope，然后 **Reinstall to Workspace**。

### 能收消息但不能返回文本
检查 `chat:write`。

### 文本正常但 screenshot / 文件失败
检查 `files:write`。

更多故障见 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。

## 17. Token 安全

`xapp-...` 和 `xoxb-...` 都按密码处理。不要 commit 到 GitHub、发给 Web GPT、放进 MCP `command/argv`、普通 Slack 消息、截图、日志或 issue。

如果 token 已泄漏，到 Slack App 管理页撤销 / 重新生成，而不是只删聊天记录然后期待宇宙配合失忆。

## 18. 官方 Slack 文档

Slack UI 名称偶尔会变化，菜单位置变化时参考：

- Socket Mode: `https://docs.slack.dev/apis/events-api/using-socket-mode/`
- Quickstart: `https://docs.slack.dev/quickstart/`
- `connections:write`: `https://docs.slack.dev/reference/scopes/connections.write/`
- `chat:write`: `https://docs.slack.dev/reference/scopes/chat.write/`
- `files:write`: `https://docs.slack.dev/reference/scopes/files.write/`
- `conversations.info`: `https://docs.slack.dev/reference/methods/conversations.info/`
- `conversations.history`: `https://docs.slack.dev/reference/methods/conversations.history/`
