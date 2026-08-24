# CWapi v1.6.1

CWapi 是个人使用的 Windows 桌面网关：通过一个固定 Slack 频道接收严格的 MCP v2 请求，准备 GitHub exact-commit 工作区，并把 stock Codex MCP 与受控进程能力返回给调用方。

项目原则只有：简单、稳定、高效。

## 当前架构

```text
Slack [CWapi/MCP/2]
  -> Go Gateway（v2 校验、route、幂等）
  -> Gateway virtual process tools 或 stock Codex MCP
  -> request-unique exact-commit worktree
  -> Codex safe backend
  -> 必要时 60 秒一次性 System Token fallback
  -> Slack [CWapi/MCP/2] response
```

- 桌面：Wails v2 + React/TypeScript。
- 核心：Go；SQLite schema 3 保存当前运行会话状态。
- Git：portable 内置 MinGit；外部 `gh` 只提供 GitHub credential helper。
- Codex：固定 `0.144.4-cwapi.1` 与 SHA-256；进程执行不启动模型 turn。
- Node：仅为 Playwright MCP 与运行时探针保留，不再有 Node CWapi process server。

## 便携交付

当前 v1.6.1 任务已完成，交付物是 Windows portable ZIP。

- 解压到任意用户可写路径后直接运行 `CWapi.exe`；程序不依赖固定盘符、目录名或安装位置。
- portable 自带 Codex、MinGit、Node、Playwright MCP 与 browser runtime；用户无需安装 Go、Node、Git、Wails，也无需设置项目环境变量。
- 用户只需提供 Slack App/Bot Token 与 channel ID，并安装 GitHub CLI；private repository 使用当前 Windows 用户已有的 `gh` 凭据。
- 首次运行在程序旁创建 `CWapi-data`；整体移动解压目录后，程序使用新位置自己的数据与 runtime。
- 正式 ZIP 不包含用户配置、凭据、Token、数据库、日志、仓库、浏览器 profile 或构建机身份/绝对路径，并在发布前执行独立隐私扫描。

## 配置

首次启动在 `CWapi-data/config/cwapi.json` 创建唯一合法结构：

```json
{
  "schema": "cwapi.config.v2",
  "version": "1.6.1",
  "permission_mode": "safe",
  "slack": { "channel_id": "" }
}
```

只允许 `safe` 或 `full_access`。每次 CWapi 启动都会在 Core 初始化前把模式原子重置为 `safe`；`full_access` 只在本次运行中临时有效，不跨重启保留。Slack App/Bot Token 仍只保存在当前 Windows 用户的 Credential Manager；repository、GUI 偏好和 secret 不进入配置。旧 schema/version 会直接拒绝，不迁移。

## 执行语义

- `process_start` 必须携带 ASCII GitHub HTTPS `repository_url` 与完整 40 位 `expected_commit`。
- 每个新 repository request 创建独立 ephemeral worktree；mirror 按小写 `owner/repo` 共享。
- `process_status`、`process_stop` 和 status-list 使用 global scope。
- safe backend 只写当前 request tree；global MCP 只写 `CWapi-data/temp/mcp-global`。
- full_access 仍先尝试 Codex。只有真实结构化权限拒绝才签发最多 3 个、60 秒、一次性的 System Token。
- Codex/System 共用一个 Go process registry：8 active、48 terminal、700ms start 观察、每路 8192-byte tail。

完整 wire contract 见 [docs/PROTOCOL.md](docs/PROTOCOL.md)，安全边界见 [docs/SECURITY.md](docs/SECURITY.md)。

## 本地开发

```powershell
go test ./...
cd frontend
npm ci
npm test
npm run build
```

最终 Windows 门禁使用固定 runtime：

```powershell
.\automation\validate_v161_source.ps1 -ExpectedCommit <40hex>
.\automation\validate_v161_codex_runtime.ps1 -ExpectedCommit <40hex> `
  -CodexExecutable <absolute-codex.exe> -NodeExecutable <absolute-node.exe>
.\automation\stage_v161_portable.ps1 -ExpectedCommit <40hex> -RuntimeSourceRoot <portable-root>
.\automation\validate_v161_packaged_gui_start.ps1 -ExpectedCommit <40hex>
```

真实 Slack gate 使用 `prepare_v161_real_slack_validation.ps1` 和 `validate_v161_real_slack_mcp.ps1`。这些是维护者在明确要求生成新的正式 portable/Release 时使用的回归门禁，必须针对同一 clean commit；它们不是用户安装步骤，也不会因文档检查自动重开已完成的 v1.6.1 任务。未经明确要求不创建 tag 或 Release。

## 文档入口

- [架构](docs/ARCHITECTURE.md)
- [协议](docs/PROTOCOL.md)
- [安全](docs/SECURITY.md)
- [运行包](docs/RUNTIME_PACKAGE.md)
- [验收](docs/ACCEPTANCE.md)
- [开发进度](docs/development/progress/README.md)
