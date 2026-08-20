# CWapi v1.6.0 MCP Protocol

本文定义 Slack 上的 CWapi relay envelope、CWapi discovery method 与 stock Codex MCP relay 边界。

MCP 工具本身仍由 stock Codex app-server / configured MCP server 定义；CWapi 不复制第二套 Tool schema。

---

## 1. Slack frame

正式协议消息使用：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

Response：

```text
+++
[CWapi/MCP/1][MCP_RESPONSE][REQUEST_ID]
{JSON body}
+++
```

EVENT：

```text
+++
[CWapi/MCP/1][MCP_EVENT][REQUEST_ID]
{JSON body}
+++
```

`+++` 是 frame token；subject 单独占一行；JSON body 位于 subject 与 closing frame 之间。

当前 parser 会把 Slack 可能附加在 closing frame 同一行之后的 attribution 视为 frame 外内容，不纳入 JSON body。

普通频道聊天不是 CWapi protocol message。

---

## 2. Message families

```text
MCP_REQUEST
MCP_RESPONSE
MCP_EVENT
```

当前主要工作流是：

```text
request -> terminal response
```

EVENT 只保留给有真实进度来源的场景，不由 CWapi 伪造。

---

## 3. Request schema

项目相关调用：

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

纯 discovery / status 调用可以省略：

```text
project_id
expected_commit
```

一旦提供其中一个，另一个必须同时提供。

当前 `project_id` 格式：

```text
prj- + 24 个小写十六进制字符
```

`expected_commit` 必须是完整 40 位 Git SHA；decode 后规范成小写。

---

## 4. Request method 分两类

### CWapi discovery method

```text
projects/list
```

这是 CWapi 自己处理的 discovery request，不转发到 stock app-server。

参数必须为空对象：

```json
{}
```

示例：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][GPTPROJECTS01]
{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"GPTPROJECTS01","method":"projects/list","params":{}}
+++
```

成功结果 schema：

```text
cwapi.projects.list.v1
```

内容包括：

```text
source_commit
projects[].project_id
projects[].display_name
projects[].repository
usage
```

### stock Codex app-server MCP relay method

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

这三个 method 才是正常 relay 到 stock app-server 的 MCP method。

因此“当前 request methods”整体是：

```text
projects/list
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

其中 `projects/list` 属于 CWapi discovery，另外三个属于 stock MCP relay。

---

## 5. `mcpServerStatus/list` 的 CWapi discovery 扩展

CWapi 会在 stock status 结果上附加：

```text
cwapi.discovery.v1
```

主要字段：

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

这允许 Web GPT 在新会话先发现：

- 当前 CWapi source commit；
- 当前配置项目；
- 真实 `project_id`；
- 当前 process tool；
- command 支持的路径形式。

不要从旧会话猜这些值。

---

## 6. Caller 不能注入 thread / workspace context

`params.threadId` 由 CWapi 管理，远端提供则拒绝。

对于 packaged `cwapi` process server，以下字段也由 CWapi 注入：

```text
_cwapi_workspace
_cwapi_expected_commit
_cwapi_request_id
```

caller 不能自行提供。

Web GPT 只提供外层：

```text
project_id
expected_commit
```

CWapi 自己把真实 prepared workspace context 注入本地 MCP tool。

---

## 7. 已删除的旧 custom method

CWapi v1.6.0 不再定义：

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

这些名称属于旧架构，不是当前顶层协议。

特别注意：

```text
cwapi/process_start
cwapi/process_status
cwapi/process_stop
```

是 configured MCP server + tool 的表示方式，不是旧顶层 `process.*` contract。

---

## 8. Packaged `cwapi` process server

当前随包 local MCP server：

```text
server = cwapi
```

通过：

```text
method = mcpServer/tool/call
```

调用。

示例：

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

请求外层仍必须带：

```text
project_id + expected_commit
```

### 当前 process tool

```text
process_start
process_status
process_stop
```

### `process_start` mode

当前 discovery 暴露：

```text
command_argv
runtime_entrypoint
```

正式环境管理工作流优先使用 `command + argv`，由 Web GPT 选择准确 executable。

### command path forms

支持：

```text
PATH executable name
absolute executable path
working-directory-relative executable path
```

例如：

```text
python.exe
C:/Program Files/Git/cmd/git.exe
C:/Program Files/Java/jdk-25/bin/java.exe
.venv/Scripts/python.exe
tools/build.cmd
node_modules/.bin/tool.cmd
```

直接路径会解析真实 regular file；找不到时返回 `PROCESS_COMMAND_NOT_FOUND`，不会退化成 PATH 猜测。

native executable 的 argv 直接传入，不经过 shell。

Windows `.cmd/.bat` 使用 `cmd.exe` 启动，因此遵循 Windows command-script 参数语义。

---

## 9. Windows path canonical form

Web GPT 在 MCP JSON 中应把 `\` 转成 `/`：

```text
C:/Python312/python.exe
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
//server/share/tool.exe
```

`C:\\Python312\\python.exe` 只保留为实现兼容输入，不是正式工作流格式。

不要给 `command` 值再额外包一层引号。

安装位置、语言版本和命令语义由 Web GPT / 用户决定，CWapi 不管理。

---

## 10. Identity / idempotency

- `request_id` 是业务身份；
- subject request ID 与 JSON `request_id` 必须一致；
- fingerprint 由 `project_id + expected_commit + method + canonical params` 生成；
- same request ID + same fingerprint 不重复执行；
- same request ID + different fingerprint 返回 conflict；
- 当前运行会话内，已 terminal request 可重投已有 compact response；
- 已上传 artifact 使用已有 Slack file 引用，不重新执行 MCP tool；
- Slack timestamp 不是业务幂等键；
- ambiguous side-effect MCP call 不自动 replay。

---

## 11. Exact-commit context

如果 request 带项目上下文，CWapi 内部：

```text
project_id
 -> configured repository
 -> Git mirror fetch
 -> verify expected_commit
 -> detached isolated worktree
 -> Codex thread/start(cwd=worktree, permissions=profile)
```

caller 不能提供：

```text
本机 project path
mirror path
worktree path
threadId
permission profile ID
```

Git 同步不是 MCP server 的职责。

exact-commit worktree 在调用与附件处理完成后释放，因此不能当作跨 request 永久环境目录。

---

## 12. Response

当前 terminal status 与代码一致：

```text
completed
blocked
failed
timed_out
unavailable
```

当前 v1.6.0 **没有独立 `cancelled` terminal status**。

如果未来加入真实 request-scoped cancellation contract，再由协议版本或实现更新明确增加；当前不能用 UI / 文案伪造 cancellation。

成功结果以 stock app-server / CWapi discovery 返回值为来源。

错误统一转成简短稳定的 CWapi error envelope，例如：

```json
{
  "code": "MCP_PROCESS_CONTEXT_REQUIRED",
  "category": "workspace",
  "message": "...",
  "retryable": false
}
```

---

## 13. 文件 / 图片 / 日志外发

当结果需要 Slack File 时，正文压缩为小型结果 / 占位信息，并在 `resources` 中加入：

```json
{
  "uri": "https://...slack.../files/...",
  "media_type": "image/png",
  "sha256": "...",
  "size_bytes": 1234
}
```

如果 Slack 没有返回 permalink，可使用：

```text
slack-file://<file_id>
```

作为稳定引用；实际文件仍在同一 Slack thread。

外发顺序：

```text
MCP 已经返回 text/blob/image
 -> CWapi outbound policy
 -> Slack File
```

CWapi **不会**根据 result 中的 path / URI 自行读取本地文件。

当前规则：

- 短文本 inline；
- 长文本 / 日志使用 `.txt` Slack File；
- MCP image / audio data 使用 Slack File；
- `mcpServer/resource/read` 的 resource text / blob 使用 Slack File；
- compact 后仍超过 inline budget 的 JSON result 可使用 `.json` Slack File；
- 单个 artifact 最大 8 MiB；
- 单次 response 最多 16 个 artifact；
- 超限或非法 base64 明确失败；
- 不静默截断；
- 不因为 delivery 失败重放 tool。

---

## 14. Delivery

无附件：

```text
terminal execution
 -> 写本地 state
 -> 投递 Slack MCP_RESPONSE
```

有附件：

```text
1. MCP execution 已结束
2. outbound policy 处理 MCP 已返回 artifact
3. Slack external upload
4. 获得 file reference
5. 生成 compact terminal response
6. 写本地 state
7. 投递 MCP_RESPONSE
```

重投已 terminal request 时只重投已有 response / Slack file reference，不重新执行 MCP tool。

---

## 15. Protocol limits

当前核心限制包括：

```text
MCP protocol text 最大 64 KiB
MCP JSON body / params 有更严格上限
error message 有界
request_id / method / project / commit 严格校验
Slack artifact 8 MiB / 16 files
secret 不允许进入普通 protocol payload / artifact
```

具体常量以当前源码为准。

---

## 16. 相关文档

- [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)：Web GPT 快速入口
- [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)：完整执行工作流
- [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)：常见错误码解释
- [`SECURITY.md`](SECURITY.md)：权限与 command boundary
- [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)：Slack transport / recovery / file upload