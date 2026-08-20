# CWapi v1.6.0 Web GPT Entry

这是 **Web GPT 使用 CWapi v1.6.0 的唯一必读入口**。

目标：先发现当前能力和项目，再对 exact commit 做结构化本机调用。不要预读整套内部文档，也不要沿用 v1.5.1 的 Gmail / Runner / custom Tool 工作流。

---

## 1. 当前链路

```text
Web GPT
 -> GitHub：读取 / 修改源码并取得 exact commit
 -> Slack：发送 CWapi MCP frame
 -> CWapi：project discovery / exact-commit / state / delivery
 -> stock Codex app-server
 -> configured MCP server
 -> MCP response / Slack File
```

Web GPT 负责决策；CWapi 不规划项目；正常 MCP relay 不启动 Codex model Turn。

---

## 2. 新会话先做 discovery

优先调用：

```text
mcpServerStatus/list
```

它会返回 stock MCP catalog，并由 CWapi 附加 discovery 信息，包括：

```text
source_commit
request_methods
projects
project_context
process_tools
process_start_modes
command_path_forms
```

如果只需要项目列表，可直接调用：

```text
projects/list
```

参数必须为空对象：

```json
{}
```

不要猜 `project_id`。

---

## 3. Slack frame 必须使用正式格式

正式请求：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

其中 `REQUEST_ID` 必须和 JSON 中的 `request_id` 一致。

例如 project discovery：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][GPTCWAPIPROJECTS01]
{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"GPTCWAPIPROJECTS01","method":"projects/list","params":{}}
+++
```

普通频道聊天不是 CWapi request。

---

## 4. 项目调用必须绑定 exact commit

项目相关请求必须同时提供：

```text
project_id
expected_commit
```

`expected_commit` 必须是目标 GitHub repository 的完整 40 位 SHA。

Web GPT 自己负责从 GitHub 确认要验证的 commit；CWapi 负责：

```text
project lookup
 -> Git mirror fetch
 -> verify expected_commit
 -> detached exact-commit worktree
 -> Codex thread/start(cwd + permissions)
 -> MCP call
```

Web GPT 不需要也不应提供：

```text
本机 project path
mirror path
worktree path
Codex threadId
CODEX_HOME
permission profile ID
_cwapi_workspace
_cwapi_expected_commit
_cwapi_request_id
```

---

## 5. 当前 request method

CWapi discovery method：

```text
projects/list
```

stock app-server MCP relay method：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

不要发送旧版：

```text
workspace.open
workspace.status
workspace.close
git.rev_parse
test.run
build.run
automation.run
fs.*
process.*
resources.read
cwapi-dev
```

注意：`cwapi/process_start` 是 **MCP server/tool**，不是旧式顶层 `process.*` method。

---

## 6. 调用本机进程

当前随包 local MCP server：

```text
server = cwapi
```

工具：

```text
process_start
process_status
process_stop
```

### process_start 示例

人话：

> 在当前项目 exact commit 的工作区中，用指定 Python 启动 `server.py`。

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

这个对象放进外层：

```text
method = mcpServer/tool/call
params = 上面的对象
```

如果返回：

```text
state = running
process_id = proc-...
```

后续查询同一个进程，不重复启动。

---

## 7. 环境由 Web GPT 管理

CWapi 不决定目标项目应该使用哪个 Python / JDK / SDK / Node / Go / Rust 版本。

正常策略：

```text
检查本机已有环境
   ↓
确认项目要求
   ↓
找到合适 executable → 直接使用
   ↓
没有 → 选择安装方式和持久位置
   ↓
再把准确 command + argv 交给 CWapi
```

优先使用准确 executable，例如：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
C:/Program Files/Git/cmd/git.exe
D:/SDK/tool.exe
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
```

`process_start.command` 支持：

```text
PATH executable name
absolute executable path
working-directory-relative executable path
```

native executable 的 argv 直接传入；`.cmd/.bat` 使用 Windows command-script 语义。

---

## 8. Windows 路径规则

进入 MCP JSON 前，统一把 Windows `\` 转为 `/`：

```text
C:/Projects/example/.venv/Scripts/python.exe
D:/SDK/jdk/bin/java.exe
//server/share/tool.exe
```

正式工作流不要发送：

```text
C:\\Projects\\...
```

也不要给 `command` 值额外套一层引号。

这是为了避免 JSON + Slack 文本的反斜杠转义问题。

---

## 9. exact-commit worktree 是临时上下文

CWapi 在项目调用中准备 detached exact-commit worktree，并在调用与附件处理完成后释放。

因此不要假定：

```text
请求 A 在 workspace 创建 .venv
```

之后：

```text
请求 B 一定还能看到同一个 .venv
```

需要跨请求长期复用的环境，更适合放在明确持久位置，并通过绝对 executable 路径调用；或者按项目需要重新创建。

---

## 10. Playwright

浏览器能力通过 configured Playwright MCP 调用。

典型流程：

```text
process_start 本地服务
 -> browser_navigate localhost
 -> fill / click
 -> browser_evaluate 验证真实结果
 -> browser_take_screenshot
 -> process_stop
```

不要把“页面能打开”当成业务功能已经通过。需要继续读取实际 DOM / 状态 / 输出。

例如：

```js
() => ({
  result: document.querySelector('#result')?.textContent,
  status: document.querySelector('#status')?.textContent,
  title: document.title
})
```

---

## 11. 文件 / 图片 / 日志外发

文件读取权限和 Slack 外发权限是两层：

```text
MCP 是否取得内容
 -> MCP 已返回 text/blob/image
 -> CWapi outbound policy
 -> Slack message / Slack File
```

当前规则：

- 短文本 inline；
- 长文本/日志、image、resource text/blob 使用 Slack File；
- 单个 artifact 最大 8 MiB；
- 单次 response 最多 16 个 artifact；
- 超限明确失败，不静默截断；
- CWapi 不根据 result 中出现的 path/URI 自己额外读取本地文件；
- delivery 失败不自动 replay 有副作用的 tool。

Slack file 引用写入 `MCP_RESPONSE.resources`，包含 media type、SHA-256、size。

---

## 12. 权限边界

Codex-managed execution：

```text
safe        -> cwapi-safe
full_access -> cwapi-full-access
```

但是 packaged `cwapi` command/process MCP 启动的自由 executable 以当前 Windows 用户权限运行，**不自动继承 Codex thread profile 的 filesystem / execpolicy sandbox**。

因此：

- 只对用户明确配置的项目使用自由 command 能力；
- 不把 token / password / private key 放进 command / argv；
- 需要认证时优先使用本机已经配置的凭据机制；
- 不把 `safe` 误解为“任意子进程都只能写项目目录”。

---

## 13. Duplicate 与副作用

- 同 request ID + 同 fingerprint 不执行第二次；
- fingerprint 包含 `project_id + expected_commit + method + canonical params`；
- 同 request ID + 不同 fingerprint -> conflict；
- terminal duplicate 可返回当前会话已有 compact response / Slack file 引用；
- ambiguous side-effect MCP call 不自动 replay。

如果一个 process 已经返回：

```text
process_id = proc-...
```

后续查原 process，不重新提交 `process_start`。

---

## 14. 等待预算

对**同一个外部任务或等待目标**，单次回复累计最多等待：

```text
3 分钟
```

规则：

- 第一次 sleep / poll / status query 开始累计；
- 短轮询不会重置预算；
- 3 分钟仍无 terminal result，立即停止本轮等待；
- 报告 request/task/process ID、exact commit、最后状态；
- 本地任务可以继续运行；
- 下一轮只查询原任务；
- 不重复提交；
- 停止等待不等于 cancel；
- 无 terminal 证据不宣布成功。

---

## 15. 常见错误的处理方向

```text
MCP_PROCESS_CONTEXT_REQUIRED
 -> 补真实 project_id + exact expected_commit

MCP_PROJECT_CONTEXT_INCOMPLETE
 -> project_id / expected_commit 必须成对

PROCESS_COMMAND_NOT_FOUND
 -> 重新发现 executable / 检查绝对或相对路径

PROCESS_START_FAILED
 -> 看 command_path / stderr / 系统启动错误

ERR_CONNECTION_REFUSED
 -> 检查服务是否 running、端口是否正确；stop 后出现则通常正常
```

详细排障见 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。

---

## 16. 完整工作流参考

正常使用读到这里已经够了。

只有需要更多细节时再继续：

- [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)：完整开发与环境管理流程；
- [`PROTOCOL.md`](PROTOCOL.md)：Slack MCP envelope / response；
- [`SECURITY.md`](SECURITY.md)：权限与 trusted command boundary；
- [`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)：transport / delivery；
- [`OPERATIONS.md`](OPERATIONS.md)：运行与维护。