# CWapi v1.6.0 Web GPT Workflow

这是 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md) 的完整参考版。正常新会话先读 Entry；需要环境、进程、附件、权限、等待和验收细节时再看这里。

## 1. 当前链路

```text
Web GPT → GitHub：源码 / exact commit
       → Slack：CWapi MCP frame
       → CWapi：discovery / exact-commit / state / delivery
       → stock Codex app-server → configured MCP server
       → result / Slack File
```

Web GPT 负责决策；CWapi 不规划项目；正常 MCP relay 不启动 Codex model Turn。

## 2. 新会话先 discovery

先调用 `mcpServerStatus/list`。当前 CWapi 会附加 `cwapi.discovery.v1`，包含 `source_commit`、request methods、projects、project context、process tools、start modes 和 command path forms。

只需要项目列表时调用：

```text
method = projects/list
params = {}
```

不要猜 `project_id`，也不要默认旧会话 catalog 仍然准确。

## 3. GitHub 是源码与 commit 事实来源

Web GPT 负责通过 GitHub阅读、修改、提交代码并取得完整 40 位 SHA。每次代码变化后，如果要宣称新版本通过本机测试，必须重新使用新的 `expected_commit`；旧 commit 的测试证据不能自动继承。

项目调用使用：

```text
project_id + expected_commit
```

CWapi 再内部执行：project lookup → Git mirror fetch → verify commit → detached worktree → MCP call → delivery → release worktree。

## 4. Slack frame

正式 request：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQUEST_ID","method":"...","params":{}}
+++
```

项目 request 再加入 `project_id` 与 `expected_commit`。subject ID 与 JSON `request_id` 必须一致。

## 5. Request method 分层

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

`cwapi/process_start` 是 `mcpServer/tool/call` 下的 `server=cwapi, tool=process_start`，不是旧版顶层 `process.*` method。

不要再使用：`workspace.open/status/close`、`git.rev_parse/status`、`test.run`、`build.run`、`automation.run`、`fs.*`、`process.*`、`resources.read`、`cwapi-dev`。

## 6. Caller 不管理内部 context

Web GPT 不提供本机 project/mirror/worktree path、Codex `threadId`、`CODEX_HOME`、profile ID，也不能注入 `_cwapi_workspace`、`_cwapi_expected_commit`、`_cwapi_request_id`。这些由 CWapi 根据已配置项目和 exact commit 管理。

## 7. 目标项目环境职责

规则是：

```text
Web GPT / 用户：发现、安装、选择、管理语言与 SDK 环境
CWapi：在 exact commit 上结构化执行
```

Web GPT 应结合 README、lockfile、`pyproject.toml`、`package.json`、`go.mod`、`Cargo.toml`、Gradle/Maven、CI 等确认项目要求，再检查本机已有环境。

环境发现优先于安装，例如 `where.exe`、`Get-Command`、`py.exe -0p`、`<tool> --version`。不要只因 `python` 不在 PATH 就安装第二份 Python。

## 8. 环境缺失时

先确认项目所需版本，再选择正常且可验证的安装方式和持久位置；安装后验证 executable 与版本，再通过 `command + argv` 使用。CWapi 不固定 Python/JDK/SDK 安装位置，也不替 Web GPT判断“哪个版本最适合项目”。

不要把 token/password 放进安装命令参数；需要认证时优先使用本机 credential store / CLI 登录态。

## 9. `process_start.command`

支持：

```text
PATH executable name
absolute executable path
working-directory-relative executable path
```

示例：

```text
python.exe
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
.venv/Scripts/python.exe
tools/build.cmd
node_modules/.bin/tool.cmd
```

native executable 的 argv 直接传入；`.cmd/.bat` 按 Windows command-script 语义执行。

## 10. Windows 路径

正式 MCP JSON 统一把 `\` 转成 `/`：

```text
C:/Projects/example/tool.exe
.venv/Scripts/python.exe
//server/share/tool.exe
```

不要给 `command` 值再加一层外部引号。带空格的绝对路径直接作为结构化 command 使用，不需要绕 PowerShell quoting。

## 11. exact-commit workspace 与持久环境

工作区在调用和附件处理结束后释放，所以不要假定请求 A 在 workspace 创建的 `.venv` 会在请求 B 保留。跨请求复用的环境更适合放在明确持久位置并用 absolute executable，或者按项目需要重新创建。

## 12. Process lifecycle

```text
process_start(command, argv, cwd?)
→ completed 或 running + process_id
→ process_status(process_id)
→ process_stop(process_id)  # 需要时
```

一个 process 已经 running 后，后续查原 `process_id`，不要重复提交同一个 start。主动 stop 后 exit code 不一定为 0；优先看 `state=stopped` 和真实服务状态。

## 13. Shell 只在需要 shell 语义时用

PowerShell/cmd 是普通 executable。如果目标只是运行明确 executable，优先直接 command + argv；不要无意义地包装成 `powershell -> command string -> quoting -> executable`，这只会增加转义、注入面和审计难度。

## 14. Web E2E 推荐流程

```text
process_start server
→ 确认 running / ready output
→ Playwright browser_navigate localhost
→ fill / click
→ browser_evaluate 或 DOM read 验证真实业务结果
→ screenshot（需要时）
→ process_stop
→ 可选：再次访问确认端口关闭
```

不要把“页面能打开”或“按钮点成功”当成业务通过；最终需要读取真实 result/status/output。

## 15. Playwright 边界

`browser_evaluate` 适合读取页面状态，例如：

```js
() => ({
  result: document.querySelector('#result')?.textContent,
  title: document.title
})
```

不要使用 Playwright unsafe arbitrary-code 能力去冒充通用 shell/build runner；本机命令已经有明确的 `cwapi/process_start`。

## 16. 文件读取与外发

```text
MCP 是否取得内容
→ MCP 已返回 text/blob/image
→ CWapi outbound policy
→ Slack message / Slack File
```

当前：短文本 inline；长文本/日志、image、resource text/blob 可转 Slack File；单 artifact 最大 8 MiB，单 response 最多 16 个；超限明确失败；不静默截断；delivery 失败不重放有副作用 tool。

CWapi 不会因为 result 中出现 `C:/...`、`file://...` 或其它 URI 就自行读取对应本地文件。

## 17. Permissions

Codex-managed：`safe -> cwapi-safe`，`full_access -> cwapi-full-access`。project / permission fingerprint 改变后，后续调用重建必要 context。

packaged `cwapi` command/process MCP 是单独 trusted boundary：自由 executable 以当前 Windows 用户权限运行，不自动继承 Codex thread filesystem / execpolicy sandbox；但 request 仍绑定 configured project + exact commit，初始 cwd 仍是 prepared workspace 或其真实子目录，secret 不进入 command 子进程环境，stop 只作用于自己记录的 process tree。

## 18. Duplicate / idempotency

fingerprint 包含 `project_id + expected_commit + method + canonical params`。same request ID + same fingerprint 不重复执行；same ID + different fingerprint -> conflict；当前会话已有 terminal response / Slack file reference 可复用；ambiguous side-effect call 不自动 replay。

## 19. 外部等待预算

对同一个外部任务，单次回复累计最多等待 3 分钟。短轮询、换 status query、换 connector 查询同一任务都不会重置预算。

3 分钟仍无 terminal result：立即结束本轮等待，报告 request/task/process ID、project/exact commit 和最后状态；本机任务可以继续；下一轮只查询原任务；不重复提交；停止等待不等于 cancel；无 terminal 证据不宣布结果。

## 20. Restart / reconnect

同一 CWapi 进程内 Slack Socket reconnect 从 durable cursor 后继续，terminal response 可在当前会话复用；app-server 退出后续调用可重建。CWapi 应用重启建立新会话，不拾取上一进程未完成 request、不重放旧 Slack history、不自动 replay ambiguous side effect。

## 21. 错误处理

优先看 `status + error.code + error.category + error.message + retryable`。常见方向：

```text
MCP_PROCESS_CONTEXT_REQUIRED → discovery project_id + exact commit
MCP_PROJECT_CONTEXT_INCOMPLETE → 两字段成对
PROCESS_COMMAND_NOT_FOUND → 重新发现 executable / 修正路径
PROCESS_START_FAILED → 检查 command_path、stderr、系统启动错误
MCP_TOOL_REPORTED_ERROR → 进入具体 MCP tool 错误上下文
```

详细见 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。

## 22. 最终验收

最终结论至少对应：source commit、project ID、expected commit、permission mode/profile、MCP method + server/tool、真实 terminal response，以及必要的 stdout/stderr、DOM、artifact、Slack delivery 证据。

Web E2E 应证明：server 真启动、页面真访问、交互真执行、业务结果真读取、截图真返回（需要时）、server 真停止。不要用“命令被接受”“按钮能点”“旧 commit 曾通过”代替最终证据。