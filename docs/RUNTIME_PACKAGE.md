# CWapi v1.6.0 Runtime / Package

v1.6.0 面向 Windows 11 x64 提供自包含便携 ZIP。最终 artifact 不进入 Git，构建提交与 runtime pin 记录在 ZIP 内的 `portable-manifest.json`，SHA-256 在本地构建完成后生成并用于 GitHub Release。

## Portable layout

```text
<install root>/
├─ CWapi.exe
├─ portable-manifest.json
└─ runtime/
   ├─ codex/current/
   ├─ git/
   ├─ node/
   ├─ mcp/
   └─ browser/
```

安装根始终是运行中的 `CWapi.exe` 所在目录，不读取构建机路径，也不依赖 process working directory。用户关闭程序后可以移动整个目录。

首次运行创建：

```text
<install root>/CWapi-data/
```

config、SQLite state、日志、临时文件与 generated `CODEX_HOME` 都位于该目录；Slack token 位于当前 Windows 用户 Credential Manager。

## Pinned runtime

版本与下载 hash 以 `config/portable-runtime.lock.json` 和 `config/codex-runtime.lock.json` 为事实源：

- Codex `0.144.4-cwapi.1` package，stock source commit `8c68d4c87dc54d38861f5114e920c3de2efa5876`；
- Git for Windows MinGit `2.55.0.windows.4`；
- Node `24.18.0`；
- `@playwright/mcp` `0.0.79`；
- Chromium headless shell revision `1237`。

项目自己的 Python、JDK、SDK 或其他语言环境不进入 CWapi 固定 runtime。Web GPT 可以选择系统环境、项目虚拟环境或用户自行安装的 portable toolchain，并把准确 executable 与 argv 交给 command MCP。

## Runtime resolution

CWapi 的 production 路径均由 executable 目录派生：

- `runtime/codex/current/bin/codex.exe`；
- `runtime/git/cmd/git.exe`；
- `runtime/node/node.exe`；
- `runtime/mcp/playwright/.../cli.js`；
- `runtime/mcp/cwapi/process-server.cjs`；
- `runtime/browser/.../chrome-headless-shell.exe`；
- `CWapi-data/config/cwapi.json`。

generated Codex config 在需要 MCP context 时按当前安装根重写，不把发行仓库或首次解压目录当作永久路径。

## Assembly

`scripts/install_portable_runtime.ps1`：

1. 读取 lock；
2. 下载并验证各 archive SHA-256；
3. 安装 pinned Codex；
4. 安装 MinGit 与 Node；
5. 使用固定 package lock 安装 Playwright MCP；
6. 安装固定 Chromium headless shell；
7. 验证 executable hash、版本和目录结构。

默认下载缓存、Node validation runtime 与 Wails CLI 都位于当前仓库的 ignored `cache` 目录。所有 helper 也接受显式路径，不包含开发机固定目录。

## Staging

`automation/stage_v160_portable.ps1`：

- 要求 clean exact source commit；
- 使用 Wails `-trimpath` 构建 `CWapi.exe`；
- 验证 Go build metadata 中存在 `-trimpath=true`；
- 只复制当前 Codex runtime 和固定的 Git、Node、MCP、browser；
- 删除 `.log/.tmp/.dmp/.trace` runtime 残留；
- 拒绝 `CWapi-data`、数据库、token、browser profile 等私有 artifact；
- manifest 只记录版本、hash、relative path policy 和 exact source commit；
- 创建 `build/stage/CWapi-v1.6.0.zip`。

## Privacy boundary

正式 ZIP 不得包含：

- Slack/Codex/GitHub token 或私钥；
- 用户 config、state、task、result、log；
- project path、project ID 或 channel ID；
- browser profile、Cookies、History、Login Data；
- 构建机用户名、用户目录或源码绝对路径；
- runtime 调试日志或验证临时文件。

Git 自带的 `cert.pem` 是公共 CA bundle，不属于用户证书或私钥。

## Relocation gate

`automation/validate_v160_portable_release.ps1` 对最终 ZIP 执行：

1. archive 私有文件名扫描；
2. manifest/source/runtime pin 验证；
3. `CWapi.exe` trimpath 与构建身份扫描；
4. 把 ZIP 解压到不同盘符、含空格与中文的临时目录；
5. 从无关 working directory 启动真实 Wails/React GUI；
6. 确认 `CWapi-data` 只出现在 executable 相邻目录；
7. 从 relocated root 运行 packaged Codex、Git 与 Node；
8. 清理全部测试进程和临时数据。

通过该 gate 后，ZIP hash 才能进入本地交付报告和后续 GitHub Release。
