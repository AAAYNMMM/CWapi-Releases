# CWapi v1.6.3 GUI

GUI 是本地状态观察与少量 owner mutation 面板，不承载 protocol truth。

## 单页窗口

- Wails 窗口固定为 `375 × 690`，不可拉伸。窗口与标题栏使用全不透明宇宙渐变背景；不启用 Windows backdrop、layered alpha 或额外 native 背景窗口，因此前台与失焦外观一致。
- 全部功能位于一个页面，不保留导航、Console、Settings、Diagnostics 或 About 路由。
- 顶部只显示 Core、Slack、Codex 状态；主体显示一个当前或最近进程；底部集成 permission mode 与 Slack。执行、最新记录、权限和 Slack 四个主卡片使用应用内深色渐变、柔和虚化与高光边界；最新记录不使用投影。
- Slack 首次配置使用同一窗口内的 sheet，不创建第二个页面。

GitHub CLI 检测/刷新、project 列表与 CRUD、本地路径编辑、活动错误聚合和 GUI 日志偏好均已删除。

## 进程监视

进程区域只读取 Go-owned process registry。运行中优先显示 active process，否则显示最近 terminal process，并公开 state、backend、repository、commit、working directory、elapsed 与 active count。

GUI 不推测脚本步骤，也不生成百分比或自然语言进度。实际 stdout/stderr 的最新非空行可作为最新记录；运行中进程显示字面按钮 `[STOP]`，该按钮只停止当前展示的 owned process。短命令与脚本使用同一 process contract。

## 最新记录

GUI 不显示完整 CWapi runtime log、结构化日志列表或历史滚动区。一个不可滚动、无边缘阴影的 viewport 只选择真实时间戳最新的一项：

- process state 或最新 stdout/stderr；
- 最新 structured execution event；
- 最新 runtime error；routine startup/connect 信息不进入 GUI；
- 当前 GUI mutation 的结果或错误。

结构化记录直接序列化为 `key=value`，保留来源、identity 和毫秒时间，不把内部字段改写成人造进度。runtime history 仍可在内部 bounded observability 中用于定位问题，但 Wails desktop snapshot 只公开最新一个 error 候选。

## Permission 与 Slack

permission switch 只调用 Service-owned mutation API。API 持 authorization mutex 完成 config 原子写与 active Token clear；GUI 不直接写 config。运行中可临时切换 `FULL`，但每次 Service 启动都会先原子重置为 `SAFE`，因此 `FULL` 不跨重启保留。

Slack 行显示连接状态与频道；配置 sheet 将 App/Bot Token 直接交给 Credential Manager mutation，前端不持久化 secret。channel id 是唯一写入 config 的 Slack 字段。

## Public data

- process record 不包含 argv、Token、absolute worktree、resolved executable 或内部 handle；
- Slack MCP v2 body 的顶层非空 `system_token` 显示为 `[REDACTED]`；
- unrelated 64hex 与嵌套普通字段保持原样；
- Git credential helper 的 executable/config path 不进入 public snapshot。

## Startup / probe

前端首次读取等待 Core 完成启动，不把瞬时 `CORE_NOT_STARTED` 显示成故障。Wails mount 会产生 `frontend-ready.json`（仅 gate 环境）；`CWAPI_GUI_PROBE_CONFIG` 支持 first-run、workbench、real-slack 自动验收，普通运行不会创建 probe evidence。

关闭窗口默认隐藏到 tray；tray Exit 或应用 shutdown 会停止 Slack、owned process registry 和 Codex process tree。
