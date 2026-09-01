# Changelog

## 2.0.3 — 2026-09-01

2.0.3 将 Agent 外部协议转换从 broker/runtime 核心中抽离，并建立轻量的正式协议边界：

- 新增 canonical conversation/message/tool definition/tool call/tool result/completion/error/stream chunk 类型，稳定保留 role、tool call ID 与 task/correlation metadata；
- 新增 OpenAI-compatible Adapter 与 canonical MCP bridge codec，broker 不再直接解析客户端协议；
- 新增确定性 Context Optimizer，只规范化 metadata、JSON tool result 和可证明重复的 system/developer/tool 状态，不调用第二个 AI、不删除重复 user task；
- `/v1/models` 明确报告 adapter capabilities：streaming/tools/parallel tools 支持，images/files 不支持；
- SSE 继续使用 keepalive + buffered completion chunks，不伪造 token-level streaming，同时保留未来真实流式扩展边界；
- 产品、配置、GUI 与发行源码版本统一为 `2.0.3`。

发行包对应开发仓库源码提交：

```text
8941aa5d41768993c01e7798678a485f56331691
```

## 2.0.2 — 2026-09-01

2.0.2 集中修复长任务中的状态推进、MCP 交互和防空转行为：

- 明确 Web GPT 是唯一任务控制器；CWapi 只维护 bridge/OpenAI request 状态，本地软件只执行工具；
- `agent_exchange` 对不含 tool call 的终态 response 以 `state=responses` 立即确认；含 `tool_calls` 时在同一 exchange 内继续 bounded wait，以便直接领取 tool-result follow-up request；
- `no_request` 只由真实 wait timeout 产生，仅表示等待窗口内没有新的本地 OpenAI request，不能用于推断本地 command 正在运行；
- exchange 增加 revision、changed、pending、inflight、active、idle count、wait time、last state/error 与 next action 等结构化 `activity`；
- request 交付增加 timing/state 信息，并支持可选 `metadata.task_id` / `metadata.correlation_id`；`request_id` 明确不是第三方 command session；
- assistant function arguments 与 JSON content 支持原生 JSON 输入并规范化，语义相同的 native/string tool arguments 使用 canonical fingerprint 保持幂等；
- Coding 正式 catalog 固定为 `coding_open`、`coding_exec`、`coding_status`、`coding_close`；Coding/Agent 都不提供文件或图片传输；
- 产品、配置、前端和 portable/candidate manifest 版本统一为 `2.0.2`。

发行包对应开发仓库源码提交：

```text
05068c482a617c6beb2acd5c6d2ff15cfedc7598
```

## 2.0.1 — 2026-09-01

- 收紧 Coding 永久执行策略，使 resolver 后的最终 executable/argv/CWD 在进程启动前再次校验；
- FULL 对通过永久策略的命令使用 `dangerFullAccess`，SAFE 继续使用隔离的 `workspaceWrite`；两种 profile 都不绕过破坏性 Git、凭据读取、禁止系统工具和 protected-path 规则；
- 修复 Windows SAFE 下 Go、npm、Vite/Vitest 的 Temp/cache/profile 与子进程兼容性；
- 移除 Coding/Agent 文件和图片传输，删除 `coding_attachment`、Agent inline media/attachment 临时存储与 MCP file/image content 输出；
- 增加 Windows CI 和相关 execution policy、MCP、前端 regression。

## 2.0.0 — 2026-08-31

CWapi 2.0 正式成为发行主线。

主要变化：

- 新增彼此隔离的 Coding MCP 与 Agent MCP；
- Coding 采用 durable repository workspace，可按 repository URL 恢复，并支持 exact commit guard；
- Web GPT 直接负责规划与编码，bundled Codex 仅作为 model-free `command/exec` 工具宿主；
- Agent 提供 localhost OpenAI-compatible `/v1/models` 与 `/v1/chat/completions`，把本地软件请求桥接给 Web GPT；
- Coding / Agent 使用独立 token、独立 MCP server、独立 tool catalog 与独立 OpenAI Secure MCP Tunnel 配置；
- `coding_attachment` 与 Agent inline attachment 边界收缩为 raster image；普通文本和文件不作为通用 MCP 文件资源传输；
- 配置升级为 `cwapi.config.v3 / 2.0.0`；
- portable 内置 OpenAI Codex `0.150.1`、MinGit `2.55.0.windows.4` 与 OpenAI `tunnel-client 0.0.10`；
- 发行包不包含用户数据，并通过 portable privacy gate；
- 发行仓库改为双路线：`main` 维护 2.x，`1.6.x` 保留 1.6.3 及旧版历史。

发行包对应开发仓库源码提交：

```text
d904ae80428c90717e050a151c65fa35b6b83c63
```

## 1.6.x

1.6.3 及更早发行仍保留在 `1.6.x` 分支和已有 GitHub Releases 中。2.x 与 1.6.x 不共享同一工作流或配置结构。
