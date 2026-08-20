# CWapi v1.6.0 故障排查

这份文档只回答：**出了什么问题，下一步查哪里。**

正常使用教程见 [`USER_GUIDE.md`](USER_GUIDE.md)，Slack 从零配置见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。这里不重复完整配置流程。

## 1. CWapi 启动 / 发行包

### CWapi 启动不了

确认：

```text
Windows 11 x64
ZIP 已完整解压
CWapi.exe 与 runtime/ 在同一便携目录
安装目录可写
```

不要在 ZIP 内直接运行，也不要把不同版本的 `runtime/` 混在一起。

首次运行后会生成：

```text
CWapi-data/
```

如果刚移动安装位置，移动整个 CWapi 目录，而不是只移动 exe。

## 2. Slack 问题

完整配置清单和 scopes 统一看 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

### “验证并保存”失败

按错误方向检查：

```text
SLACK_APP_TOKEN_INVALID
→ App Token 必须是 xapp-...

SLACK_BOT_TOKEN_INVALID
→ Bot Token 必须是 xoxb-...

SLACK_READINESS_BOT_IDENTITY_FAILED
→ Bot Token 无效 / 被撤销 / Workspace 不匹配

SLACK_READINESS_CHANNEL_FAILED
→ Channel ID 错、缺少 read scope、Bot 没加入频道

SLACK_BOT_NOT_CHANNEL_MEMBER
→ 把 CWapi Bot 加入控制频道

SLACK_READINESS_SOCKET_FAILED
→ 检查 Socket Mode、xapp token、connections:write
```

### 能保存，但运行后 Slack degraded

重点检查：

```text
public channel:
channels:read
channels:history
message.channels

private channel:
groups:read
groups:history
message.groups
```

修改 Bot scopes 后要 **Reinstall to Workspace**。

### 能收到请求，但不能回文本

检查：

```text
chat:write
```

### 文本正常，但 screenshot / Slack File 失败

检查：

```text
files:write
```

### `SLACK_API_ERROR_missing_scope`

Slack 已明确告诉你缺 scope。到 App 的 OAuth & Permissions 添加对应 scope，然后 **Reinstall to Workspace**。

### CWapi 没收到 Web GPT 消息

先确认：

```text
CWapi Slack Transport healthy
Web GPT 发到了同一个 Workspace / Channel ID
CWapi Bot 已加入频道
Event Subscription 正确
```

正式协议消息还必须是完整 frame：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

CWapi 每次启动建立新运行会话，不会自动执行启动前已经存在的旧频道消息。

## 3. Project / exact commit 问题

### Web GPT 不知道 project_id

不要猜。调用：

```text
projects/list
```

或：

```text
mcpServerStatus/list
```

从 discovery 取得当前安装实例的真实 `project_id`。

### `MCP_PROCESS_CONTEXT_REQUIRED`

process tool 缺少完整项目上下文。外层 request 同时提供：

```text
project_id + expected_commit
```

不要把 CWapi-managed context 字段塞进 tool arguments。

### `MCP_PROJECT_CONTEXT_INCOMPLETE`

只提供了 `project_id` 或 `expected_commit` 其中一个。二者必须成对出现。

### `MCP_PROJECT_ID_INVALID`

重新 `projects/list`。不要自己生成 `prj-...`，也不要照搬别的 CWapi 安装实例里的 ID。

### `MCP_EXPECTED_COMMIT_INVALID`

必须使用完整 40 位 Git SHA。短 SHA 不行。

### exact commit 准备失败

检查：

```text
项目 Git 地址是否正确
commit 是否属于该仓库
网络 / Git 凭据是否正常
Web GPT 是否真的取得当前新 commit
```

旧 commit 的测试结果不能自动证明新 commit 已通过。

## 4. MCP / process tool 问题

### `MCP_CWAPI_TOOL_UNAVAILABLE`

当前 `cwapi` process server 只公开：

```text
process_start
process_status
process_stop
```

先用 `mcpServerStatus/list` 看真实 catalog，不要沿用旧工具名。

### `MCP_PROCESS_ARGUMENTS_INVALID`

arguments 不符合当前 tool schema。重新读取 catalog 里的 schema。

### `MCP_PROCESS_CONTEXT_MANAGED`

这些字段由 CWapi 注入，caller 不允许传：

```text
_cwapi_workspace
_cwapi_expected_commit
_cwapi_request_id
```

## 5. 环境 / executable 问题

### `PROCESS_COMMAND_NOT_FOUND`

检查 command 属于哪一种：

```text
PATH 名称
absolute executable path
working-directory-relative path
```

例如：

```text
python.exe
C:/Program Files/Git/cmd/git.exe
tools/build.cmd
.venv/Scripts/python.exe
```

### Python 明明装了，但 `python` 找不到

先发现环境，不要立刻重装：

```text
where.exe python
where.exe py
py.exe -0p
```

找到真实 Python 后可以直接传准确 absolute executable。

Java/JDK、Node、Go、Rust、Android SDK、CUDA 等同理。

### Windows 路径怎么写

MCP JSON 统一使用 `/`：

```text
C:/Users/name/Tools/python.exe
C:/Program Files/Java/jdk-25/bin/java.exe
```

不要给 `command` 值再额外包一层引号。

### 上一次创建的 `.venv` 下一次为什么没了

exact-commit worktree 是临时执行上下文。请求 A 在其中生成的临时文件不保证请求 B 继续存在。

需要长期复用的环境放到明确持久位置，再通过 absolute executable 调用，或者按项目需要重建。

### `PROCESS_START_FAILED`

看返回中的：

```text
command_path
command_resolution
stdout_tail
stderr_tail
系统启动错误
```

`ENOENT` 通常表示 executable 或启动所需路径未找到。

## 6. process 生命周期

### 返回 `state = running` 是失败吗

不是。长期 server 正常会返回：

```text
state = running
process_id = proc-...
```

后续查同一个 `process_id`，不要重新 `process_start`。

### `process_stop` 后 exit_code 不是 0

主动终止长期服务不一定是自然退出。优先看：

```text
state = stopped
```

再验证端口 / 服务是否真的关闭。

### localhost `ERR_CONNECTION_REFUSED`

启动阶段出现：检查 process 是否 running、stdout 是否 ready、stderr、端口和 bind 地址。

如果是在 `process_stop` 之后出现，通常说明服务确实已经停止。

## 7. Playwright / Web E2E

### 页面能打开但功能不对

不要只看 navigate / click。继续验证真实业务状态：

```text
navigate
→ fill / click
→ browser_evaluate / DOM read
→ verify result
```

### `browser_evaluate` 报 JavaScript 错

检查 function 语法、selector、页面是否加载，以及 JSON / Slack 字符串转义。

保持 function 尽量短，例如：

```js
() => ({
  result: document.querySelector('#result')?.textContent,
  title: document.title
})
```

## 8. Slack File / 本地文件

### 截图为什么变成 Slack File

image content 不是短文本。CWapi outbound policy 会把它交付成 Slack File，并在 response resources 中记录 metadata。

当前限制：

```text
单 artifact ≤ 8 MiB
单 response ≤ 16 artifacts
```

### result 里有本地路径，为什么 CWapi 不自动上传

这是刻意的安全边界。只有 MCP **已经取得并返回**的 text/blob/image/resource content 才进入 outbound policy。

路径字符串本身不会触发 CWapi 私自读取本地文件。

## 9. Duplicate / 长任务

### duplicate request 为什么不执行第二次

CWapi 使用 request fingerprint 做幂等。same request 不应再次启动副作用任务。

如果你要发一个真正的新动作，使用新的 request ID。

### Web GPT 为什么 3 分钟就停止等待

这是等待预算，不是取消任务。

同一个外部任务单次回复累计最多等待 3 分钟；到上限仍没有 terminal result 时，Web GPT 应报告当前 ID / commit / 状态，并在下一轮继续查询原任务，不重复提交。

## 10. 权限问题

### safe 为什么还能运行自由 command

这是两套边界：

```text
Codex-managed execution
→ cwapi-safe / cwapi-full-access

packaged cwapi command/process MCP
→ 当前 Windows 用户权限
```

自由 executable 不自动继承 Codex thread sandbox。完整解释见 [`SECURITY.md`](SECURITY.md)。

### full_access 是不是关闭所有保护

不是。它扩大 Codex-managed filesystem permission，但 CWapi 自己的 secret、幂等、owned process、delivery 等边界仍然存在。

## 11. 仍然定位不了问题

先从 CWapi“诊断”页收集最小必要信息：

```text
CWapi version / source_commit
permission mode
project_id + expected_commit
request_id + method + server/tool
terminal status + error.code + error.message
process_id（如果有）
必要的 stdout_tail / stderr_tail
```

不要直接上传整个 `CWapi-data`、Credential Manager 内容或所有日志。

专项文档：

- Slack：[`SLACK_SETUP.md`](SLACK_SETUP.md)
- 运行维护：[`OPERATIONS.md`](OPERATIONS.md)
- 安全：[`SECURITY.md`](SECURITY.md)
- Web GPT：[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
- 协议：[`PROTOCOL.md`](PROTOCOL.md)