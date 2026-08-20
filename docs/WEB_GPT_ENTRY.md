# CWapi v1.6.0 Web GPT Entry

这是 **Web GPT 使用 CWapi v1.6.0 的唯一必读入口**。先 discovery，再对 exact commit 做结构化本机调用；不要沿用 v1.5.1 的 Gmail / Runner / custom Tool 工作流。

## 1. 当前链路

```text
Web GPT → GitHub：源码 / exact commit
       → Slack：CWapi MCP frame
       → CWapi：discovery / exact-commit / state / delivery
       → stock Codex app-server → configured MCP server
       → response / Slack File
```

Web GPT 负责决策；CWapi 不规划项目；正常 relay 不启动 Codex model Turn。

## 2. 新会话先 discovery

优先调用 `mcpServerStatus/list`，读取 stock MCP catalog 和 CWapi 附加的 `cwapi.discovery.v1`：`source_commit`、request methods、projects、project context、process tools、start modes、command path forms。

只要项目列表时调用：

```text
method = projects/list
params = {}
```

不要猜 `project_id`。

## 3. Slack frame

正式请求：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

subject ID 与 JSON `request_id` 必须一致。普通频道聊天不是 protocol request。

项目 discovery 示例：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][GPTPROJECTS01]
{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"GPTPROJECTS01","method":"projects/list","params":{}}
+++
```

## 4. 项目调用必须绑定 exact commit

项目 request 同时提供：

```text
project_id
expected_commit   # GitHub repository 的完整 40 位 SHA
```

CWapi 自己执行：project lookup → Git mirror fetch → verify commit → detached worktree → `thread/start(cwd + permissions)` → MCP call。

Web GPT 不提供本机 project/mirror/worktree path、Codex `threadId`、`CODEX_HOME`、profile ID，也不能注入 `_cwapi_workspace`、`_cwapi_expected_commit`、`_cwapi_request_id`。

## 5. 当前 request methods

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

不要发送旧 `workspace.open/test.run/build.run/automation.run/fs.* / process.* / resources.read / cwapi-dev`。

`cwapi/process_start` 是 MCP `server/tool`，不是旧顶层 `process.*` method。

## 6. 本机进程

当前 local server：`server=cwapi`；工具：`process_start`、`process_status`、`process_stop`。

示例，人话是“用准确 Python 在当前 exact commit 启动 `server.py`”：

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

这个对象放进外层 `method=mcpServer/tool/call` 的 `params`。如果返回 `running + process_id`，后续查同一 process，不重复 start。

## 7. 环境由 Web GPT / 用户管理

CWapi 不决定目标项目该使用哪个 Python/JDK/SDK/Node/Go/Rust 版本。先读取项目要求，再检查本机已有环境；已有就用准确 executable，缺失才选择安装方式和位置。

`command` 支持：PATH 名称、absolute executable、working-directory-relative executable，例如：

```text
python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
C:/Program Files/Git/cmd/git.exe
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
```

正式 MCP JSON 中 Windows 路径统一把 `\` 转成 `/`，不要给 `command` 值再包一层引号。

exact-commit worktree 在调用与附件处理后释放；不要假定请求 A 临时创建的 `.venv` 会跨请求保留。长期复用环境用明确持久位置 + absolute executable，或按项目需要重建。

## 8. Playwright

典型 Web E2E：

```text
process_start server
→ browser_navigate localhost
→ fill / click
→ browser_evaluate / DOM read 验证真实结果
→ screenshot（需要时）
→ process_stop
```

不要把“页面能打开”“按钮能点”当成业务通过。需要读取实际结果 / 状态。

不要使用 Playwright unsafe arbitrary-code 能力冒充通用 shell/build runner；本机命令走 `cwapi/process_start`。

## 9. 文件 / 图片 / 日志

```text
MCP 已取得并返回内容
→ CWapi outbound policy
→ Slack message / Slack File
```

当前：短文本 inline；长文本/日志、image、resource text/blob 可转 Slack File；单 artifact 最大 8 MiB，单 response 最多 16 个；超限明确失败；不静默截断；delivery 失败不重放有副作用 tool。

CWapi 不因为 result 里出现 `C:/...`、`file://...` 就自行读取对应本地文件。

## 10. Permissions

Codex-managed：`safe -> cwapi-safe`，`full_access -> cwapi-full-access`。

packaged `cwapi` command/process MCP 启动的自由 executable 以当前 Windows 用户权限运行，**不自动继承 Codex thread filesystem / execpolicy sandbox**。只对用户明确配置的项目使用自由 command；不要把 token/password/private key 放进 command/argv。

## 11. Duplicate

fingerprint = `project_id + expected_commit + method + canonical params`。same request ID + same fingerprint 不重复执行；same ID + different fingerprint -> conflict；当前会话 terminal response / Slack file reference 可复用；ambiguous side-effect call 不自动 replay。

## 12. 等待预算

对同一个外部任务，单次回复累计最多等待 3 分钟。短轮询不会重置。到上限仍无 terminal result：停止本轮等待；报告 request/task/process ID、exact commit、最后状态；本机任务可继续；下一轮只查原任务；不重复提交；停止等待不等于 cancel。

## 13. 常见错误方向

```text
MCP_PROCESS_CONTEXT_REQUIRED → 取得真实 project_id + exact commit
MCP_PROJECT_CONTEXT_INCOMPLETE → 两字段必须成对
PROCESS_COMMAND_NOT_FOUND → 重新发现 executable / 修正路径
PROCESS_START_FAILED → 看 command_path / stderr / 系统启动错误
ERR_CONNECTION_REFUSED → 看 server 是否 running；stop 后出现通常正常
```

详细见 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。完整规则见 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)、[`PROTOCOL.md`](PROTOCOL.md)、[`SECURITY.md`](SECURITY.md)、[`OPERATIONS.md`](OPERATIONS.md)。