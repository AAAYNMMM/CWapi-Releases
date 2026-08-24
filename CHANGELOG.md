# Changelog

## 1.6.1 — 2026-08-25

- 协议整体升级为 `[CWapi/MCP/2]` / `cwapi-mcp/2` / v2 schema；route、scope、Token 位置与 virtual tool shape 在幂等 claim 前严格校验。
- config 固定为 `cwapi.config.v2/1.6.1`，只保留 permission mode 与 Slack channel；删除 project 配置、validator、GUI CRUD 与日志偏好。
- repository identity 统一为 GitHub HTTPS owner/repo；共享 mirror、每请求独立 exact-commit worktree、repository MCP thread terminal unsubscribe。
- 删除 Node CWapi process MCP；`process_start/status/stop` 改为 Gateway/Core virtual tools。
- Codex/System 共用一个 Go process registry，提供 700ms start、8 active、48 terminal、8KiB tails、Job Object cleanup 与最小 public record。
- registry 的 terminal `done` 信号在 owned cleanup 完成后关闭，保证 stop/shutdown 返回时资源已经清理。
- full_access 改为 Codex-first；结构化权限拒绝最多签发 3 个、60 秒、一次性、final-invocation-bound System Token，并保留原 dirty tree 完成 fallback。
- permanent policy 与 canonical child env 由 Codex/System/Git credential helper 共用；补齐 PATH/batch wrapper/reparse/secret isolation 对抗测试。
- Service 构造移动到 Wails SingleInstanceLock 后；startup reset/sweep/Slack 顺序收口。
- 每次 Service 启动在 runtime 创建前原子重置 permission mode 为 `safe`；运行中的 `full_access` 不跨重启保留，写盘失败则启动失败。
- 删除 GitHub CLI 版本/登录检测、状态/刷新 API 与 readiness 阻断；private repository 仍可在实际 Git I/O 时使用当前用户的可选 credential helper。
- GUI 重构为固定 `375 × 690`、不可拉伸的单页窗口；permission、Slack 与真实 process registry 状态集中展示，active process 可用 `[STOP]` 停止。
- 窗口与标题栏改为全不透明宇宙渐变背景，不使用 Windows backdrop、layered alpha 或额外 native 背景窗口；四个主卡片使用应用内深色渐变、柔和虚化与高光边界，最新记录 viewport 无边缘阴影或投影。
- 删除完整 runtime/structured 日志列表、Diagnostics、Settings/About 与活动错误聚合；只显示真实时间戳最新的一条数据化 process/event/runtime-error 记录，routine runtime info 不进入 GUI；public Slack snapshot 对 v2 顶层 Token 做字段级脱敏。
- portable 保留 MinGit、Node/Playwright/Chromium，移除旧 CWapi MCP，新增 v1.6.1 source、Codex、GUI、package 与真实 Slack gates。
- 发行 Node runtime 精简为固定 `node.exe` 与 `LICENSE`，不携带 npm/corepack/docs；Codex 归档中的空 Git 元数据也在 staging 时移除。
- 正式构建使用 trimpath，并增加 stage/ZIP privacy gate；发行包拒绝用户配置、凭据/Token、日志、数据库、仓库、browser profile 与构建机身份/绝对路径。

本版本以可直接解压运行的 Windows portable ZIP 发布；用户只需提供 Slack 凭据/频道并安装 GitHub CLI。

## 1.6.0 — 2026-08-21

CWapi v1.6.0 的实现与用户验收已经完成。最终运行时验收候选为：

```text
source commit: 2bcb58d188205f4b79034a82149985c28e2f4a0c
portable SHA-256: 991d2a05bff36b0f1b4a4a5d692140cb0f9c9aefe409f1f029808c50b6558e5e
stock Codex commit: 8c68d4c87dc54d38861f5114e920c3de2efa5876
stock Codex SHA-256: 51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5
```

本次只合并并上传源码；未创建 Git tag 或 GitHub Release。

### Current architecture

```text
Web GPT
 -> Slack MCP envelope
 -> CWapi relay + exact-commit context
 -> stock Codex app-server
 -> configured MCP server
 -> MCP response / Slack File
```

- CWapi 不运行模型，不启动 Codex Agent Turn；
- remote relay 只允许 stock MCP status/resource/tool 方法；
- configured project 通过稳定 `project_id` discovery，并绑定 40 位 `expected_commit`；
- detached exact-commit workspace 由 CWapi 准备和释放；
- packaged `cwapi` MCP server 提供透明 `command + argv` process lifecycle；
- executable 支持 PATH 名称、Windows 绝对路径和 workspace 相对路径；
- CWapi 不识别语言、选择版本或管理安装环境；
- structured execution、runtime log、真实状态、tray icon 和错误格式使用说明已完成；
- 每次 CWapi 启动建立新会话，不显示或重放上一进程任务；
- portable 包不包含用户 state、secret、日志或 browser profile。

### Web GPT path convention

进入 MCP JSON 的 Windows 路径统一把反斜杠转换为正斜杠，例如：

```text
C:/Users/name/project/.venv/Scripts/python.exe
//server/share/tool.exe
```

`C:\\...` 只属于实现兼容能力，不再是 Web GPT 正式工作流格式。

### Acceptance

- 用户确认真实 Web GPT -> Slack -> CWapi 工作流完整通过；
- exact-commit project discovery、process、Playwright、Slack File 和 duplicate 行为通过；
- malformed Slack MCP message 会返回 v1.6.0 使用说明；
- absolute executable、Python 项目启动和 fresh-session 行为通过；
- Go、frontend、Wails、stock relay、permission、portable 和文档门禁通过；
- source tree 在验收前后保持 clean。

## Earlier versions

v1.5.1 及更早版本、Stage 1/Stage 2 的中间路线和已退役 Private Toolhost/custom Tool 设计保留在 Git 历史与对应旧 tag 中，不再作为当前产品事实。
