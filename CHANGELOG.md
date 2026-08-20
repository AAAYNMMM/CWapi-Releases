# Changelog

## 1.6.0 — 2026-08-21

CWapi v1.6.0 是当前公开发行版本。发行仓库工作树已从旧 Python/Tauri 产品实现整体切换到 Go Core + Wails v2 + React 架构；旧版本内容只保留在 Git 历史中。

### Runtime architecture

```text
Web GPT
 -> Slack MCP envelope
 -> CWapi relay + exact-commit context
 -> stock Codex app-server
 -> configured MCP server
 -> MCP response / Slack File
```

- CWapi 不运行模型，也不启动 Codex Agent Turn；
- `project_id` discovery 与 40 位 `expected_commit` 绑定真实 detached workspace；
- packaged `cwapi` MCP server 提供透明 `command + argv` process lifecycle；
- executable 支持 PATH 名称、绝对路径、workspace 相对路径和 `.cmd/.bat` shim；
- structured logs、runtime logs、真实进程状态、托盘图标与 malformed-message 使用说明完整可用；
- 每次启动建立新 runtime session，不恢复或重放上一进程的任务与 Slack history。

### Public portable release

- 随包提供 pinned Codex、MinGit、Node、Playwright MCP 与 Chromium；
- 所有 runtime 路径从 `CWapi.exe` 所在目录动态解析；
- 可从不同盘符、含空格或非 ASCII 字符的目录启动；
- Go/Wails 构建启用 `-trimpath`，不嵌入构建机用户目录或源码绝对路径；
- staging 删除 `.log/.tmp/.dmp/.trace` 等运行残留；
- ZIP 不包含 `CWapi-data`、token、配置、数据库、任务、日志或浏览器 profile；
- release gate 从与安装目录无关的 working directory 启动真实 GUI 并验证数据只写入 executable 相邻的 `CWapi-data`。

### Web GPT path convention

MCP JSON 中的 Windows 路径统一使用 `/`，例如：

```text
C:/Projects/example/.venv/Scripts/python.exe
//server/share/tool.exe
```
