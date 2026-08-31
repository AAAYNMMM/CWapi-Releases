# CWapi v1.6.3 安全边界

CWapi 面向个人单机使用，不实现 RBAC 或多租户。配置的 Slack channel 是远程信任边界；System Token 是该频道内的 60 秒 bearer，不绑定 Slack UserID/BotID/request_id。

## 永久策略

Go `executionpolicy` 是唯一规则定义，Core 对 Codex 与 System 都检查调用者直接指定的 final target、原 argv 与 real cwd；Codex base exec rules 从同一组定义生成。

永久规则包括：

- 禁止直接运行 format/diskpart/bcdedit/regedit/taskkill/Stop-Process；
- 禁止直接 Git history/worktree mutation；
- 禁止执行 CWapi dataRoot 内部文件；
- 禁止把 System/Program Files/Downloads/CWapi dataRoot 作为直接 path argument（当前 owned tree 除外）。

System matcher 不解析 nested shell、script、descendant 或 cmd/bat 脚本内部语义。这是明确的个人使用边界，不构建 shell parser。

## safe

- 每次 Service 启动都会在 authorization/runtime 创建前把 permission mode 原子重置为 `safe`；若 config 无法写回则拒绝启动，上次运行的 `full_access` 不会恢复；
- repository process 唯一 writable root 是当前 request tree；
- global MCP 唯一动态 writable root 是 `CWapi-data/temp/mcp-global`；
- 不共享 mutable tree；cwd 不能 traversal/reparse escape；
- child/descendant 继承同一 Codex Windows sandbox；
- Codex/System 都使用 canonical bounded environment。

## full_access 与 System Token

full_access 仍先运行 Codex。仅当 attempt 起始 mode 为 full、Codex 返回结构化权限拒绝、当前本地 mode 仍是 full、permanent policy 通过且容量未满时，Core 才签发 Token。

- 最多 3 个 active Token；
- 每个 60 秒、一次性；
- 每个绑定独立 dirty tree、repo、commit、final executable+argv+real cwd；
- consume 在 authorization mutex 内完成，是授权线性化点；
-切回 safe 会在 config 原子写成功后清空 active Token；写盘失败不改变 runtime mode、不清 Token；
- consume 已完成的 System launch 不因随后切 safe 回滚。

## Secret environment

Codex/System/Git credential helper 子进程只继承明确列出的 Windows/profile/temp/PATH 变量，固定 PATHEXT，并剔除 CWapi、Slack、Codex、OpenAI、GitHub credential 与 Git/gh debug 环境。

Slack App/Bot Token 只存 Windows Credential Manager。旧 credential target 名称为兼容已有本机凭据而保留，但不会进入 config、日志、public snapshot 或 package。

## Token redaction

raw Slack transport index 与当前会话 SQLite response 可以保存 Token 以支持同进程重投。Wails/GUI/acceptance evidence 仅对 MCP v2 顶层非空 `system_token` 字段替换为 `[REDACTED]`；不会误删无关 64hex。普通日志不记录 Token 值，错误消息不 echo Token。

## Reparse 与 cleanup

startup sweep 只遍历 known worktree root 的直接 children。symlink/reparse point 作为链接本身删除，绝不跟随到 root 外。mirror/worktree root 自身发生 reparse 或解析漂移会阻止新的 repository workspace；已有 global/status/Slack 仍可运行。

## 接受边界

CWapi 只能约束自己直接启动和拥有的进程树。用户在 System fallback 中执行的 shell/script 可以产生广泛副作用；一次性 Token、固定 Slack channel、permanent top-level policy 与 Windows 当前用户权限共同构成最终边界。
