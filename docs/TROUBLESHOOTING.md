# CWapi v1.6.0 故障排查

这份文档只回答：**出了什么问题，下一步查哪里。** 正常使用见 [`USER_GUIDE.md`](USER_GUIDE.md)，Slack 从零配置见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

## 1. CWapi 启动 / 发行包

### CWapi 启动不了
确认 Windows 11 x64、ZIP 已完整解压、`CWapi.exe` 与 `runtime/` 在同一便携目录、安装目录可写。不要在 ZIP 内直接运行，也不要混用不同版本的 `runtime/`。

首次运行后会生成 `CWapi-data/`。如果刚移动安装位置，移动整个目录而不是只移动 exe。

## 2. Slack 问题

完整 scopes / Socket Mode / token / Channel ID 步骤统一看 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

### “验证并保存”失败

```text
SLACK_APP_TOKEN_INVALID
→ App Token 必须是 xapp-...

SLACK_BOT_TOKEN_INVALID
→ Bot Token 必须是 xoxb-...

SLACK_READINESS_BOT_IDENTITY_FAILED
→ Bot Token 无效 / 被撤销 / Workspace 不匹配

SLACK_READINESS_CHANNEL_FAILED
→ Channel ID 错、缺 read scope、Bot 没加入频道

SLACK_BOT_NOT_CHANNEL_MEMBER
→ 把 CWapi Bot 加入控制频道

SLACK_READINESS_SOCKET_FAILED
→ 检查 Socket Mode、xapp token、connections:write
```

### 能保存，但 Slack 很快 degraded
public channel 重点检查 `channels:read`、`channels:history`、`message.channels`；private channel 检查 `groups:read`、`groups:history`、`message.groups`。修改 scopes 后要 **Reinstall to Workspace**。

### 能收请求但不能回文本
检查 `chat:write`。

### 文本正常但截图 / 文件失败
检查 `files:write`。

### `SLACK_API_ERROR_missing_scope`
添加缺失 scope 后 **Reinstall to Workspace**。

### CWapi 没收到 Web GPT 消息
确认 Slack Transport healthy、Web GPT 发到同一 Workspace / Channel ID、Bot 在频道内、Event Subscription 正确。

正式协议消息还必须是完整 frame：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

CWapi 不会自动执行启动前已经存在的旧频道消息。

## 3. Project / exact commit

### Web GPT 不知道 project_id
调用 `projects/list` 或 `mcpServerStatus/list`，不要猜，也不要照搬别的安装实例里的 ID。

### `MCP_PROCESS_CONTEXT_REQUIRED`
外层 request 同时提供 `project_id + expected_commit`。

### `MCP_PROJECT_CONTEXT_INCOMPLETE`
只提供了二者之一；必须成对出现。

### `MCP_PROJECT_ID_INVALID`
重新 discovery 当前真实 `project_id`。

### `MCP_EXPECTED_COMMIT_INVALID`
必须使用完整 40 位 Git SHA，不能用短 SHA。

### exact commit 准备失败
检查项目 Git 地址、commit 是否属于该仓库、网络 / Git 凭据，以及 Web GPT 是否真的拿到当前新 commit。旧 commit 的测试结果不能证明新 commit 已通过。

## 4. MCP / process tool

### `MCP_CWAPI_TOOL_UNAVAILABLE`
当前 `cwapi` process server 只公开：`process_start`、`process_status`、`process_stop`。先用 `mcpServerStatus/list` 看真实 catalog。

### `MCP_PROCESS_ARGUMENTS_INVALID`
arguments 不符合当前 tool schema，重新读取 catalog。

### `MCP_PROCESS_CONTEXT_MANAGED`
caller 不允许传 `_cwapi_workspace`、`_cwapi_expected_commit`、`_cwapi_request_id`；这些由 CWapi 注入。

## 5. 环境 / executable

### `PROCESS_COMMAND_NOT_FOUND`
检查 command 是 PATH 名称、absolute executable path 还是 working-directory-relative path。

例如：

```text
python.exe
C:/Program Files/Git/cmd/git.exe
tools/build.cmd
.venv/Scripts/python.exe
```

### Python 明明装了但找不到
先发现环境：`where.exe python`、`where.exe py`、`py.exe -0p`。找到真实 Python 后直接传准确路径。Java/JDK、Node、Go、Rust、Android SDK、CUDA 同理。

### Windows 路径怎么写
MCP JSON 统一使用 `/`，例如 `C:/Program Files/Java/jdk-25/bin/java.exe`。不要给 `command` 再额外套一层引号。

### 上一次创建的 `.venv` 下一次没了
exact-commit worktree 是临时执行上下文。需要长期复用的环境放明确持久位置并用 absolute executable，或者按项目需要重建。

### `PROCESS_START_FAILED`
看 `command_path`、`command_resolution`、`stdout_tail`、`stderr_tail` 和系统启动错误。`ENOENT` 通常表示 executable 或启动路径未找到。

## 6. process 生命周期

### 返回 `state = running` 是失败吗
不是。长期 server 正常会返回 `running + process_id`。后续查同一个 `process_id`，不要重新 start。

### `process_stop` 后 exit_code 不是 0
主动终止长期服务不一定自然退出。优先看 `state = stopped`，再验证端口 / 服务是否真的关闭。

### localhost `ERR_CONNECTION_REFUSED`
启动阶段出现时，检查 process 是否 running、stdout 是否 ready、stderr、端口和 bind 地址。若在 `process_stop` 后出现，通常说明服务确实已停止。

## 7. Playwright / Web E2E

### 页面能打开但功能不对
不要只看 navigate / click。继续读取真实业务状态：

```text
navigate → fill / click → browser_evaluate / DOM read → verify
```

### `browser_evaluate` 报 JavaScript 错
检查 function 语法、selector、页面加载状态，以及 JSON / Slack 字符串转义。function 尽量保持简短。

## 8. Slack File / 本地文件

### 截图为什么变成 Slack File
图片不是短文本。CWapi outbound policy 会按 Slack File 交付。当前限制：单 artifact ≤ 8 MiB，单 response ≤ 16 artifacts。

### result 里有本地路径，为什么不自动上传
只有 MCP **已经取得并返回**的 text/blob/image/resource content 才进入 outbound policy。路径字符串本身不会触发 CWapi 私自读取本地文件。

## 9. Duplicate / 长任务

### duplicate request 为什么不执行第二次
CWapi 用 request fingerprint 做幂等。same request 不应再次启动副作用任务；真正的新动作使用新的 request ID。

### Web GPT 为什么 3 分钟就停止等待
这是等待预算，不是 cancel。同一个外部任务单次回复累计最多等待 3 分钟；到上限仍无 terminal result 时，Web GPT 报告当前 ID / commit / 状态，下一轮继续查询原任务，不重复提交。

## 10. 权限问题

### safe 为什么还能运行自由 command
这是两套边界：Codex-managed execution 使用 `cwapi-safe / cwapi-full-access`；packaged `cwapi` command/process MCP 以当前 Windows 用户权限运行。自由 executable 不自动继承 Codex thread sandbox。

### full_access 是不是关闭所有保护
不是。它扩大 Codex-managed filesystem permission，但 CWapi 自己的 secret、幂等、owned process、delivery 等边界仍存在。完整解释见 [`SECURITY.md`](SECURITY.md)。

## 11. 仍然定位不了问题

从 CWapi“诊断”页收集最小必要信息：

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

专项文档：Slack [`SLACK_SETUP.md`](SLACK_SETUP.md)；运行维护 [`OPERATIONS.md`](OPERATIONS.md)；安全 [`SECURITY.md`](SECURITY.md)；Web GPT [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)；协议 [`PROTOCOL.md`](PROTOCOL.md)。