# CWapi v1.6.3 Logging

日志用于定位问题，不是 authoritative request/process state。

## Structured execution

记录 request_id、repository identity/commit（如有）、MCP method、execution/delivery state、elapsed 与 terminal error code。fingerprint、response 和 delivery truth 来自当前会话 SQLite。

GUI 不展示 structured execution 列表；desktop snapshot 最多公开最新一个 event，与进程和 runtime error 按时间戳选择最终显示项。

## Runtime log

记录 Core startup/shutdown、Slack connect/reconnect、Codex/MCPHost、workspace prepare/release、process cleanup、degraded component 和 operational error。CWapi 不再维护独立 Diagnostics/活动错误面板；故障以 `error` 级别直接进入此日志。

完整 runtime log 不进入单页 GUI，routine startup/connect 信息也不公开。desktop snapshot 最多公开最新一个 error/fatal record；若它是当前最新数据，GUI 以 `level=... message=...` 等原始 `key=value` 字段显示。该 viewport 不滚动、不保留可见历史。

## 有界数据

- GUI：只公开一个最新候选；live observability：内部有界窗口；
- SQLite observability：独立有界 retention；
- process stdout/stderr：各保存最后 8192 bytes；
- 不创建 per-process 完整日志文件；
- 大 MCP 结果可按需转 Slack File，不逐行刷 Slack。

## Secret

credential、browser cookie/session、System Token 和敏感 env 不写普通日志。常见 Slack/GitHub/OpenAI token 形态与 keyed secret 会在 observability 入口脱敏。

System Token 的 transport 例外：raw v2 Slack/当前会话 response 可保存以支持重投；Wails public snapshot 只按 schema 对顶层 `system_token` 脱敏，不扫描普通 64hex。

## 验收

- 两类日志独立；
- process tail 8KiB 且 UTF-8 有效；
- operational error 直接进入 runtime log 并带稳定 fingerprint；
- GUI 只显示真实时间戳最新记录，无完整日志列表、滚动或伪造进度；
- secret canary 不出现在 child/tail/public GUI；
- restart 后旧运行会话不进入当前 UI；
- delivery failure 不重跑有副作用调用。
