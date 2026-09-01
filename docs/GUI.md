# CWapi 2.0.2 GUI

窗口固定为 430 × 625、frameless、不可拉伸；主内容区支持鼠标滚轮纵向滚动；标题栏 `×` 只隐藏主窗口到系统托盘，不终止 CWapi，真正退出由托盘菜单执行；single-instance 再次启动会唤醒已有窗口。主界面通过页签分为 Coding 与 Agent 两页，两个页面只显示并管理各自链路。

## 页面结构

```text
CWapi
Coding | Agent
当前链路的 Tunnel 卡片
当前链路的运行卡片
当前错误码（仅失败时）
```

- Coding 页：Coding Tunnel、Codex SAFE/FULL、独立的命令网络能力、active session 与 workspaces。
- Agent 页：Agent Tunnel、enabled、Provider URL、API key、bridge Offline/Ready/Busy、pending/claimed/completed。
- Tunnel 初始启动失败或异常退出后的自动退避重启显示为“正在重连”；断开或重配置会取消重启。
- Tunnel ID 只在可编辑输入框中显示，不再额外渲染重复的“隧道 ID / 复制”行。
- 本地 `127.0.0.1` MCP 地址不在主页面展示；它仍作为本机 MCP 客户端和 Tunnel 的内部转发目标保留。
- 页面不保留日志历史；失败时只显示当前错误码，不显示路径、密钥或原始错误详情。

Coding 与 Agent 没有模式切换器，两条链可同时工作。页签切换不会清空另一页的输入状态。

所有需要确认的危险操作（断开隧道、删除工作区）使用应用内确认窗口，不调用浏览器原生确认框；窗口明确显示影响范围、取消按钮和带动作含义的确认按钮。

## 配置 mutation

GUI 可以：

- SAFE/FULL 切换；
- Coding 命令网络访问开关；
- Agent enable/disable；
- Agent API key regenerate；

SAFE/FULL 与 Coding network access 都是运行时能力，可在 Coding session 保持打开时切换，不重启 Service，也不改变 active internal Coding session；已在执行的命令保持启动时的 sandbox/network，下一条 coding_exec 使用新设置。网络默认关闭，FULL push 要求显式启用。其它需要重启 Service 的配置变更在 active Coding session 或 pending/claimed Agent request 存在时仍拒绝。

## Workspace maintenance

独立 overlay 显示 canonical repository。delete/rebuild 需要二次确认，操作期间 Service 停止；它不是远程 MCP tool。

## 隐私与可用性

- token/key 默认遮蔽；复制失败不得显示成功状态；
- mutation 失败显示当前错误码，并保留尚未成功保存的 Tunnel key 输入；
- UI 不显示完整 prompt、answer、tool arguments 或 Codex transcript；
- 状态来自 Go snapshot，不伪造进度；
- packaged GUI gate 使用真实 `CWapi.exe` 与 WebView 验证启动、布局、mutation guard 和 listener 状态。
