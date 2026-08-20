# CWapi v1.6.0

CWapi 是面向个人 Windows 开发环境的 Slack MCP 桥接程序。Web GPT 通过 Slack 发送结构化 MCP 请求，CWapi 在用户配置的项目与 exact commit 上下文中调用本机 Codex CLI，再把结果或文件返回 Slack。

本仓库是公开发行仓库，当前工作树只保留 v1.6.0 的代码、文档与可复现构建配置。旧版本源码仅保留在 Git 历史中。

## 使用便携包

1. 从 [GitHub Releases](https://github.com/AAAYNMMM/CWapi-Releases/releases) 下载 `CWapi-v1.6.0.zip`。
2. 把整个 ZIP 解压到任意可写目录，例如 `D:/Tools/CWapi` 或 `C:/Users/name/Apps/CWapi`。
3. 运行目录内的 `CWapi.exe`。
4. 在首次启动页面配置 Slack，并添加允许 CWapi 使用的项目。

不要只复制 `CWapi.exe`；`runtime` 目录必须与它一起移动。关闭 CWapi 后可以移动整个目录，下一次启动会从新的 executable 目录重新解析所有随包 runtime。

Windows 11 x64 不需要另外安装 Codex、Git、Node、Playwright MCP 或 Chromium。目标项目自己的 Python、Java、JDK、SDK 等环境不由 CWapi 固定管理，可由用户或 Web GPT 选择并通过结构化 `command + argv` 使用。

## 可移植目录

```text
<任意安装目录>/
├─ CWapi.exe
├─ portable-manifest.json
└─ runtime/
   ├─ codex/
   ├─ git/
   ├─ node/
   ├─ mcp/
   └─ browser/
```

CWapi 不依赖当前工作目录；`CWapi.exe` 不保存本机构建用户名、发行仓库路径或用户目录。运行时路径全部从 executable 所在目录解析。

首次运行后，用户数据只写入：

```text
<安装目录>/CWapi-data/
```

Slack App Token 与 Bot Token 写入当前 Windows 用户的 Credential Manager，不写入普通配置、日志或发行 ZIP。发布包本身不包含用户配置、数据库、任务历史、日志、浏览器资料或凭据。

## 工作链路

```text
Web GPT
  ├─ GitHub：查看、修改并提交源码
  └─ Slack：发送 MCP request
                │
                ▼
              CWapi
       Slack relay + exact commit
                │
                ▼
       stock Codex app-server
                │
                ▼
       configured MCP servers
                │
                ▼
       response / Slack File
```

CWapi 不运行模型，不启动 Codex Agent Turn，也不重新实现 Git、Build、Test 或 File 工具平台。正常 relay 只允许 stock app-server 的三个 MCP 方法：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

随包 `cwapi` MCP server 提供透明的 `process_start`、`process_status`、`process_stop`。`command` 支持 PATH 名称、绝对 executable 路径以及 exact-commit workspace 相对路径，包括 `.venv/Scripts/python.exe`、特定 JDK/SDK、portable toolchain 和 Windows `.cmd/.bat` shim。

## Web GPT 路径约定

Windows 路径进入 MCP JSON 前统一把 `\` 转为 `/`：

```text
C:/Projects/example/.venv/Scripts/python.exe
//server/share/tool.exe
```

`C:\\...` 只作为兼容输入，不是正式工作流格式。

## 权限边界

CWapi 提供默认 `safe` 和用户显式选择的 `full_access` 两种 Codex permission profile，并保留 secret 隔离、exact commit、幂等、owned process 和基础危险命令保护。

需要特别注意：packaged command MCP 以当前 Windows 用户权限运行，不自动继承 Codex thread 的 filesystem/execpolicy sandbox。自由 executable 能力应只用于用户明确配置的项目，命令参数不得携带 secret。

## 从源码构建

要求 Windows 11 x64、Go 与 PowerShell。Node、Wails 和发行 runtime 由固定 lock 与脚本准备到仓库相对的 ignored 目录，不依赖任何开发机固定路径。

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install_portable_runtime.ps1

$commit = (git rev-parse HEAD).Trim()
powershell -NoProfile -ExecutionPolicy Bypass -File automation/stage_v160_portable.ps1 `
  -ExpectedCommit $commit `
  -RuntimeSourceRoot .

powershell -NoProfile -ExecutionPolicy Bypass -File automation/validate_v160_portable_release.ps1 `
  -ExpectedCommit $commit
```

发行构建启用 Wails/Go `-trimpath`，staging 会移除 runtime 临时日志，并拒绝用户数据、数据库、浏览器 profile 和凭据类文件。随包第三方预编译 runtime 可能保留其公开 CI 构建 provenance，但不得包含本次打包用户的身份、路径或 secret。最终 gate 会把 ZIP 解压到不同盘符、含空格和中文的临时目录，从无关工作目录启动真实 GUI，并验证 runtime 版本与数据落点。

## 文档

- `docs/ARCHITECTURE.md`
- `docs/PROTOCOL.md`
- `docs/CHATGPT_WORKFLOW.md`
- `docs/CODEX_TOOLHOST.md`
- `docs/RUNTIME_PACKAGE.md`
- `docs/SECURITY.md`
- `docs/OPERATIONS.md`
- `docs/LOCAL_VALIDATION.md`
- `docs/RELEASE_CHECKLIST.md`

版本变化见 `CHANGELOG.md`。
