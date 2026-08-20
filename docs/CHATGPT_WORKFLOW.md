# CWapi v1.6.0 Web GPT Workflow

这份文档是 `WEB_GPT_ENTRY.md` 的完整参考版。

如果只是第一次让 Web GPT 使用 CWapi，先读 [`WEB_GPT_ENTRY.md`](WEB_GPT_ENTRY.md)；只有需要环境管理、exact-commit 生命周期、长任务、附件、权限或最终验收细节时再看这里。

## 当前链路

```text
Web GPT
 -> GitHub：源码 / 文档 / exact commit
 -> Slack：CWapi MCP frame
 -> CWapi：discovery / relay / exact-commit / state / delivery
 -> stock Codex app-server
 -> configured MCP server
 -> result / Slack File
```

Web GPT 负责决策；CWapi 不规划项目；正常 MCP relay 不启动 Codex model Turn。

---

## 1. 每个新会话的起点

新会话不要直接拿旧 `project_id`、旧 tool schema 或旧 runtime 假设开工。

先 discovery：

```text
mcpServerStatus/list
```

当前 CWapi 会在 stock status 结果中附加：

```text
cwapi.discovery.v1
source_commit
request_methods
projects
project_context
process_tools
process_start_modes
command_path_forms
```

如果只需要项目列表，调用：

```text
projects/list
```

```json
{}
```

得到当前实例真实：

```text
project_id
display_name
repository
source_commit
```

不要推算随机 `project_id`。

---

## 2. GitHub 是源码与 commit 事实来源

Web GPT 负责通过 GitHub：

- 阅读源码；
- 阅读当前文档；
- 修改文件；
- 创建 commit；
- 获取完整 40 位 commit SHA；
- 确认最终要验证的是哪个 commit。

本机测试不能用“看起来应该是最新”的工作树代替 exact commit。

每次代码变化后，如果要宣称新版本已经通过测试，必须重新使用新的：

```text
expected_commit
```

旧 commit 的测试证据不能自动继承给新 commit。

---

## 3. Slack frame

正式 CWapi request：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

JSON：

```json
{
  "schema": "cwapi.mcp.request.v1",
  "protocol_version": "cwapi-mcp/1",
  "request_id": "REQUEST_ID",
  "method": "...",
  "params": {}
}
```

项目相关调用再加：

```json
{
  "project_id": "prj-...",
  "expected_commit": "0123456789abcdef0123456789abcdef01234567"
}
```

subject 与 JSON `request_id` 必须一致。

普通频道聊天不是 protocol request。

---

## 4. Request method 分两层

### CWapi discovery

```text
projects/list
```

### stock app-server MCP relay

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

不要混淆：

```text
cwapi/process_start
```

不是顶层 method，而是：

```text
method = mcpServer/tool/call
server = cwapi
tool = process_start
```

旧版这些 custom Tool 合同不再使用：

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

---

## 5. 项目调用的 exact-commit 生命周期

项目 request 带：

```text
project_id + expected_commit
```

CWapi 内部执行：

```text
project_id
 -> configured repository
 -> Git mirror fetch
 -> verify expected_commit
 -> detached isolated worktree
 -> thread/start(cwd=worktree, permissions=profile)
 -> MCP call
 -> artifact handling / delivery
 -> release worktree
```

Web GPT 不管理：

```text
local mirror path
managed worktree path
Codex threadId
CODEX_HOME
profile ID
_cwapi_* context fields
```

如果只给 `project_id` 或只给 `expected_commit`，属于不完整项目上下文。

---

## 6. 环境管理职责

v1.6.0 的明确职责分工：

```text
Web GPT：发现 / 安装 / 选择 / 管理目标项目环境
CWapi：在 exact-commit 上结构化执行
```

CWapi 不识别“这是 Python 项目所以应该装 Python 3.12”，也不判断“这个 Java 项目应该使用哪个 JDK”。

Web GPT 应结合：

- 项目 README；
- `pyproject.toml` / `requirements.txt`；
- `package.json`；
- `go.mod`；
- `Cargo.toml`；
- Gradle / Maven 文件；
- CI 配置；
- lockfile；
- 项目已有脚本；
- 本机已存在环境；

决定需要什么。

---

## 7. 环境发现优先于安装

不要看到 `python` 不在 PATH 就立刻安装第二份 Python。

先检查可用环境。

Windows 常见发现方式：

```text
where.exe
Get-Command
py.exe -0p
工具自己的 --version / version
```

例如 Python：

```text
where.exe python
where.exe python3
where.exe py
py.exe -0p
```

Node：

```text
where.exe node
node.exe --version
```

Go：

```text
where.exe go
go.exe version
```

Rust：

```text
where.exe cargo
cargo.exe --version
rustc.exe --version
```

Java：

```text
where.exe java
java.exe -version
```

如果已经找到满足项目要求的环境，就直接复用。

---

## 8. 环境缺失时如何处理

如果项目确实缺少 runtime / SDK，Web GPT 可以根据项目和用户权限选择安装方式。

原则：

1. 先确定项目要求的版本；
2. 优先使用正常、可验证的官方或项目既有安装方式；
3. 明确安装位置；
4. 安装完成后验证 executable 与版本；
5. 后续通过准确 executable 调用；
6. 不把 token / password 放入 command / argv；
7. 不为了演示“会安装”而重复安装已有环境。

CWapi 不替 Web GPT 维护一个强制全局环境目录。

---

## 9. `process_start` 的 command 选择

`process_start.command` 支持三种形式：

### PATH executable name

```text
python.exe
node.exe
go.exe
cargo.exe
```

适合当前 PATH 已经唯一、明确时。

### absolute executable path

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Git/cmd/git.exe
C:/Program Files/Java/jdk-25/bin/java.exe
D:/SDK/tool.exe
```

适合多版本并存、portable toolchain、不在 PATH 的 SDK。

### working-directory-relative executable path

```text
.venv/Scripts/python.exe
tools/build.cmd
node_modules/.bin/tool.cmd
```

适合 executable 确实存在于当前 exact-commit workspace 的情况。

native executable argv 直接传入；`.cmd/.bat` 按 Windows command-script 语义执行。

---

## 10. Windows path canonicalization

正式 MCP JSON 中，把 Windows `\` 转成 `/`：

```text
C:/Projects/example/tool.exe
.venv/Scripts/python.exe
//server/share/tool.exe
```

不要正式发送：

```text
C:\\Projects\\example\\tool.exe
```

也不要把整个 executable 外层引号写进 `command` 值：

错误：

```text
"C:/Program Files/Java/jdk-25/bin/java.exe"
```

正确：

```text
C:/Program Files/Java/jdk-25/bin/java.exe
```

路径中的空格由结构化 command 字段处理，不需要退化成 shell quoting。

---

## 11. exact-commit worktree 不等于持久环境目录

工作区在本次调用和附件处理结束后释放。

因此：

```text
请求 A：python -m venv .venv
```

成功后，不应该假设：

```text
请求 B：.venv/Scripts/python.exe
```

一定还能看到 A 创建的临时文件。

需要跨请求持久复用的环境：

- 放在明确持久位置并使用 absolute executable；或者
- 每次按项目需要重新准备。

这和 exact-commit 的干净执行语义并不冲突。

---

## 12. 通用进程流程

启动：

```text
process_start(command, argv, cwd?)
```

返回可能是：

```text
completed
```

或者：

```text
running + process_id
```

长任务：

```text
process_status(process_id)
```

结束长期服务：

```text
process_stop(process_id)
```

不要用新 `process_start` 代替原 process 的 status 查询。

---

## 13. Shell 什么时候用

PowerShell / cmd 只是普通 executable：

```text
powershell.exe
cmd.exe
```

如果某个操作确实需要 shell 语义，可以结构化调用它。

但如果目标只是运行一个明确 executable：

```text
C:/Program Files/Git/cmd/git.exe --version
```

优先直接：

```text
command = C:/Program Files/Git/cmd/git.exe
argv = [--version]
```

不要没必要地变成：

```text
powershell -> command string -> quoting -> executable
```

直接 command + argv 更容易审计，也更少转义问题。

---

## 14. Web 应用 E2E 推荐流程

典型本机 Web 测试：

```text
1. process_start 启动本地 server
2. 确认 state=running / ready output
3. Playwright browser_navigate localhost
4. 填表 / 点击 / 交互
5. browser_evaluate 读取真实业务结果
6. 必要时 screenshot
7. process_status 确认服务仍正常
8. process_stop
9. 可选：再次 navigate，确认端口已关闭
```

不要只验证“首页打开了”。

例如测试加法页面，应该最终读取：

```text
result = 42
status = calculated
```

而不是只记录：

```text
click succeeded
```

---

## 15. Playwright `browser_evaluate`

适合读取当前页面实际状态。

示例：

```js
() => ({
  result: document.querySelector('#result')?.textContent,
  status: document.querySelector('#status')?.textContent,
  title: document.title
})
```

保持 function 简洁、可解释。

不要使用 Playwright 的 unsafe arbitrary-code 能力去冒充通用 shell / build runner；本机命令已经有明确的 `cwapi/process_start` 路径。

---

## 16. 文件读取与外发是两个权限层

```text
Codex / MCP 是否能取得内容
 -> MCP 已返回内容
 -> CWapi outbound policy
 -> Slack message / Slack File
```

CWapi 不会因为 MCP result 中出现：

```text
C:/...
file://...
其它 URI
```

就自行打开对应本地文件。

当前 outbound policy：

- 短文本 inline；
- 长文本/日志 -> `.txt` Slack File；
- image/audio data -> Slack File；
- resource text/blob -> Slack File；
- compact 后仍过大的结构化 result 可转 `.json` Slack File；
- 单个 artifact 最大 8 MiB；
- 单次 response 最多 16 个 artifact；
- 超限明确失败；
- 不静默截断；
- delivery 失败不重放有副作用的 tool。

`MCP_RESPONSE.resources` 记录：

```text
uri
media_type
sha256
size_bytes
```

---

## 17. Permissions

权限不从 Slack 文本猜测。

Codex-managed：

```text
safe        -> cwapi-safe
full_access -> cwapi-full-access
```

permission / project fingerprint 改变后，后续调用重建必要 context。

但是 packaged `cwapi` command/process MCP 是独立 trusted remote execution boundary：

- executable 以当前 Windows 用户权限运行；
- 不自动继承 Codex thread filesystem / execpolicy sandbox；
- command 可以是 workspace 外 absolute executable；
- 初始 cwd 仍绑定 prepared exact-commit workspace 或其真实子目录；
- secret 不进入 command 子进程环境；
- CWapi 只 stop 自己启动并记录的 process tree。

---

## 18. Duplicate / idempotency

fingerprint 包含：

```text
project_id
expected_commit
method
canonical params
```

规则：

- same request ID + same fingerprint -> 不执行第二次；
- same request ID + different fingerprint -> conflict；
- 当前运行会话内 terminal response 可重投已有 compact response；
- 已上传 artifact 引用可复用；
- ambiguous side-effect call 不自动 replay；
- Slack timestamp 不是业务幂等键。

新动作使用新 request ID。

---

## 19. 外部等待预算

对**同一个外部任务或等待目标**，单次回复累计最多等待：

```text
3 分钟
```

累计从第一次 sleep / poll / status query 开始。

以下都不会重置预算：

- 每次只等 30 秒；
- 换一种 status query；
- 在同一任务上切换不同 connector 查询。

达到上限仍无 terminal result：

1. 立即结束本轮继续等待；
2. 报告 request / task / process ID；
3. 报告 project / exact commit；
4. 报告最后状态；
5. 说明本机任务仍可继续运行；
6. 下一轮只查询原任务；
7. 不重复提交；
8. 停止等待不等于 cancel；
9. 无 terminal 证据不宣布成功或失败。

---

## 20. CWapi restart / Slack reconnect

当前语义：

- 同一 CWapi 进程内 Slack Socket reconnect：从 durable cursor 后继续；
- terminal response 在当前运行会话内可复用；
- app-server 退出：后续调用可重建；
- CWapi 应用重启：建立新会话，不拾取上一进程未完成 request，不重放旧 Slack history；
- ambiguous side-effect state 不自动恢复执行。

因此不要把“应用重启”当成“后台任务调度器恢复”。

---

## 21. 错误处理

优先看结构化：

```text
status
error.code
error.category
error.message
retryable
```

常见方向：

```text
MCP_PROCESS_CONTEXT_REQUIRED
 -> discovery project_id + GitHub exact commit

MCP_PROJECT_CONTEXT_INCOMPLETE
 -> project_id / expected_commit 成对

PROCESS_COMMAND_NOT_FOUND
 -> 重新发现 executable / 修正路径

PROCESS_START_FAILED
 -> 检查真实 command path、版本、stderr、系统启动错误

MCP_TOOL_REPORTED_ERROR
 -> 进入具体 MCP tool 的错误上下文，不要把 relay 成功和业务 tool 成功混为一谈
```

详细见 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。

---

## 22. 最终验收规则

最终结论至少对应：

```text
source commit
project_id
expected_commit
permission mode / profile
MCP method + server + tool
真实 terminal response
必要的 stdout/stderr / DOM / artifact 证据
真实 Slack response / Slack File（如果验收 delivery）
```

对于 Web E2E：

```text
server 真启动
页面真访问
交互真执行
业务结果真读取
截图真返回（需要时）
server 真停止
```

不要用：

```text
命令被接受
按钮能点击
日志看起来没报错
旧 commit 曾经通过
```

代替最终业务证据。

v1.6.0 已完成发行收口；后续任何代码变化重新适用 exact-commit 验证，不能沿用旧候选证据。