# CWapi v1.6.1 架构

CWapi 是单用户、单机 Windows 应用。系统只保留 Slack transport、Go Core、stock Codex app-server、Git workspace 和 Wails GUI 五个边界。

## 请求流

```text
Slack Socket Mode
  -> frame + strict JSON v2 decode
  -> route/scope validation（claim 前）
  -> SQLite current-session idempotency claim
  -> global context 或 request-unique repository worktree
  -> Gateway virtual process runtime / stock Codex MCPHost
  -> terminal response durable
  -> Slack delivery state
```

## Core ownership

- Gateway：协议、route、fingerprint、idempotency、delivery。
- Workspace Manager：一个粗 mutex；共享 bare mirror；每个 repository request 独立 detached exact tree。
- MCPHost：一个 permission generation；global thread 可复用，repository thread 每请求创建并在 terminal unsubscribe。
- Process Runtime：Codex/System 共用一个 registry、final invocation resolver、permanent policy 和 Token authorization mutex。
- Slack Runtime：Socket reconnect、当前会话 cursor、bounded in-memory transport index。
- Observability：有界 GUI/SQLite 日志；不作为 authoritative process/request state。

## 启动顺序

`NewApp` 只创建 Wails facade。Wails SingleInstanceLock 成功后 `OnStartup` 才构造 Service：

1. 打开严格 config v2，并在任何 authorization/runtime 创建前原子重置 permission mode 为 `safe`；
2. 打开 SQLite schema 3 并执行 `ResetRuntimeSession`；
3. 创建 observability、Gateway、Codex/MCPHost、process runtime；
4. 清理 stale ephemeral worktree；
5. 启动 Slack supervisor。

config/safe reset/state/Core 构造失败才 fatal。`full_access` 只在当前进程生命周期有效，Slack channel 不受 reset 影响。stale 单项失败仅 degraded；worktree root 完整性异常只阻止 repository readiness。

## Repository 与 Git

唯一 identity parser 接受 GitHub HTTPS owner/repo，输出 lowercase identity 和去 `.git` URL。该结果由 mirror key、fingerprint、Token binding 与日志复用。

目录固定为：

```text
CWapi-data/workspaces/git/
  mirrors/<identity-hash>.git
  worktrees/<request-id-hash>/
```

MinGit 子进程使用绝对 executable 与最小环境；实际 repository Git I/O 前可解析当前用户已有的外部 `gh auth git-credential` helper。系统不执行版本/登录预检，不维护 GitHub CLI 状态；helper 缺失或凭据失败由 Git 操作直接返回并记录。不修改 global Git config，不继承 GH/GitHub/CWapi/Slack/OpenAI secret 或 debug env。

## Global context

global stock MCP 的 CWD 固定为 `CWapi-data/temp/mcp-global`。safe 模式唯一 global 动态 writable root 就是该目录；不解析 repository 凭据、不创建 Git tree。

## Process architecture

`process_start/status/stop` 是 Gateway/Core virtual tools，不进入 MCPHost。start 在 Core 解析最终 executable、argv、real cwd，执行 permanent policy，再由 Codex safe backend 启动。结构化权限拒绝可签发一次性 Token，下一条新请求在相同 dirty tree 上用当前 Windows 用户的 System backend 执行。

每个 Codex process 使用独立 CODEX_HOME/client/sandbox root；不热改 shared MCPHost CODEX_HOME。所有 owned process tree 使用 Windows Job Object 收口。

## Desktop

Wails 只暴露轻量 snapshot/mutation binding。GUI 是固定 `375 × 690`、不可拉伸的单页窗口；窗口与标题栏由 WebView 绘制全不透明宇宙渐变，不启用 Windows backdrop、layered alpha 或额外 native 背景窗口。四个主卡片在应用内部叠加深色渐变、柔和虚化与高光边界，失焦不改变外观。无导航、project layer、GitHub CLI 检测、Diagnostics、Settings/About page 或可持久化日志偏好。

Desktop snapshot 读取 authoritative process registry，并分别最多公开最新一个 structured execution event 与 runtime error；routine runtime info 不进入 GUI。前端按真实时间戳在 process state/output、execution event、runtime error 和本地 mutation 之间选择一条 `key=value` 最新记录；不公开完整日志列表、不提供滚动历史，也不推测脚本进度。运行中展示 `[STOP]` 并调用 Service-owned stop binding；permission mode 与 Slack 配置也在同页通过 Service-owned mutation 完成。首次读取等待 Core 启动，避免把瞬时 `CORE_NOT_STARTED` 暴露为用户错误。
