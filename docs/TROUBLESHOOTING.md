# CWapi v1.6.0 故障排查

按“你看到什么现象”排查。推荐顺序：先看 CWapi 诊断页，再看本页；需要更多内部细节再查 `OPERATIONS.md`、`SECURITY.md`、`PROTOCOL.md`。

## 1. CWapi 启动不了

确认 Windows 11 x64、ZIP 已完整解压、`CWapi.exe` 与 `runtime/` 仍在同一便携目录、当前目录可写。不要在 ZIP 内运行，也不要把新旧版本的 runtime 混在一起。

正常至少包含：

```text
CWapi.exe
portable-manifest.json
runtime/
```

首次运行后还会生成 `CWapi-data/`。如果刚移动安装位置，确保移动的是整个目录。

## 2. Slack 未连接 / CWapi 没收到消息

检查 `App Token (xapp-...)`、`Bot Token (xoxb-...)`、`Channel ID (C...)`、Workspace 和目标频道。CWapi 每次启动建立新会话，不会自动回头执行启动前已经存在的旧频道消息。

正式请求必须是完整 frame：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

## 3. Web GPT 不知道 project_id

不要猜。调用 `projects/list`（`params={}`）或 `mcpServerStatus/list`，从 `cwapi.discovery.v1` 读取当前配置项目。项目列表为空时先去 CWapi“项目”页添加项目。

## 4. `MCP_PROCESS_CONTEXT_REQUIRED`

含义：process tool 没拿到完整项目执行上下文。处理：`projects/list` 获取真实 `project_id`，GitHub 获取完整 40 位 commit SHA，然后在**外层 request**同时提供 `project_id + expected_commit`。不要把它们塞进 `process_start.arguments`。

## 5. `MCP_PROJECT_CONTEXT_INCOMPLETE`

含义：只提供了 `project_id` 或 `expected_commit` 其中一个。项目相关请求要求二者成对出现。

## 6. `MCP_PROJECT_ID_INVALID`

当前格式类似：

```text
prj-0123456789abcdef01234567
```

不要自己生成、从别的 CWapi 安装实例复制或长期缓存旧 ID。重新调用 `projects/list`。

## 7. `MCP_EXPECTED_COMMIT_INVALID`

`expected_commit` 必须是完整 40 位 SHA，不要用 `2a45c3b` 这种短 SHA。最终测试也不能拿旧 commit 的证据证明新 commit 已通过。

## 8. exact commit 准备失败

检查 repository / remote URL、commit 是否属于这个仓库、本机网络和 Git 凭据、Web GPT 是否拿到真正的新 SHA。CWapi 会做 Git mirror fetch + commit verification，不会因为某个本地目录“看起来是最新”就跳过验证。

## 9. `MCP_CWAPI_TOOL_UNAVAILABLE`

当前 packaged `cwapi` process server 只公开：

```text
process_start
process_status
process_stop
```

先用 `mcpServerStatus/list` 读取当前 catalog，不要沿用旧工具名。

## 10. `MCP_PROCESS_ARGUMENTS_INVALID`

`arguments` 不是合法 object 或不符合当前 tool schema。先读 MCP catalog 中真实 schema，再构造参数。

## 11. `MCP_PROCESS_CONTEXT_MANAGED`

caller 不允许提供 `_cwapi_workspace`、`_cwapi_expected_commit`、`_cwapi_request_id`；这些由 CWapi 注入。Web GPT 只提供正常业务参数与外层项目上下文。

## 12. `PROCESS_COMMAND_NOT_FOUND`

检查 `command` 属于哪种形式：PATH 名称、绝对 executable、working-directory-relative 路径。

```text
python.exe
C:/Program Files/Git/cmd/git.exe
tools/build.cmd
.venv/Scripts/python.exe
```

相对路径必须存在于**本次 exact-commit workspace**。上一次请求临时生成的 `.venv` 不保证下一次还存在。

## 13. Python 明明装了但 `python.exe` 找不到

Windows 可能是 `python` 不在 PATH，但 `py.exe` 存在。Web GPT 应先发现环境：`where.exe python`、`where.exe py`、`py.exe -0p`，再选择真实 Python 路径。CWapi 不负责猜安装位置。

同理适用于 Java/JDK、Node、Go、Rust、Android SDK、CUDA 等：先发现和确认版本，再安装或选择准确 executable。

## 14. 绝对路径 / 带空格路径怎么写

正式 MCP JSON 使用 `/`：

```text
C:/Users/name/Tools/python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
```

不要给 `command` 值再包一层引号。结构化 command 字段能处理路径空格，不需要为了空格退化成一整段 shell 字符串。

## 15. `PROCESS_START_FAILED`

表示进程启动阶段失败。继续看 `command_path`、`command_resolution`、stderr 和系统错误。底层 `ENOENT` 通常表示 executable 或启动所需路径未找到；先确认实际环境，不要直接把问题归咎于项目脚本。

## 16. process 返回 `running` 算失败吗

不算。长期 server 正常可能返回：

```text
state = running
process_id = proc-...
```

后续使用 `process_status(process_id)`；测试完成再 `process_stop(process_id)`。不要重复提交新的 `process_start`。

## 17. `process_stop` 后 exit_code 不是 0

主动终止长期进程可能不是自然退出，所以 exit code 不一定为 0。判断 stop 是否成功优先看 `state = stopped`，再验证服务 / 端口是否真的关闭。

## 18. localhost `ERR_CONNECTION_REFUSED`

启动后立即出现：检查 process 是否仍 running、stdout 是否 ready、stderr 是否崩溃、端口是否正确、server 是否绑定 localhost / 127.0.0.1。

如果是在 `process_stop` 后重新访问出现，通常反而证明服务已经停止。

## 19. Playwright 页面能打开但功能结果不对

不要只验证 navigate / click 成功。继续读取真实业务结果：

```text
navigate → fill / click → browser_evaluate / DOM read → verify
```

“按钮点了”不是“功能正确”。

## 20. `browser_evaluate` JavaScript 报错

检查 function 是否为合法 JavaScript、selector 是否正确、页面是否加载、元素是否存在，以及 JSON / Slack 转义是否破坏字符串。尽量保持 function 简短，例如：

```js
() => ({
  result: document.querySelector('#result')?.textContent,
  title: document.title
})
```

## 21. 截图为什么变成 Slack File

图片不是短文本。Playwright 返回 image content 后，CWapi outbound policy 会作为 Slack File 交付，`MCP_RESPONSE.resources` 记录 `uri / media_type / sha256 / size_bytes`。

当前限制：单个 artifact 最大 8 MiB，单 response 最多 16 个 artifact。

## 22. 为什么本地文件路径不会自动上传

这是刻意的权限边界。MCP result 只返回 `C:/Projects/report.log` 时，CWapi不会自行打开它。只有 MCP 已经取得并返回 text/blob/image/resource content 后，才进入 outbound policy。

## 23. duplicate request 为什么不执行第二次

fingerprint 绑定 `project_id + expected_commit + method + canonical params`。同 request ID + 同 fingerprint 不重复执行；同 ID + 不同 fingerprint 属于 conflict。新动作使用新 request ID。

## 24. Web GPT 为什么等 3 分钟就停

这是等待预算，不是 cancel。对同一个外部任务，单次回复累计最多等待 3 分钟；到上限仍无 terminal result 时，Web GPT 报告 request/task/process ID、exact commit 和最后状态，下一轮继续查询原任务，不重复提交。

## 25. CWapi 重启后旧 request 为什么不恢复

每次启动建立新运行会话：不重放启动前 Slack history，不自动恢复上一进程未完成 request；同一进程内 Socket reconnect 可从 durable cursor 后继续。这样避免无法确认状态的副作用任务在重启后再次执行。

## 26. safe 模式为什么还能运行自由 command

这是两层边界：Codex-managed execution 受 `cwapi-safe / cwapi-full-access` profile 和 execpolicy；packaged `cwapi` command/process MCP 的自由 executable 以当前 Windows 用户权限运行，不自动继承 thread sandbox。

所以 `safe` 不等于“所有任意本机子进程只能访问项目目录”。详细见 [`SECURITY.md`](SECURITY.md)。

## 27. full_access 是不是关闭所有保护

不是。它扩大 Codex-managed filesystem permission，但 CWapi 仍保留 secret、idempotency、owned process、delivery 等边界，也不使用裸 `:danger-full-access` 作为默认 profile。

## 28. 本机命令需要 token / password

不要直接放进 `command`、`argv`、Slack 消息、GitHub commit、artifact 或截图。优先使用 Windows Credential Manager、已登录 CLI、工具自己的 credential store 或本机既有认证状态。

## 29. 还不能定位问题怎么办

收集必要且最小的信息：

```text
CWapi version / source_commit
permission mode
project_id + expected_commit
request_id + method + server/tool
terminal status + error.code + error.message
process_id（如果有）
必要的 stdout_tail / stderr_tail
```

不要为了排障直接上传整个 `CWapi-data`、Credential Manager 内容或所有日志。更多信息见 [`OPERATIONS.md`](OPERATIONS.md)、[`SECURITY.md`](SECURITY.md)、[`PROTOCOL.md`](PROTOCOL.md)、[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。