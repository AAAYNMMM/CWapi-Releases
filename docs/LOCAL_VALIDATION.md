# CWapi v1.6.0 本地验证

## Source checks

常规 Go 改动：

```powershell
gofmt -d
go test ./...
go vet ./...
git diff --check
```

前端/Wails 改动按需运行 typecheck、React build、Wails Windows build。

可选的真实 remote exact-commit integration 不绑定发行者自己的验收仓库。启用 `CWAPI_RUN_REMOTE_EXACT_COMMIT=1` 时，同时提供 `CWAPI_REMOTE_EXACT_REPOSITORY`、`CWAPI_REMOTE_EXACT_URL` 与 40 位 `CWAPI_REMOTE_EXACT_COMMIT`，再运行对应 Go test。

## Public portable gate

发行包必须从 clean exact commit 构建：

```powershell
$commit = (git rev-parse HEAD).Trim()

powershell -NoProfile -ExecutionPolicy Bypass -File automation/stage_v160_portable.ps1 `
  -ExpectedCommit $commit `
  -RuntimeSourceRoot .

powershell -NoProfile -ExecutionPolicy Bypass -File automation/validate_v160_portable_release.ps1 `
  -ExpectedCommit $commit
```

第二个 gate 会检查 archive privacy、Go `-trimpath`、构建身份字符串、runtime 版本，并把 ZIP 解压到不同盘符且含空格/中文的路径，从无关 working directory 启动真实 GUI。仅在 gate 通过后记录 ZIP SHA-256。

## Stock relay smoke

持续确认：

- `codex app-server --stdio` 可 initialize；
- experimental app-server API 已启用；
- 仅允许三个 stock MCP relay 方法；
- caller `threadId` 被拒绝；
- context 使用 `thread/start` / `ephemeral=true`；
- 正常路径无 `turn/start`；
- source 中无 custom MCP Tool catalog。

## Permission smoke

验证：

- safe 生成/选择 `cwapi-safe`；
- full_access 生成/选择 `cwapi-full-access`；
- safe roots 包含配置项目 + CWapi data root；
- full profile 不使用 `:danger-full-access`；
- `rules/default.rules` 可由 stock Codex 解析；
- permission/project 变化后 context fingerprint 变化；
- system-path deny 和基础 forbidden execpolicy 存在。

## Slack smoke

- Socket Mode connect/ACK；
- configured channel/source；
- request receive/response delivery；
- duplicate request；
- request_id conflict；
- reconnect/backoff；
- 同一 CWapi 进程内 terminal result redelivery；
- unsupported method 明确失败。

真实 token 不进仓库/fixture。

## Recovery

- app-server crash 后可重建；
- 同一 CWapi 进程内 terminal result 不丢；
- CWapi restart 后旧任务、日志与 Slack history 不恢复；
- ambiguous side-effect call 不自动 replay；
- shutdown 后无 CWapi-owned process 泄漏。

## MCP server trust

实际启用的 local stdio MCP server 必须单独验证启动边界。不能因为 thread 带 permissions 就假定它自动受 filesystem sandbox。

## Same-commit rule

最终证据必须属于同一个 40 位 source commit。任何代码修复生成新 commit 后，旧 candidate 的最终验收不代表新源码。
