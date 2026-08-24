# CWapi v1.6.1 Runtime / Package

portable 目录：

```text
CWapi-v1.6.1/
  CWapi.exe
  portable-manifest.json
  runtime/
    codex/current/bin/codex.exe
    git/
    node/
    mcp/playwright/
    browser/
```

## 用户交付约定

- ZIP 可解压到任意用户可写路径，包括不同盘符、带空格或中文的目录；直接运行 `CWapi.exe`，没有固定安装路径。
- portable 自带 Codex、MinGit、Node、Playwright MCP 与 browser runtime；用户不需要安装 Go、Node、Git、Wails，也不需要配置项目环境变量。
- 用户侧只需提供 Slack App/Bot Token 与 channel ID，并安装 GitHub CLI；private repository 使用当前 Windows 用户已有的 `gh` 凭据。
- 首次启动在程序旁创建 `CWapi-data`。整体移动解压目录后，应用使用新位置自己的数据目录与 bundled runtime。
- 正式 ZIP 只包含程序、manifest 与 bundled runtime；构建使用 trimpath，并在上传前扫描用户数据、凭据/Token、日志、数据库、仓库、浏览器 profile、用户名、机器名与构建机绝对路径。

明确不打包：

- `CWapi-data`、用户 config/state/log/temp/worktree；
- gh executable/config/token；
- Git global config；
- Slack/GitHub/OpenAI/Codex credential、active secret 与 private key；
- 日志、数据库、repository/worktree/mirror、browser profile、构建用户名/机器名/绝对路径；
- 旧 `runtime/mcp/cwapi` Node process server；
- 源码 cache/build 目录。

## 固定组件

- Codex `0.144.4-cwapi.1`，commit `8c68d4c87dc54d38861f5114e920c3de2efa5876`，SHA-256 `51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5`。
- MinGit、Node、Playwright MCP、Chromium revision 来自 `config/portable-runtime.lock.json`。
- 发行包的 Node runtime 只保留锁定的 `node.exe` 与 Node `LICENSE`；npm/corepack/docs 不进入 package。Node 仅服务 Playwright MCP 与运行时探针。

## 构建

以下步骤只面向维护者生成新的正式 portable，不是用户安装步骤。

```powershell
.\automation\stage_v161_portable.ps1 `
  -ExpectedCommit <40hex> `
  -RuntimeSourceRoot <absolute-runtime-root>
```

脚本要求 clean exact commit，使用固定 Node/Wails，执行 Wails windows/amd64 build，把 source commit 写入 ldflags，验证 runtime lock/hash/version，生成 manifest 与 `build/stage/CWapi-v1.6.1.zip`。

## Package gate

```powershell
.\automation\validate_v161_packaged_gui_start.ps1 -ExpectedCommit <40hex>
.\automation\validate_v161_portable_privacy.ps1 `
  -ExpectedCommit <40hex> `
  -PortableRoot .\build\stage\CWapi-v1.6.1 `
  -ZipPath .\build\stage\CWapi-v1.6.1.zip
```

gate 覆盖 first-run config、embedded React mount、v1.6.1 UI marker、进程存活、数据清理和 package privacy。明确要求新的正式 portable 时，还要验证任意可写路径 relocation、无旧 MCP package、manifest/hash 一致，以及真实 Slack。

这些 gate 只约束被明确发起的新正式交付，不会因文档或开发者源码检查自动重开当前已完成任务。执行后 source worktree 必须仍 clean 且 HEAD 不变。未经明确要求不创建 tag 或 GitHub Release。
