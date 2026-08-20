# CWapi v1.6.0 MCP Protocol

本文定义 Slack 上的 CWapi frame、CWapi discovery method 与 stock Codex MCP relay 边界。MCP tool schema 仍由 stock app-server / configured MCP server 定义，CWapi 不复制第二套 Tool schema。

## 1. Slack frame

正式 request：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

Response / Event 使用同样 frame，family 分别为 `MCP_RESPONSE` / `MCP_EVENT`。`+++` 是 frame token；subject 单独一行；JSON body 位于 subject 与 closing frame 之间。普通频道聊天不是 protocol request。

## 2. Message families

```text
MCP_REQUEST
MCP_RESPONSE
MCP_EVENT
```

当前主要工作流是 request → terminal response。EVENT 只用于有真实进度来源的场景，不由 CWapi 伪造。

## 3. Request schema

项目 request：

```json
{
  "schema": "cwapi.mcp.request.v1",
  "protocol_version": "cwapi-mcp/1",
  "request_id": "GPTEXAMPLE01",
  "project_id": "prj-0123456789abcdef01234567",
  "expected_commit": "0123456789abcdef0123456789abcdef01234567",
  "method": "mcpServer/tool/call",
  "params": {}
}
```

纯 discovery/status 可以省略 `project_id` / `expected_commit`。一旦出现其中一个，另一个必须同时出现。

当前 `project_id`：`prj-` + 24 个小写十六进制字符；`expected_commit` 必须为完整 40 位 Git SHA，decode 后规范成小写。subject request ID 与 JSON `request_id` 必须一致。

## 4. Request method 分两类

CWapi discovery：

```text
projects/list
```

stock app-server relay：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

因此 discovery 暴露的整体 request method 集合是四项；其中 `projects/list` 由 CWapi 自己处理，不转发到 stock app-server。

## 5. `projects/list`

参数必须为空对象：

```text
method = projects/list
params = {}
```

成功 result schema：`cwapi.projects.list.v1`，包括 `source_commit`、`projects[].project_id`、`projects[].display_name`、`projects[].repository`、usage。

示例：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][GPTPROJECTS01]
{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"GPTPROJECTS01","method":"projects/list","params":{}}
+++
```

## 6. `mcpServerStatus/list` discovery 扩展

CWapi 在 stock status 结果附加 `cwapi.discovery.v1`，主要字段：

```text
source_commit
request_methods
projects
project_context
process_tools
process_start_modes
command_path_forms
projects_list_request
```

Web GPT 用它发现当前运行实例，而不是沿用旧会话假设。

## 7. Caller 不能注入内部 context

`params.threadId` 由 CWapi 管理。packaged `cwapi` process server 的 `_cwapi_workspace`、`_cwapi_expected_commit`、`_cwapi_request_id` 也由 CWapi 注入，caller 不能提供。

Web GPT 只提供外层 `project_id + expected_commit`；本机 project/mirror/worktree path、threadId、permission profile ID 都不属于远端输入。

## 8. 旧 custom method 已删除

当前不定义：

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

`cwapi/process_start` 表示 configured MCP server/tool，不是旧顶层 `process.*` contract。

## 9. Packaged `cwapi` process server

通过 `method=mcpServer/tool/call`：

```json
{
  "server": "cwapi",
  "tool": "process_start",
  "arguments": {
    "command": "C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe",
    "argv": ["server.py"]
  }
}
```

项目调用外层仍必须带 `project_id + expected_commit`。

当前 process tool：`process_start`、`process_status`、`process_stop`。discovery 暴露 start modes `command_argv` 与兼容 `runtime_entrypoint`；正式环境管理优先 `command + argv`。

## 10. Command path forms

支持：PATH executable、absolute executable、working-directory-relative executable。

```text
python.exe
C:/Program Files/Git/cmd/git.exe
C:/Program Files/Java/jdk-25/bin/java.exe
.venv/Scripts/python.exe
tools/build.cmd
```

直接路径找不到时返回 `PROCESS_COMMAND_NOT_FOUND`，不会退化为 PATH 猜测。native executable argv 直接传入；`.cmd/.bat` 使用 Windows command-script 语义。

正式 MCP JSON 把 Windows `\` 转成 `/`，不要给 `command` 值再套一层引号。安装位置、语言版本和命令语义由 Web GPT/用户选择，CWapi 不管理。

## 11. Identity / idempotency

- `request_id` 是业务身份。
- fingerprint = `project_id + expected_commit + method + canonical params`。
- same ID + same fingerprint 不重复执行。
- same ID + different fingerprint 返回 conflict。
- 当前运行会话内 terminal response / Slack file reference 可复用。
- Slack timestamp 不是业务幂等键。
- ambiguous side-effect MCP call 不自动 replay。

## 12. Exact-commit context

项目 request 内部：

```text
project_id → configured repository → Git mirror fetch
→ verify expected_commit → detached worktree
→ thread/start(cwd=worktree, permissions=profile)
```

exact-commit worktree 在调用与附件处理后释放，不能当成跨 request 永久环境目录。

## 13. Terminal response

当前代码 terminal status：

```text
completed
blocked
failed
timed_out
unavailable
```

当前 v1.6.0 **没有独立 `cancelled` terminal status**。没有真实 request-scoped cancellation contract 时，不应由 UI/文档伪造 cancellation。

错误 envelope 提供稳定字段，例如 `code / category / message / retryable`。

## 14. Artifact / Slack File

当结果需要文件，`MCP_RESPONSE.resources` 记录：

```json
{
  "uri": "https://...slack.../files/...",
  "media_type": "image/png",
  "sha256": "...",
  "size_bytes": 1234
}
```

没有 permalink 时可使用 `slack-file://<file_id>` 稳定引用。

外发只处理 MCP **已经返回**的 text/blob/image/resource content；CWapi 不根据 result 中的 path/URI 自行读取本地文件。

当前：短文本 inline；长文本/日志 `.txt`；image/audio、resource content 使用 Slack File；必要时过大的结构化 result 可 `.json`；单 artifact 最大 8 MiB，单 response 最多 16 个；超限或非法 base64 明确失败；不静默截断；delivery 失败不重放 tool。

## 15. Delivery 顺序

无附件：terminal execution → 写本地 state → 投递 Slack MCP_RESPONSE。

有附件：MCP execution 结束 → outbound policy → Slack external upload → file reference → compact response → 写 state → 投递 MCP_RESPONSE。重投 terminal request 时只重投已有 response/reference，不重新执行 tool。

## 16. Limits

当前核心限制包括：protocol text 最大 64 KiB；body/params 有更严格上限；error message 有界；request ID/method/project/commit 严格校验；Slack artifact 8 MiB / 16 files；secret 不允许进入普通 protocol payload/artifact。具体常量以当前源码为准。

相关：[`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)、[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)、[`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)、[`SECURITY.md`](SECURITY.md)、[`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)。