# CWapi v1.6.0 Web GPT Entry

这是 **Web GPT 使用 CWapi v1.6.0 的唯一必读入口**。这里只保留执行所需规则；人类安装、Slack 配置、GUI、运维不在这里重复。

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

优先：

```text
mcpServerStatus/list
```

读取 stock MCP catalog 与 CWapi 附加的 `cwapi.discovery.v1`。

只需要项目列表时：

```text
method = projects/list
params = {}
```

不要猜 `project_id`。

## 3. 项目调用绑定 exact commit

项目 request 同时提供：

```text
project_id
expected_commit   # GitHub 仓库完整 40 位 SHA
```

CWapi 自己完成 project lookup、Git fetch、commit verification、detached worktree 与 context 建立。

Web GPT 不提供本机 worktree path、Codex `threadId`、`CODEX_HOME`、profile ID，也不能注入 `_cwapi_workspace`、`_cwapi_expected_commit`、`_cwapi_request_id`。

## 4. 当前 request methods

CWapi 自己处理：

```text
projects/list
```

stock app-server relay：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

不要沿用 v1.5.1 的 Gmail / Runner / `workspace.open` / `test.run` / `build.run` / `automation.run` / `fs.*` / 顶层 `process.*` 等旧合同。

## 5. Slack frame

正式 request：

```text
+++
[CWapi/MCP/1][MCP_REQUEST][REQUEST_ID]
{JSON body}
+++
```

subject ID 与 JSON `request_id` 必须一致。

## 6. 本机命令走 cwapi process tools

当前：

```text
server = cwapi
process_start
process_status
process_stop
```

`process_start.command` 支持：

```text
PATH executable name
absolute executable path
working-directory-relative executable path
```

Windows MCP JSON 路径统一使用 `/`，不要给 `command` 值再套引号。

如果 `process_start` 返回：

```text
state = running
process_id = proc-...
```

后续查询同一个 `process_id`，不要重复 start；测试结束需要时 `process_stop`。

## 7. 环境职责

目标项目的 Python / JDK / Go / Rust / SDK 等环境由 Web GPT 或用户发现、安装、选择和管理。

原则：

```text
先读项目版本要求
→ 检查已有环境
→ 已有则复用
→ 缺失才安装
→ 使用准确 executable
```

exact-commit worktree 是临时执行上下文，不要默认一次 request 在其中生成的 `.venv` 会跨 request 永久存在。

完整环境与进程策略见 [`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)。

## 8. Playwright

典型 localhost E2E：

```text
process_start server
→ browser_navigate
→ fill / click
→ browser_evaluate / DOM read 验证真实结果
→ screenshot（需要时）
→ process_stop
```

不要把“页面打开了”或“按钮点了”当成业务通过。

不要使用 Playwright unsafe arbitrary-code 能力冒充通用 shell / build runner；本机命令走 `cwapi/process_start`。

## 9. 文件 / 图片 / 日志

CWapi 只外发 **MCP 已经返回的内容**：短文本 inline；长文本 / 日志、image、resource text/blob 可转 Slack File。

当前主要限制：

```text
单 artifact ≤ 8 MiB
单 response ≤ 16 artifacts
```

CWapi 不因为 result 中出现本地 path / URI 就自行读取对应文件。

## 10. 权限边界

Codex-managed execution：

```text
safe        -> cwapi-safe
full_access -> cwapi-full-access
```

packaged `cwapi` command/process MCP 启动的自由 executable 以当前 Windows 用户权限运行，**不自动继承 Codex thread filesystem / execpolicy sandbox**。

不要把 token、password、private key 放进 command / argv。

## 11. Duplicate 与等待

fingerprint 绑定：

```text
project_id + expected_commit + method + canonical params
```

same request 不重复执行；ambiguous side-effect call 不自动 replay。

对同一个外部任务，单次回复累计最多等待 **3 分钟**。到上限仍无 terminal result：报告 request/task/process ID、exact commit 和最后状态；下一轮只查询原任务；不重复提交；停止等待不等于 cancel。

## 12. 需要更多细节时

- 完整 Web GPT 工作流：[`CHATGPT_WORKFLOW.md`](CHATGPT_WORKFLOW.md)
- 错误排查：[`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)
- 协议格式：[`PROTOCOL.md`](PROTOCOL.md)
- 安全边界：[`SECURITY.md`](SECURITY.md)

Slack App 怎么创建和配置是用户安装问题，统一见 [`SLACK_SETUP.md`](SLACK_SETUP.md)，不要让 Web GPT 通过普通消息索要用户的 `xapp-...` / `xoxb-...`。