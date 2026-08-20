# CWapi v1.6.0 GUI

CWapi Desktop 使用 Wails v2 + React + TypeScript。GUI 只展示用户真正需要的状态，不复制 backend state machine。

## 页面

- 控制台；
- 项目；
- 设置；
- 诊断；
- 关于。

## 项目

项目页维护：

- display name；
- GitHub repository identity；
- 本地项目路径。

已配置本地路径同时成为 `cwapi-safe` Codex profile 的 workspace root。项目变化后无需手工重启，下一次 MCP 调用会创建新的 permission context。

## 权限设置

界面保留两个可选模式：

### 安全权限

默认。只允许 Codex managed profile 在已配置项目和 CWapi data root 内写入。

### 完全访问权限

用户显式开启。扩大 Codex filesystem permission，但仍受基础层 execpolicy/system-path deny 约束。

GUI 文案应明确：**基础层始终生效**。

GUI 不暴露 Codex 内部几十个 permission 字段，也不让用户直接输入 profile ID。

## 控制台

显示：

- Slack 状态；
- stock Codex runtime/app-server 状态；
- MCP relay 状态；
- 最近 MCP request 的 method/status/elapsed/delivery；
- structured execution log；
- CWapi runtime log。

不要再显示 custom Toolhost、managed workspace、`automation.run` 等已删除架构为当前能力。

## Cancel

只有存在真实 backend cancellation contract 时才显示“可取消”。当前 stock MCP tool relay 对 in-flight tool call 没有安全 request-scoped cancel hook，因此不能把 UI 状态变化冒充本地执行已停止。

## Diagnostics

显示：

- CWapi version/source commit；
- Slack state；
- Codex executable/version/SHA；
- app-server/MCP relay readiness；
- active/terminal MCP requests；
- permission mode；
- 最近错误。

## Efficiency

- background refresh 节流；
- 日志有界；
- 不默认加载大资源；
- readiness 查询不启动模型 Turn；
- 不因为 UI 刷新反复重建 app-server/context。
