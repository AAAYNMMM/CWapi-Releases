# CWapi v1.6.0 Web GPT Entry

这是新会话接手当前项目的最短入口。

## 原则

```text
简单
稳定
高效
```

## 当前链路

```text
Web GPT
 -> GitHub：源码 / exact commit
 -> Slack MCP envelope
 -> CWapi relay
 -> stock Codex app-server
 -> configured MCP server
 -> MCP response / Slack File
```

GitHub 是源码/commit 事实来源。CWapi 不实现 custom Toolhost/workspace 平台，Codex relay 不启动 model Turn。

## 当前文档读取顺序

1. `PROJECT_PRINCIPLES.md`
2. `V1_6_0_STAGE_PLAN.md`
3. `ARCHITECTURE.md`
4. `SECURITY.md`
5. `PROTOCOL.md`
6. `SLACK_TRANSPORT.md`
7. `CODEX_TOOLHOST.md`
8. `RUNTIME_PACKAGE.md`
9. `CHATGPT_WORKFLOW.md`
10. `DEVELOPMENT.md`
11. `ACCEPTANCE.md`

历史路线查 Git history/CHANGELOG。

## Web GPT 只需要提供什么

项目相关调用：

```text
request_id
project_id
expected_commit
method
params
```

其中 `project_id + expected_commit` 必须一起出现。CWapi 自己负责：

```text
project lookup
 -> Git mirror fetch
 -> detached exact-commit worktree
 -> Codex thread/start(cwd + permissions)
 -> stock MCP call
```

Web GPT 不需要知道本机 project path、mirror path、worktree path、Codex `threadId`、CODEX_HOME 或 profile ID。

纯状态查询 `mcpServerStatus/list` 可以不带项目上下文。

## Remote MCP

只发送：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

不要发送旧 `workspace.open/test.run/automation.run/fs.*` custom Tool 名称。

通用本地命令通过 configured MCP server `cwapi` 调用：

```json
{
  "server": "cwapi",
  "tool": "process_start",
  "arguments": {
    "command": "powershell.exe",
    "argv": ["-NoProfile", "-NonInteractive", "-Command", "<command>"],
    "cwd": "."
  }
}
```

随后用 `process_status(process_id)` 查询，必要时用 `process_stop(process_id)` 停止。Web GPT 自己选择安装器、语言版本和安装位置；CWapi 不解释或管理。不得把 secret 放入 command/argv。

`command` 可写 PATH 名称、绝对路径或 `cwd` 相对路径，例如 `C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe`、`.venv/Scripts/python.exe`、`node_modules/.bin/tool.cmd`。Windows 路径进入 MCP JSON 前必须把 `\` 全部转换为 `/`；正式请求不使用 `C:\\...`。不要在字符串内部再包引号。native executable 保持结构化 argv；`.cmd/.bat` 必然按 Windows command-script 语义执行。

## 文件 / 图片 / 日志

如果 MCP 已经合法返回内容，CWapi 才考虑外发：

- 短文本直接进入 MCP response；
- 长文本/日志、图片、resource text/blob 转 Slack File；
- 单个文件最大 8 MiB；
- 单次 response 最多 16 个文件；
- 只上传 MCP 已返回的内容，不根据本地 path/URI 自己额外读取文件；
- 超限明确失败，不截断，也不重新调用可能有副作用的 MCP tool。

因此“能读本地文件”和“能把文件传到 Slack”不是同一个权限。

## Permissions

- 默认 safe -> `cwapi-safe`；
- 显式 full_access -> `cwapi-full-access`；
- Codex-managed 基础层不约束 packaged command MCP；通用命令以当前 Windows 用户权限运行；
- 权限绑定 Codex context，不由 Slack parser 判断具体指令。

## 等待

同一外部任务单轮累计等待最多 **3 分钟**。到上限仍在运行就停止本轮等待，报告 task/request id、exact commit 与最后状态；下一轮查询原任务，不重复提交。停止等待不等于 cancel。

## 完成定义

v1.6.0 已完成实现和用户验收；收口证据见 `V1_6_0_STAGE_PLAN.md`、`ACCEPTANCE.md`、`RELEASE_CHECKLIST.md`。后续任何代码变化重新适用 same-commit Gate，不能沿用 v1.6.0 候选证据。
