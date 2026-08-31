# CWapi v1.6.3 Codex app-server / MCPHost

CWapi 使用固定 stock Codex app-server，不创建模型 turn。

## Stock MCP relay

允许的 app-server method 只有 status-list、resource-read、tool-call。CWapi 创建 ephemeral context thread 并注入 owner `threadId`；caller 提供 threadId 会拒绝。

- global：固定 CWD `CWapi-data/temp/mcp-global`，可在 permission generation 内复用 thread；
- repository：每个 request 使用独立 exact worktree 与独立 thread，terminal 时 unsubscribe；
- tool transport/process failure 不自动 replay。

`server=cwapi` 的 process tools 在 Gateway/Core 截获，不进入 MCPHost，不出现在 status-list。

## Command backend

每个 `process_start` 创建独立 `CWapi-data/temp/codex-executions/<process_id>` CODEX_HOME 与 app-server client。Core 通过 model-free `command/exec` 传入：

- resolved absolute executable + final argv；
- real cwd；
- canonical child env；
- `workspaceWrite` sandbox，只含当前 repository root；
- network enabled；host temp excluded；timeout disabled，由 process registry 管 lifecycle。

完成/停止后 Job Object、client 与 execution home 由唯一 owner 清理。

## Permission profiles

shared MCPHost config 只生成两个 profile：

- `cwapi-safe`：`:workspace` + global MCP root；
- `cwapi-full-access`：root write，但永久 protected roots deny。

永久 exec rule 由 Go `executionpolicy` 生成。process backend 不修改 shared CODEX_HOME 或 profile；每次 attempt 在开始时固定 permission mode。

## Runtime integrity

每次启动 Codex client 前验证 pinned executable SHA-256。能力门必须证明 Windows sandbox readiness、native/batch fidelity、child isolation、structured denial、long process Job lifecycle 与 secret env 隔离；任一关键能力缺失即阻止发布。
