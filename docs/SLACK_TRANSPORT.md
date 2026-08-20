# CWapi v1.6.0 Slack Transport

这份文档只讲 **Slack transport 的技术实现**。第一次创建 Slack App、配置 scopes、Socket Mode、token 和 Channel ID，请直接看 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

Slack 负责远程传输，不负责本地执行权限判定。

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

## Credential boundary

CWapi runtime 使用：

```text
App Token   xapp-...
Bot Token   xoxb-...
Channel ID  C...
```

Token 验证后存 Windows Credential Manager，不进入普通 config、SQLite、Git、argv、MCP body、artifact 或日志。

## 当前 Slack API 使用

CWapi 当前主要调用：

```text
auth.test
conversations.info
conversations.history
apps.connections.open
chat.postMessage
chat.delete
files.getUploadURLExternal
files.completeUploadExternal
```

对应用户配置所需 scopes 见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

## Protocol frame

正式 CWapi protocol message：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

Response / Event 使用相同 frame，只替换 subject family。

普通频道聊天不是协议 request。parser 只处理完整 frame；明显意图调用 CWapi 但格式损坏的文本可以被识别为 protocol candidate，并返回协议 / discovery 方向。

## Inbound

transport 层只做：

- configured team / channel；
- self-event filtering；
- frame / envelope / message size；
- MCP schema / request_id；
- project_id / expected_commit 组合；
- duplicate / idempotency。

**Slack parser 不解析具体 command/tool 内容来做 filesystem 权限。** 真正执行边界分别属于 Codex profile、MCP server 或当前 Windows 用户权限。

## Request method 分层

CWapi 自己处理：

```text
projects/list
```

stock Codex app-server relay：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

`projects/list` 不转发给 stock app-server。

## Socket Mode

CWapi 使用 `apps.connections.open` 获取 WebSocket URL，并要求 Socket hello 成功。

收到 Socket envelope 后：

1. 先基于 `envelope_id` ACK；
2. 再解析业务 payload；
3. 只处理 `events_api` 中的 `message` event；
4. 只接受配置的 Channel ID；
5. 过滤 CWapi 自己的 bot/user event；
6. 普通非协议聊天忽略。

ACK 不等待本机 build/test/tool 完成，避免把 Slack transport 的时限和本机任务生命周期绑在一起。

## Project discovery

新 Web GPT 会话通过：

```text
projects/list
```

或：

```text
mcpServerStatus/list
```

取得当前 `project_id` / discovery。项目调用再使用：

```text
project_id + expected_commit
```

## Duplicate / recovery

- 每次 CWapi 启动建立新运行会话，不回放启动前的旧 channel history；
- 同一进程内 Socket reconnect 从 durable cursor 后恢复；
- duplicate same request 不启动第二次副作用调用；
- fingerprint 包含 project / commit / method / params；
- terminal duplicate 可复用已有 compact response / Slack file 引用；
- ambiguous side-effect call 不自动 replay；
- Slack timestamp 不是业务幂等键。

## Text / file delivery

短文本直接进入 MCP response；长文本 / 日志、图片、resource text/blob 可走 Slack external file upload：

```text
files.getUploadURLExternal
→ upload URL
→ files.completeUploadExternal
```

当前产品级限制：

```text
单 artifact ≤ 8 MiB
单 response ≤ 16 artifacts
```

超限明确失败，不静默截断。

CWapi 只上传 **MCP 已经返回的 bytes/text/image/resource content**，不会因为 result 里出现本地 path / URI 就自行读取文件。因此 filesystem read permission 与 Slack upload permission 是两个独立层次。

## Real gate

真实验收链路：

```text
external Web GPT / Slack identity
→ Slack MCP_REQUEST
→ CWapi exact-commit context
→ stock Codex app-server
→ configured MCP server
→ MCP result
→ Slack MCP_RESPONSE / Slack File
```

不得用 CWapi bot 自发自收冒充真实入站链路。

## 相关文档

- Slack 从零配置：[`SLACK_SETUP.md`](SLACK_SETUP.md)
- Web GPT 快速入口：[`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)
- 完整工作流：[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
- 协议：[`PROTOCOL.md`](PROTOCOL.md)
- 故障：[`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)