# CWapi v1.6.0 MCP Protocol

本文只定义 Slack 上的 CWapi relay envelope。MCP 工具本身由 stock Codex app-server / MCP server 定义，CWapi 不复制第二套 Tool schema。

## Message families

```text
MCP_REQUEST
MCP_RESPONSE
MCP_EVENT
```

当前主要工作流是 request -> terminal response。EVENT 只保留给有真实进度来源的场景，不由 CWapi 伪造。

## Request

项目相关调用：

```json
{
  "schema": "cwapi.mcp.request.v1",
  "protocol_version": "cwapi-mcp/1",
  "request_id": "...",
  "project_id": "prj-...",
  "expected_commit": "0123456789abcdef0123456789abcdef01234567",
  "method": "mcpServer/tool/call",
  "params": {}
}
```

纯状态调用可以省略 `project_id` 和 `expected_commit`。一旦提供其中一个，另一个必须同时提供。

允许的方法只有：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

`params.threadId` 由 CWapi 管理，远端提供则拒绝。

CWapi 不再定义：

```text
workspace.open/status/close
git.rev_parse/status
test.run
build.run
automation.run
fs.*
process.*
resources.read
cwapi-dev
```

这些旧 custom Tool 名称不属于当前 v1.6.0 协议。

当前随包 local MCP server 名为 `cwapi`，通过 stock `mcpServer/tool/call` 调用，不属于上述旧 custom envelope：

```json
{
  "server": "cwapi",
  "tool": "process_start",
  "arguments": {
    "command": "powershell.exe",
    "argv": ["-NoProfile", "-NonInteractive", "-Command", "<command>"],
    "cwd": "."
  }
}
```

`command` 支持三种定位：PATH executable 名称、绝对路径、相对 `cwd` 的路径；也保留 `runtime + entrypoint` 兼容模式。直接路径会解析为真实 regular file，找不到时返回 `PROCESS_COMMAND_NOT_FOUND`，不会退化为 PATH 猜测。Windows `.cmd/.bat` 通过 `cmd.exe` 启动，因此 shim 参数遵循 Windows command-script 语义；native executable 的 `argv` 直接传入，不经过 shell。

Web GPT 在 MCP JSON 中必须把 Windows 路径的 `\` 统一转换成 `/`，例如 `C:/Python312/python.exe`、`.venv/Scripts/python.exe`、`node_modules/.bin/tool.cmd` 或 `//server/share/tool.exe`。`C:\\Python312\\python.exe` 只保留为实现兼容输入，不属于正式工作流格式。不要给 `command` 值再加一层引号。调用必须带外层 `project_id + expected_commit`；CWapi 注入真实 workspace context，caller 不能注入 `_cwapi_*` 字段。安装位置、语言版本和命令语义由 Web GPT 选择，CWapi 不管理。

## Identity / idempotency

- `request_id` 是业务身份；
- fingerprint 由 `project_id + expected_commit + method + canonical params` 生成；
- 同 request_id + 同 fingerprint 不重复执行；
- 同 request_id + 不同 fingerprint 返回 conflict；
- 当前运行会话内，已 terminal request 可重投已有 compact response；
- Slack timestamp 不是执行幂等键；
- ambiguous side-effect MCP call 不自动 replay。

## Exact-commit context

如果 request 带项目上下文，CWapi 内部执行：

```text
project_id
 -> configured repository
 -> Git mirror fetch
 -> verify expected_commit
 -> detached isolated worktree
 -> Codex thread/start(cwd=worktree, permissions=profile)
```

caller 不能提供本机 path、worktree path、`threadId` 或 permission profile ID。Git 同步也不是 MCP server 的职责。

## Response

统一 terminal status：

```text
completed
blocked
failed
cancelled
timed_out
unavailable
```

成功结果以 stock app-server 返回值为来源；错误转成简短稳定的 CWapi error envelope。

当结果需要 Slack File 时，正文被压缩为小型结果/占位信息，并在 `resources` 中加入外发引用：

```json
{
  "uri": "https://...slack.../files/...",
  "media_type": "image/png",
  "sha256": "...",
  "size_bytes": 1234
}
```

若 Slack 没有返回 permalink，可使用 `slack-file://<file_id>` 作为稳定引用；文件本身仍位于同一 Slack thread。

## 文件 / 图片 / 日志外发

外发发生在 MCP 已经返回内容之后：

```text
MCP read/tool permission
 -> returned text/blob/image
 -> CWapi outbound policy
 -> Slack File
```

CWapi **不会**根据 result 中的 path/URI 自行读取本地文件。

当前规则：

- 短文本保留 inline；
- 长文本/日志使用 `.txt` Slack File；
- MCP image/audio data 使用 Slack File；
- `mcpServer/resource/read` 返回的 resource text/blob 使用 Slack File；
- compact 后仍超过 inline budget 的 JSON result 使用 `.json` Slack File；
- 单个 artifact 最大 8 MiB；
- 单次 response 最多 16 个 artifact；
- 超限或非法 base64 明确失败，不静默截断，不重放 tool。

## Delivery

无附件时：terminal response 先写本地 state，再投递 Slack protocol message。

有附件时：

1. MCP execution 已经结束；
2. CWapi 按 outbound policy 上传由 MCP 返回的 artifact；
3. 得到 Slack file 引用；
4. 生成 compact terminal response；
5. 写本地 state；
6. 投递 MCP_RESPONSE。

重投已 terminal request 时只重投已有 response / Slack file 引用，不重新执行 MCP tool。

## Limits

- MCP message/body 有大小上限；
- method/request/project/commit 严格校验；
- error message 有大小上限；
- Slack artifact 受 8 MiB / 16 files 限制；
- secret 不允许进入普通 protocol payload 或可上传 artifact。
