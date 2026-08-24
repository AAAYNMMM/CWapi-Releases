# CWapi v1.6.1 Release Checklist

状态：`DONE`。这是已完成 v1.6.1 的历史 closeout 记录，不是当前待办。只有用户明确要求新的正式 portable/Release 时，才为该新交付启用一份新的 checklist；文档变更或开发者源码检查不会自动重开本任务。除非用户明确要求，不创建 tag 或 GitHub Release。

## Source

- [x] exact clean commit；
- [x] gofmt / vet / full Go tests / diff check；
- [x] frontend clean install / tests / build；
- [x] Wails Windows build；
- [x] documentation/line-limit/legacy scan。

## Runtime

- [x] pinned Codex version/commit/hash；
- [x] P0 model-free/sandbox/System/Job/secret gate；
- [x] MinGit/Node/Playwright/browser lock 与 manifest 一致；
- [x] first-run 和 relocation；
- [x] portable 无 user data、gh、旧 Node CWapi MCP。
- [x] trimpath 与 stage/ZIP privacy gate；无凭据、Token、日志、数据库、仓库、browser profile 或构建机身份/绝对路径。

## Protocol / process

- [x] v2 only + old v1 guidance；
- [x] strict route/scope/token-before-claim；
- [x] private + second repo exact commit；
- [x] unique trees/global root；
- [x] one registry limits/tails/cleanup/status/stop；
- [x] 3 Token、TTL、binding、race、dirty System fallback；
- [x] restart/second-instance/reparse/redaction。

## Real Slack

- [x] Codex success；
- [x] denial -> Token -> new-id System success；
- [x] long/status/stop；
- [x] duplicate/redelivery；
- [x] public evidence Token redacted；
- [x] request/process IDs 已记录。

## Closeout

- [x] P0-P7 progress 为 DONE；
- [x] formal docs 是 v1.6.1 当前事实；
- [x] final HEAD clean，所有证据 same implementation commit；
- [x] 未执行未授权 tag/Release。
