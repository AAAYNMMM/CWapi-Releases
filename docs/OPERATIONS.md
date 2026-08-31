# CWapi v1.6.3 运行与维护

## 首次启动

1. 将 portable ZIP 解压到任意用户可写路径；盘符、目录名、空格和中文路径不影响使用。
2. 安装 GitHub CLI；private repository 使用当前 Windows 用户已有的 `gh` 凭据。
3. 直接启动 `CWapi.exe`，程序在旁边生成 `CWapi-data/config/cwapi.json`。
4. 在 GUI 单页底部连接 Slack；channel 写入 config，App/Bot Token 写入 Windows Credential Manager。

除此之外，用户不需要安装 Go、Node、Git、Wails，不需要设置 PATH、项目环境变量或固定解压目录。Codex、MinGit、Node、Playwright MCP 与 browser runtime 均由 portable 提供。

CWapi 不做 GitHub CLI 状态检测。repository request 会直接执行非交互 Git；private repository 可使用当前 Windows 用户已有的外部 `gh auth git-credential`，凭据不可用时错误会进入 GUI 的最新记录。

## 数据目录

```text
CWapi-data/
  config/cwapi.json
  state/cwapi.db
  logs/
  temp/mcp-global/
  temp/codex-executions/
  workspaces/git/mirrors/
  workspaces/git/worktrees/
```

portable ZIP 不包含 `CWapi-data`。移动整个解压目录后，应用在新位置使用自己的数据目录和 bundled runtime。

## 启动恢复

- 仅第一个 Wails instance 构造 Service。
- permission mode 在 Core/runtime 创建前原子重置为 `safe`；上次运行的 `full_access` 不会恢复，Slack channel 保留。
- 每次启动清空上一进程运行会话。
- stale worktree 逐项安全清理；mirror 保留并 prune。
- root integrity 异常标记 workspace degraded/blocked，不跟随链接删除外部内容。
- Slack 连接失败可恢复，不导致 Core fatal。

## Shutdown

应用关闭顺序：Slack supervisor、Authorization/registry、Codex MCPHost、SQLite。registry 通过 Job Object 停止 owned descendants；终止 record 不跨 restart 保存。

## 故障定位

- `CONFIG_*`：检查 config 是否精确 v2/1.6.1 shape。
- `WORKSPACE_MIRROR_CLONE_FAILED` / `WORKSPACE_FETCH_FAILED`：查看 GUI 最新记录或内部 runtime log 中的 Git 错误；private repository 同时确认当前 Windows 用户的凭据可供可选 helper 使用。
- `WORKSPACE_ROOT_BLOCKED`：检查 `CWapi-data/workspaces/git/worktrees` 是否被替换为 reparse/symlink。
- `CODEX_EXECUTABLE_SHA256_MISMATCH`：重新安装固定 portable runtime。
- `SYSTEM_TOKEN_*`：使用新 request_id，并在 60 秒内保持 repo/commit/最终调用不变。

不要手工修改运行中的 SQLite、Token registry 或 process tree。需要清理时先退出 CWapi，再处理明确的数据子目录；mirror 可保留以减少下次 fetch。
