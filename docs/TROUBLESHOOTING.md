# CWapi v1.6.1 故障排查

这份文档只回答：**出了什么问题，下一步查哪里。** 正常使用见 [`USER_GUIDE.md`](USER_GUIDE.md)，Slack 从零配置见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

## 1. CWapi 启动 / 发行包

### CWapi 启动不了
确认 Windows 11 x64、ZIP 已完整解压、`CWapi.exe` 与 `runtime/` 在同一便携目录、安装目录可写。不要在 ZIP 内直接运行，也不要混用不同版本的 `runtime/`。

首次运行后会生成 `CWapi-data/`。移动时关闭 CWapi 并移动整个便携目录。

## 2. Slack 问题

完整 scopes / Socket Mode / token / Channel ID 步骤见 [`SLACK_SETUP.md`](SLACK_SETUP.md)。

### 配置验证失败

```text
SLACK_APP_TOKEN_INVALID
-> App Token 必须是 xapp-...

SLACK_BOT_TOKEN_INVALID
-> Bot Token 必须是 xoxb-...

SLACK_READINESS_BOT_IDENTITY_FAILED
-> Bot Token 无效 / 被撤销 / Workspace 不匹配

SLACK_READINESS_CHANNEL_FAILED
-> Channel ID 错、缺 read scope、Bot 没加入频道

SLACK_BOT_NOT_CHANNEL_MEMBER
-> 把 CWapi Bot 加入控制频道

SLACK_READINESS_SOCKET_FAILED
-> 检查 Socket Mode、App Token、connections:write
```

### 能收请求但不能回文本
检查 `chat:write`。

### 文本正常但截图 / 文件失败
检查 `files:write`。如果是 Playwright，确认 `browser_take_screenshot` 没有指定 `filename`，让工具返回真正的 MCP image content。

### CWapi 没收到 Web GPT 消息
确认 Slack healthy、Web GPT 和 CWapi 使用同一 Workspace/Channel、Bot 在频道内，并且消息是完整 MCP v2 frame：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

v1.6.1 不接受 MCP v1。

## 3. Repository / exact commit

v1.6.1 没有 project registry、`projects/list` 或 `project_id`。

repository-scoped request 必须同时提供：

```text
repository_url
expected_commit
```

### repository URL 被拒绝
只使用标准 GitHub HTTPS repository URL，例如：

```text
https://github.com/owner/repo
```

不要携带 userinfo、port、query、fragment 或额外 path。

### commit 被拒绝
`expected_commit` 必须是完整 40 位 Git commit，并且属于该 repository。

### private repository 准备失败
检查本机 GitHub CLI / GitHub 凭据是否能访问目标 private repository，以及 commit 是否真实存在。不要把 GitHub token 放进 MCP command/argv。

## 4. MCP route / process tool

当前允许的方法只有：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

`server=cwapi` 只接受：

```text
process_start
process_status
process_stop
```

### `MCP_REQUEST_JSON_INVALID`
先检查 JSON 本身，尤其是 Windows `\` 路径与嵌套 shell 字符串。正式远程路径优先使用 `/`，减少 JSON escape 错误。

### `MCP_SYSTEM_TOKEN_INVALID`
System Token 只能位于 request 顶层，格式必须符合 v2 协议。不要放进 params/arguments。

### request id conflict
同一个 request id 只能对应同一个 fingerprint。新动作或新状态快照使用新的 request id。

## 5. 环境 / executable

### `process invocation could not be resolved` / command not found
先把它当成 executable 解析失败，不要立即归因于项目代码。

按顺序检查：

```text
1. CWapi portable/runtime/tools/cache
2. 本机已有环境，而且必须是 CWapi 当前进程实际可见的环境
3. 两边都没有：停止猜路径
4. 用户切换 FULL 后由 Web GPT 安装，或用户手动安装
5. 安装后重新探测 executable/version
```

不要固定 `C:/WINDOWS/py.exe`、`python.exe`、`node.exe` 或某个用户目录作为通用规则。

### 本机终端能运行，CWapi 却找不到
CWapi 使用启动时冻结的 PATH。交互式 PowerShell 后来新增的 PATH 不一定对当前 CWapi 生效。找到实际 executable 后直接使用绝对路径，或重启 CWapi 后再验证。

### repository 内脚本 basename 找不到
优先直接使用 repository-relative command：

```text
tools/build.cmd
scripts/test.bat
```

不要为了找到脚本先改 cwd，再只传 basename。

### Windows path
MCP JSON 中优先使用：

```text
C:/Program Files/Tool/tool.exe
```

而不是未经正确 JSON 转义的反斜杠路径。

## 6. SAFE / FULL 权限

### SAFE 能不能调用本机已有程序
能否启动与能否写系统目录是两件事。SAFE 可以调用允许解析的本机程序，但 repository process 的写权限仍受控。

如果安装器、pip、npm 或其它工具尝试写受控范围外位置，可能得到权限拒绝。

### 什么时候切 FULL
确认任务确实需要安装软件或扩大本机写权限，并且 CWapi 自管环境与本机已有环境都不能满足需求时，由用户明确切换 FULL。

FULL 仍是 Codex-first。只有真实结构化 `PERMISSION_DENIED` 才进入短期 System Token fallback。FULL 不跨 CWapi 重启保留。

## 7. process 生命周期

### `state = running` 是失败吗
不是。长进程正常会返回 `running + process_id`。后续用新的 global request id 查询同一个 `process_id`，不要重新 start。

### `process_stop` 后 exit code 不好看
主动终止长期服务不一定自然返回 0。重点确认公开 state 是否为 `stopped`，以及服务/端口是否真的关闭。

### localhost `ERR_CONNECTION_REFUSED`
启动阶段出现时检查 process state、stdout/stderr、端口与 bind address；如果是在 stop 后验证，反而可能说明服务已正确停止。

## 8. Playwright / Web E2E

### navigate 成功，下一条 fill 却找不到元素
stock MCP context 是 request-scoped ephemeral。不同 request 不保证共享浏览器页面状态。

连续 E2E 优先在一次 Playwright 调用中完成；拆开时，每个后续 request 自己重建所需页面状态。

### 截图只得到 `./xxx.png`
这通常表示工具把截图保存在本地，并只返回了路径文本。CWapi 不会因为普通文本里出现本地 path/URI 就擅自读取文件。

需要传给 ChatGPT 时不传 `filename`，让 Playwright 返回 MCP `type=image` content，CWapi 再上传为 Slack File。

## 9. Duplicate / 长任务

### duplicate request 为什么不执行第二次
相同 request id + 相同 fingerprint 会返回已保存 response，不重复执行副作用。

### 为什么 3 分钟就停止等待
这是 Web GPT 工作流的等待预算，不是 cancel。

同一个外部任务连续等待/轮询累计最多 3 分钟；到上限仍无 terminal result 时，报告“任务仍在运行”、request/process id 和最后状态。下一轮继续查询原任务，不重复提交。

环境缺失不是等待事件。确认依赖不存在后立即进入安装决策。

## 10. 收集最小排障信息

优先收集：

```text
CWapi version
permission mode
repository URL + expected commit
request_id + method + server/tool
terminal status + error.code + error.message
process_id（如果有）
必要的 stdout_tail / stderr_tail
```

不要直接上传整个 `CWapi-data`、Credential Manager、Token 或所有日志。

专项文档：[`PROTOCOL.md`](PROTOCOL.md)、[`SECURITY.md`](SECURITY.md)、[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)、[`SLACK_TRANSPORT.md`](SLACK_TRANSPORT.md)。
