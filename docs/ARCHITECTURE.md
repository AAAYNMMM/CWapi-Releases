# CWapi v1.6.3 架构

CWapi 是单用户、单机 Windows 应用。系统只保留 Slack transport、Go Core、stock Codex app-server、Git workspace 和 Wails GUI 五个主要边界。

## 请求流

```text
Slack Socket Mode
  -> opening frame + strict JSON v2 decode
  -> route/scope validation（claim 前）
  -> SQLite current-session idempotency claim
  -> global context 或 repository-owned process-lifetime workspace
  -> repository lease
  -> Gateway virtual process runtime / stock Codex MCPHost
  -> terminal response durable
  -> Slack delivery state
```

## Core ownership

- Gateway：协议、route、fingerprint、idempotency、delivery 与 structured execution terminal truth。
- Workspace Manager：共享 bare mirror；每个 repository 一个 process-lifetime workspace；repository-level lease 串行同仓库任务，不同仓库互不阻塞。
- MCPHost：一个 permission generation；global thread 可复用，repository stock MCP context 仍按请求管理并在 terminal unsubscribe。
- Process Runtime：Codex/System 共用一个 registry、final invocation resolver、permanent policy 和 Token authorization mutex。
- Slack Runtime：Socket reconnect、当前会话 cursor、bounded in-memory transport index。
- Observability：有界 GUI/SQLite runtime log；workspace operational error 会进入现有 error/fatal 可见链，但 runtime log 不替代 authoritative request/process state。

## 启动顺序

`NewApp` 只创建 Wails facade。Wails SingleInstanceLock 成功后 `OnStartup` 才构造 Service：

1. 打开严格 config v2，并在任何 authorization/runtime 创建前原子重置 permission mode 为 `safe`；
2. 打开 SQLite schema 3 并执行 `ResetRuntimeSession`；
3. 创建 observability、Gateway、Codex/MCPHost、process runtime；
4. startup sweep 删除上一进程遗留的 repository workspaces，并对 shared mirrors 执行安全 prune；
5. 启动 Slack supervisor。

config/safe reset/state/Core 构造失败才 fatal。`full_access` 只在当前进程生命周期有效，Slack channel 不受 reset 影响。workspace root 完整性异常会阻止新的 repository readiness；global/status/Slack 仍可继续工作。

## Repository 与 Git

唯一 identity parser 接受 GitHub HTTPS owner/repo，输出 lowercase identity 和 canonical remote URL。该 identity 同时用于 mirror key、persistent workspace key、fingerprint、Token binding 与日志字段。

目录模型：

```text
CWapi-data/workspaces/git/
  mirrors/<repository-hash>.git
  repositories/<repository-hash>/
```

`mirrors/` 是跨 CWapi 进程复用的 bare mirror。`repositories/` 只在当前 CWapi 进程生命周期内存在：startup sweep 清理上次异常退出残留，normal shutdown 也会统一清理；request terminal 不删除 repository workspace。

首次访问 repository 时：

1. 获取该 repository 的 lease；
2. 创建或复用 bare mirror；
3. 若 exact commit 不在 mirror 中才 fetch/prune；
4. 创建 persistent detached workspace，或把现有 workspace 的 tracked source 强制同步到 exact commit；
5. 校验 HEAD 精确等于请求的 40hex commit；
6. 只要求 tracked state clean，不主动清除 ignored/untracked derived state。

因此 `target/`、`node_modules/`、`.venv/`、`build/`、`dist/` 等项目衍生物可以在同一 CWapi 进程内、同一 repository 的后续请求中自然复用。CWapi 不判断这些衍生物是否健康；若项目自身检测到缓存损坏，应通过项目命令进行清理。

同一 repository 的 task 在 lease 上串行。不同 repository 使用独立 lease，可以并行执行。长进程、Codex denial 和后续 System Token fallback 在真正 terminal 前继续持有原 repository lease，避免同仓库 tracked source 被另一个任务切换。

MinGit 子进程使用绝对 executable 与最小环境；实际 repository Git I/O 前可按需解析当前用户已有的外部 `gh auth git-credential` helper。系统不执行强制版本/登录预检，不修改 global Git config，也不继承 GH/GitHub/CWapi/Slack/OpenAI secret 或 debug env。

## Workspace lifecycle

### Request terminal

普通 repository request terminal 后只执行 `Release`：释放 repository lease，不删除 workspace。derived state 因而保留给后续任务。

### Normal shutdown

Service 先停止接收新任务并收口 owned process，再对每个 repository 获取 lease 后删除 process-lifetime workspace；最后对 mirrors 执行 metadata prune。shared mirrors 保留。

### Startup recovery

startup `Sweep` 只删除已知 repository workspace root 的直接 children，不跟随 symlink/reparse point；随后 prune mirrors。由于 sweep 在 Service 接收新任务之前执行，不会与当前进程 active repository task 竞争。

workspace prepare/release/sweep/prune 的网络、认证、Git 与 cleanup failure 除了维持原 MCP/structured execution 错误语义，还必须通过现有 observability runtime logger 暴露给 GUI 最新错误候选，并继续执行 secret redaction。

## Global context

global stock MCP 的 CWD 固定为 `CWapi-data/temp/mcp-global`。safe 模式唯一 global 动态 writable root 就是该目录；不解析 repository 凭据、不创建 Git tree。

## Process architecture

`process_start/status/stop` 是 Gateway/Core virtual tools，不进入 MCPHost。start 在 Core 解析最终 executable、argv、real cwd，执行 permanent policy，再由 Codex safe backend 启动。

`full_access` 仍然 Codex-first。仅当 Codex 返回 CWapi 认可的结构化 sandbox permission denial 时才允许签发一次性 System Token。下一条新 request_id 必须使用相同 repository、commit 和 final invocation；Core 在原 repository workspace 上重新校验 binding/policy 后消费 Token并启动 System backend。

每个 Codex process 使用独立 CODEX_HOME/client/sandbox root；不热改 shared MCPHost CODEX_HOME。所有 owned process tree 使用 Windows Job Object 收口。

## Desktop

Wails 只暴露轻量 snapshot/mutation binding。GUI 是固定 `375 × 690`、不可拉伸的单页窗口；窗口与标题栏由 WebView 绘制全不透明宇宙渐变，不启用 Windows backdrop、layered alpha 或额外 native 背景窗口。

Desktop snapshot 读取 authoritative process registry，并分别公开 latest structured execution event 与 latest runtime error。routine runtime info 不进入 GUI。workspace operational error 复用同一 runtime error 通道，不新增第二套日志页面。

前端会在 process state/output、execution event、runtime error 和本地 mutation 之间选择最新可见记录，但有一条 failure-truth 约束：**同一 request 的 `mcp.delivery/delivered` acknowledgment 不得覆盖对应 runtime failure**。不同 request 的新事件仍可正常替换旧错误。active process 才显示 `[STOP]`。

## v1.6.3 released package

最终发布 source/tag：`d89e019e8a7acf340e8d38694939689b403d6523`。

最终 portable SHA-256：`6d559d0f80a0de3631c1b3bf1685abfbf4b7f11ad7eddbe61af63d0dd8b40c3d`。

发布包已完成 Wails production build、packaged real-chain 与 final deep-ZIP privacy；`v1.6.3` tag / GitHub Release 已发布。
