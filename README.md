# CWapi v1.6.0

CWapi 是面向个人 Windows 开发环境的 **Web GPT → Slack → 本机开发工具** 桥接程序。Web GPT 负责理解需求、读取和修改 GitHub 项目、选择测试方式与目标项目环境；CWapi 负责把结构化 MCP 请求绑定到用户配置的项目与 **exact commit**，调用本机 stock Codex app-server / MCP server，再把结果、日志或文件返回 Slack。

```text
Web GPT 描述 / 修改 GitHub 项目
        ↓ exact commit
Slack MCP request
        ↓
CWapi：project + exact-commit + state + delivery
        ↓
stock Codex app-server → configured MCP server
        ↓
result / Slack File → Web GPT
```

CWapi **不运行模型，不启动 Codex Agent Turn 来替 Web GPT 思考**，也不重新实现第二套 Git / Build / Test / File 平台。使用中有任何问题，可加入“小黑盒群”，链接在帖子置顶评论。

本仓库是公开发行仓库；当前工作树只保留 v1.6.0 代码、文档与可复现构建配置，旧版本源码保留在 Git 历史中。

## v1.6.0 能力

- Slack 接收 Web GPT MCP request，返回 MCP response / Slack File。
- `projects/list` 与 `mcpServerStatus/list` discovery 当前项目、`project_id`、source commit 和 MCP catalog。
- 项目调用绑定 `project_id + expected_commit`，准备 detached exact-commit worktree。
- 正常 relay 使用 stock app-server：`mcpServerStatus/list`、`mcpServer/resource/read`、`mcpServer/tool/call`。
- 随包 `cwapi` MCP server 提供 `process_start`、`process_status`、`process_stop`。
- `command` 支持 PATH 名称、绝对 executable 路径、working-directory-relative 路径和 Windows `.cmd/.bat`。
- Web GPT / 用户自己发现、安装、选择和管理目标项目的 Python、JDK、Go、Rust、SDK 等环境。
- Playwright 可用于 localhost 页面访问、交互、DOM 验证和截图。
- 长文本、日志、图片、resource text/blob 可按 outbound policy 交付 Slack File。
- 默认 `safe`、显式 `full_access` Codex permission profile，并保留 secret、幂等、owned process 等边界。

## 快速开始

1. 从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载 `CWapi-v1.6.0.zip`。
2. 完整解压到可写目录，例如 `D:/Tools/CWapi`；不要只复制 `CWapi.exe`，也不要在 ZIP 内运行。
3. 运行 `CWapi.exe`，在首次启动或设置页配置 Slack，并在“项目”页添加允许 CWapi 使用的项目。
4. 默认保持 `safe`；只有明确需要扩大 Codex-managed filesystem 权限时再启用 `full_access`。
5. 在 ChatGPT 中连接 GitHub 和 Slack。
6. 第一次告诉 Web GPT：

> 连接 GitHub，读取 `AAAYNMMM/CWapi-Releases` 仓库中的 `docs/WEB_GPT_ENTRY.md`，了解 CWapi v1.6.0 当前工作流。

然后直接下任务，例如：

> 使用 CWapi 工作流开发 GitHub 仓库 `username/my-project`，检查当前问题，修改后在对应 exact commit 上完成本地测试。

完整逐步教程见 [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)。

## Slack 配置

当前 transport 使用：`App Token (xapp-...)`、`Bot Token (xoxb-...)`、`Channel ID (C...)`。Token 写入当前 Windows 用户的 Credential Manager，不写普通配置、Git、MCP body、artifact 或发行 ZIP。更完整的 transport / recovery / file upload 说明见 [`docs/SLACK_TRANSPORT.md`](docs/SLACK_TRANSPORT.md)。

## 项目与 exact commit

项目记录包含 display name、GitHub repository identity、本地路径和 remote URL。CWapi 为项目生成 `prj-...` ID；Web GPT 不应猜 ID，而应通过 `projects/list` 或 status discovery 获取。

项目 request 使用完整 40 位 `expected_commit`。CWapi 内部执行：

```text
project lookup → Git mirror fetch → verify commit
→ detached worktree → thread/start(cwd + permissions)
→ MCP call → delivery → release worktree
```

因此 GitHub 上准备验证的版本和本机真正执行的版本可以明确对应。

## 环境由 Web GPT / 用户管理

发行包自身带 CWapi 工作所需的 Codex、Git、Node、Playwright MCP、Chromium 等 runtime；目标项目自己的环境不由 CWapi 固定管理。Web GPT 应先检查本机已有环境，确认项目要求，缺失时选择合适安装方式和位置，再把准确 `command + argv` 交给 CWapi。

常见 executable：

```text
C:/Users/name/AppData/Local/Programs/Python/Python312/python.exe
C:/Program Files/Git/cmd/git.exe
C:/Program Files/Java/jdk-25/bin/java.exe
.venv/Scripts/python.exe
node_modules/.bin/tool.cmd
```

正式 MCP JSON 中 Windows 路径统一把 `\` 转成 `/`，不要给 `command` 值再套一层引号。

exact-commit worktree 是临时执行上下文；不要假定请求 A 在其中创建的 `.venv` 一定能在请求 B 继续存在。需要跨请求长期复用的环境更适合放在明确持久位置并通过绝对 executable 调用。详细规则见 [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)。

## 进程、浏览器和附件

`cwapi` process server 提供：

```text
process_start → running/completed + process_id
process_status(process_id)
process_stop(process_id)
```

典型 Web E2E：启动 localhost 服务 → Playwright navigate → fill/click → `browser_evaluate` 验证真实业务结果 → screenshot → `process_stop`。

CWapi 只对 **MCP 已返回的内容**执行外发：短文本 inline；长文本/日志、图片和 resource content 可转 Slack File。当前单 artifact 最大 8 MiB、单 response 最多 16 个 artifact。CWapi 不会因为 result 里出现 `C:/...` 或 `file://...` 就自行读取那个本地路径。

## 权限边界

Codex-managed execution 使用 `safe -> cwapi-safe` 或 `full_access -> cwapi-full-access`。但 packaged `cwapi` command/process MCP 启动的自由 executable 以当前 Windows 用户权限运行，**不自动继承 Codex thread 的 filesystem / execpolicy sandbox**。

所以不要把 `safe` 理解成“任意本机子进程都只能访问项目目录”。自由 command 能力只用于用户明确配置的项目，且不得把 token、password、private key 等 secret 放入 `command` / `argv`。完整边界见 [`docs/SECURITY.md`](docs/SECURITY.md)。

## 便携目录与迁移

```text
<安装目录>/
├─ CWapi.exe
├─ portable-manifest.json
├─ runtime/{codex,git,node,mcp,browser}/
└─ CWapi-data/     # 首次运行后生成
```

CWapi 不依赖启动时的 current working directory。关闭 CWapi 后可以移动整个安装目录，下一次启动从新的 executable location 重新解析随包 runtime；不要只移动 `CWapi.exe`。

每次启动建立新的运行会话：不自动拾取启动前 Slack history，也不恢复上一进程未完成 request；同一进程内 duplicate request 不重复执行，Socket reconnect 从 durable cursor 后继续。详细维护见 [`docs/OPERATIONS.md`](docs/OPERATIONS.md)。

## 文档导航

### 第一次使用
- [`docs/USER_GUIDE.md`](docs/USER_GUIDE.md)：下载安装、Slack、项目、权限、第一次完整开发。
- [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md)：常见错误与处理方向。
- [`docs/GUI.md`](docs/GUI.md)：控制台、项目、设置、诊断、关于。

### Web GPT
- [`docs/WEB_GPT_ENTRY.md`](docs/WEB_GPT_ENTRY.md)：Web GPT 唯一必读快速入口。
- [`docs/CHATGPT_WORKFLOW.md`](docs/CHATGPT_WORKFLOW.md)：完整 workflow、环境、进程、等待和验收规则。

### 使用、安全与协议
- [`docs/OPERATIONS.md`](docs/OPERATIONS.md)：运行、迁移、恢复、日志、进程。
- [`docs/SECURITY.md`](docs/SECURITY.md)：Codex profile、trusted command boundary、secret。
- [`docs/PROTOCOL.md`](docs/PROTOCOL.md)：Slack frame、discovery、request / response。
- [`docs/SLACK_TRANSPORT.md`](docs/SLACK_TRANSPORT.md)：Slack transport 与文件外发。

### 架构与发行
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)、[`docs/CODEX_TOOLHOST.md`](docs/CODEX_TOOLHOST.md)、[`docs/RUNTIME_PACKAGE.md`](docs/RUNTIME_PACKAGE.md)、[`docs/RUNTIME_LOGGING.md`](docs/RUNTIME_LOGGING.md)、[`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)、[`docs/LOCAL_VALIDATION.md`](docs/LOCAL_VALIDATION.md)、[`docs/ACCEPTANCE.md`](docs/ACCEPTANCE.md)、[`docs/RELEASE_CHECKLIST.md`](docs/RELEASE_CHECKLIST.md)。版本变化见 [`CHANGELOG.md`](CHANGELOG.md)。

## 从源码构建

要求 Windows 11 x64、Go 与 PowerShell。Node、Wails 和发行 runtime 由固定 lock 与脚本准备到仓库相对 ignored 目录：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_portable_runtime.ps1
$commit = (git rev-parse HEAD).Trim()
powershell -NoProfile -ExecutionPolicy Bypass -File automation/stage_v160_portable.ps1 -ExpectedCommit $commit -RuntimeSourceRoot .
powershell -NoProfile -ExecutionPolicy Bypass -File automation/validate_v160_portable_release.ps1 -ExpectedCommit $commit
```

发行构建启用 Wails/Go `-trimpath`；staging 会拒绝用户数据、数据库、浏览器 profile 和凭据类文件，并验证 portable runtime 与数据落点。