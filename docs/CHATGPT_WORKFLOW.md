# CWapi v1.6.0 Web GPT Workflow

## 当前链路

```text
Web GPT
 -> GitHub：源码/文档/exact commit
 -> Slack：MCP envelope
 -> CWapi：relay/exact-commit/state/delivery
 -> stock Codex app-server
 -> configured MCP server
 -> result / Slack File
```

Web GPT 负责决策；CWapi 不规划；正常 MCP relay 不启动 Codex model Turn。

## 开发流程

1. 在 GitHub 查看或修改源码并获得 exact commit；
2. 需要本地能力时，通过 Slack 发送 `request_id + project_id + expected_commit + method + params`；
3. CWapi 校验 envelope、来源、request fingerprint 与幂等；
4. CWapi 根据 `project_id` 找到配置项目，fetch Git mirror，创建 detached `expected_commit` worktree；
5. CWapi 创建/复用 stock Codex app-server，并为该调用建立带 `cwd + permissions` 的 ephemeral context thread；
6. CWapi 只转发 `mcpServerStatus/list`、`mcpServer/resource/read` 或 `mcpServer/tool/call`；
7. MCP 返回后，CWapi 只对**已经返回的内容**执行外发策略：短文本 inline，长文本/日志、图片、resource text/blob 转 Slack File；
8. 需要附件时先取得 Slack file 引用，再生成 compact terminal response，写本地 state 并投递 Slack；
9. exact-commit worktree 在本次调用及附件处理完成后释放；
10. 新 commit 的最终验证必须重新使用新的 `expected_commit`，不能沿用旧提交证据。

CWapi 不再提供 `workspace.open`、`test.run`、`automation.run` 等自定义 Tool 合同。Web GPT 也不管理本机 worktree path、Codex `threadId`、CODEX_HOME 或 profile ID。

## 通用命令

安装、环境检查和服务启动由 Web GPT 选择实际 executable 与 argv，再调用 `cwapi/process_start`。CWapi 不识别 Python/Java/Node 等语言，也不决定全局、用户级或项目级安装位置。

```text
process_start(command, argv, cwd?)
 -> process_id + running/completed/failed
 -> process_status(process_id)
 -> process_stop(process_id)  # 需要时
```

PowerShell/cmd 作为普通 executable 使用。命令初始 CWD 绑定 exact-commit workspace，但子进程以当前 Windows 用户权限运行，并不受 Codex thread profile filesystem/execpolicy sandbox。不要在 command/argv 中发送 secret。

选择环境时优先直接传准确 executable：`.venv/Scripts/python.exe`、`C:/Program Files/Java/jdk-25/bin/java.exe` 或其它 SDK/portable 路径都可用。相对路径以 `cwd` 为基准；绝对路径可在 workspace 外。**Windows 路径进入 MCP JSON 前，Web GPT 必须把所有 `\` 转换为 `/`**，统一使用 `C:/...`、`.venv/Scripts/...` 或 `//server/share/...`。不得在正式工作流中发送 `C:\\...`，也不要把 executable 外层引号放进 `command` 字符串。

## 文件读取与外发是两层权限

```text
Codex/MCP 是否允许取得内容
 -> CWapi outbound policy
 -> Slack message / Slack File
```

CWapi 不因为 MCP 结果里出现 `C:\...`、`file://...` 或其他 URI 就自行读取该路径。只有 MCP 已经返回的 text/blob/image 数据才可能被外发。

当前 outbound policy：

- 单个 artifact 最大 8 MiB；
- 单次 MCP response 最多 16 个 artifact；
- 超限明确失败；
- 不静默截断；
- 不因 delivery 失败自动重放有副作用的 MCP tool；
- Slack file 引用写入 `MCP_RESPONSE.resources`，包含 media type、SHA-256、size。

## 权限

权限不从 Slack 文本判断。

- safe -> Codex `cwapi-safe`；
- full_access -> Codex `cwapi-full-access`；
- 基础层 execpolicy / filesystem deny 只约束 Codex-managed execution，不约束 packaged command MCP；
- permission/project 配置改变后，下次调用重建必要的 Codex context；
- exact-commit worktree 只改变本次调用 `cwd`，不把 Git 同步职责交给 MCP server。

## 外部等待预算

对**同一个外部任务或等待目标**，单次回复累计最多等待 **3 分钟**。

- 第一次 sleep/poll/status query 开始累计；
- 30 秒轮询不会重置；
- Slack/Gmail/Connector 之间切换查询同一个任务也不会重置；
- Tool 调用自身已阻塞接近或超过 3 分钟时，返回后的第一机会即视为预算耗尽；
- 3 分钟仍无 terminal result，立即结束本轮并报告 request/task id、exact commit、最后状态；
- 本地任务可继续运行，下一轮只查询原任务，不重复提交；
- 停止等待不等于 cancel；当前 relay 不伪造 request-scoped cancellation；
- 无 terminal 证据不宣布结果。

## Slack / duplicate

- duplicate same request 不执行第二次；
- fingerprint 包含 `project_id + expected_commit + method + canonical params`；
- request_id + 不同 fingerprint -> conflict；
- 当前 CWapi 运行会话内，已 terminal response 可以重投 compact response / 已存在的 Slack file 引用；
- ambiguous side-effect MCP call 不自动 replay。

## 最终证据

最终验收至少记录：

```text
source commit
packaged Codex hash
permission profile
project_id + expected_commit
MCP method
terminal response
real Slack path
clean before/after
```

v1.6.0 已完成收口；当前验收状态见 `V1_6_0_STAGE_PLAN.md`。任何后续代码变化都必须生成新的 exact-commit 验收候选。
