# CWapi v1.6.0 Logging

日志用于回答：当前请求是什么、状态如何、耗时多少、哪里失败。日志不是第二套 authoritative state。

## 两类日志

### Structured MCP log

记录：

- request_id；
- project_id / expected_commit（项目相关调用）；
- stock MCP method；
- start/elapsed；
- execution state；
- delivery state；
- terminal result/error 摘要。

### CWapi runtime log

记录：

- startup/shutdown；
- Slack connect/reconnect；
- stock Codex app-server startup/recovery；
- context permission profile / cwd 变化；
- exact-commit workspace prepare/release；
- Slack file delivery；
- process cleanup；
- runtime warnings/errors。

不再记录 custom Toolhost/workspace state 为当前产品事实。

## State boundary

request terminal truth、fingerprint 和 delivery state 来自 Go/SQLite，不从日志文本反推。

## Retention

CWapi **没有“日志只保留最近 10 条”的产品约束**。

当前实现使用有界 retention 防止无限增长：

- GUI/live observability 只保留最近的有界窗口；
- SQLite persistent observability 也有独立上限；
- request/state 真相不依赖 GUI 日志窗口；
- 调试时按 request/error 选择必要范围，不默认把整库或整份大日志上传 Slack。

有界 retention 是资源控制，不是“只能查看 10 条”的协议限制。

## 大输出 / Slack

- Slack 不是逐行 stdout；
- 短结果留在 MCP response；
- MCP 已返回的长文本/日志可转 Slack File；
- 单个 artifact 最大 8 MiB，单次 response 最多 16 个；
- CWapi 不因日志中出现 path/URI 就额外读取本地文件；
- 不高频重读完整 diagnostics；
- 不因为 UI refresh 重启 app-server/context。

## Secrets

Slack/Git/API credential、browser session/cookie、敏感 env 不得进入普通日志、MCP payload 或可上传 artifact。

## Acceptance

- 两类日志独立；
- request elapsed/status 正确；
- duplicate error 聚合；
- secret scan PASS；
- restart 时清空上一运行会话的 request/event/runtime log/error，旧记录不进入当前 UI；
- Slack 断线不导致 terminal 调用重跑；
- 不存在固定 10 条 retention 合同。
