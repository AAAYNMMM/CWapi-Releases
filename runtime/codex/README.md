# CWapi 管理的 stock Codex 运行时

本目录是 CWapi 随包管理的 Codex CLI 运行时入口。实际二进制、辅助程序和状态文件不得提交到 GitHub；发行包由 `config/codex-runtime.lock.json` 固定版本、来源和 SHA-256。

当前 v1.6.0 baseline：

- upstream ref：`rust-v0.144.4`
- source commit：`8c68d4c87dc54d38861f5114e920c3de2efa5876`
- packaged version：`0.144.4-cwapi.1`
- `codex.exe` SHA-256：`51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5`

`-cwapi.1` 是 CWapi 发行包版本标识，不表示 CWapi 维护自定义执行平台。对应源码树保持 stock stable baseline；CWapi 不依赖 patched/private Toolhost。

推荐布局：

```text
runtime/codex/
  releases/
    <version>/
      bin/codex.exe
      codex-resources/
      codex-path/
  current/
    bin/codex.exe
    codex-resources/
    codex-path/
```

运行时规则：

- CWapi 使用随包 `runtime/codex/current/bin/codex.exe`，不得依赖系统 `PATH` 中的裸 `codex`；
- CWapi 为 app-server 使用独立 `CODEX_HOME`，不读取或覆盖用户日常 `%USERPROFILE%\.codex`；
- CWapi 通过 stock `app-server --stdio` 与 `mcpServerStatus/list`、`mcpServer/resource/read`、`mcpServer/tool/call` 通信；
- MCP context 只创建 ephemeral `thread/start`，不启动 `turn/start`，不创建模型 turn；
- `safe` / `full_access` 只选择 CWapi 写入 CODEX_HOME 的 Codex-native permission profile；
- CWapi 不把 OpenAI/Codex 模型凭据注入 app-server 子进程；
- Node、Playwright MCP、browser 与 Git 属于同一 portable runtime 的其他受控组件，不由本目录替代。

可使用 `scripts/install_codex_runtime.ps1` 从固定发行包安装/切换 runtime。旧的 `*_codex_fork_*` 构建与安装流程不属于 v1.6.0 当前架构。
