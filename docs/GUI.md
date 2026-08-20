# CWapi v1.6.0 GUI

CWapi Desktop 使用 Wails v2 + React + TypeScript。

GUI 的目标不是把 backend state machine 全部搬到屏幕上，而是让普通用户能回答几件最重要的事：

```text
CWapi 现在正常吗？
Slack 连上了吗？
项目配对了吗？
我现在是什么权限模式？
Codex / MCP 能用吗？
刚才的请求发生了什么？
出错应该去哪里看？
```

---

## 1. 页面总览

当前主要页面：

- 控制台；
- 项目；
- 设置；
- 诊断；
- 关于。

如果你是第一次使用，推荐顺序：

```text
设置
 -> 项目
 -> 控制台
 -> 诊断（有问题时）
```

---

## 2. 控制台

普通用户可以把控制台理解成“CWapi 当前工作状态”。

主要关注：

- Slack 状态；
- stock Codex runtime / app-server 状态；
- MCP relay 状态；
- 最近 MCP request 的 method / status / elapsed / delivery；
- structured execution log；
- CWapi runtime log。

### 怎么判断基本可用

至少应确认：

```text
Slack 已连接
Codex runtime 可用
MCP relay ready
没有持续刷新的 fatal error
```

### 最近请求

最近请求不是“完整项目历史管理器”。

它主要用于回答：

```text
这个 request 收到了吗？
运行多久？
completed / failed / unavailable？
是否有 Slack delivery？
```

大日志仍按需查看，不应默认整份塞进 GUI 或 Slack。

---

## 3. 项目页面

项目页维护：

```text
display name
GitHub repository identity
本地项目路径
remote URL
```

CWapi 会为项目维护内部：

```text
project_id = prj-...
```

Web GPT 正常通过 discovery / `projects/list` 取得项目 ID，不需要用户手工复制到每次请求里。

### 本地项目路径的作用

已配置本地路径同时是 `cwapi-safe` Codex profile 的 workspace root 之一。

但是项目执行时真正用于 exact-commit 的工作目录由 CWapi 自己准备 detached worktree，不要求 Web GPT 知道那个路径。

### 修改项目后需要重启吗

通常不需要。

项目或权限 fingerprint 变化后，后续 MCP 调用会建立新的必要 context。

---

## 4. 设置页面

设置页主要维护用户需要明确决定的运行配置。

### Slack

当前 Slack transport 使用：

```text
App Token
Bot Token
Channel ID
```

Token 保存在 Windows Credential Manager，不写入普通配置。

用户主要需要确认：

- token 对应正确 Workspace；
- Channel ID 正确；
- Slack 状态连接成功。

### 权限模式

当前两个主要选择：

```text
safe
full_access
```

默认推荐 `safe`。

---

## 5. 安全权限 `safe`

默认模式。

CWapi 为 Codex-managed execution 使用：

```text
cwapi-safe
```

主要效果：

- 已配置项目作为 managed workspace root；
- CWapi data root 作为 managed workspace root；
- Codex thread 使用对应权限 profile；
- 基础 rules 继续生效。

普通用户如果不知道该选哪个，就保持 `safe`。

---

## 6. 完全访问权限 `full_access`

用户显式开启。

CWapi 为 Codex-managed execution 使用：

```text
cwapi-full-access
```

它扩大 Codex-managed filesystem permission，但并不等于：

```text
关闭所有保护
```

CWapi 仍保留自己的 secret、幂等、owned process、delivery 等边界，也不使用裸 `:danger-full-access` 作为默认实现。

完整安全说明见 [`SECURITY.md`](SECURITY.md)。

---

## 7. command/process MCP 与权限模式的区别

这个区别必须在 GUI 使用说明里讲清楚。

随包 `cwapi` MCP server 的：

```text
process_start
process_status
process_stop
```

可以启动用户明确允许的自由 executable。

这些子进程以当前 Windows 用户权限运行，**不自动继承 Codex thread 的 `cwapi-safe` / `cwapi-full-access` filesystem / execpolicy sandbox**。

所以 GUI 的“安全权限”不能理解成：

> 所有从 CWapi 启动的任意程序都被困在项目目录。

这是两层不同的执行边界。

---

## 8. 诊断页面

诊断页是出问题时最应该先看的页面。

建议至少展示 / 检查：

- CWapi version；
- source commit；
- Slack state；
- Codex executable；
- Codex version / SHA；
- app-server readiness；
- MCP relay readiness；
- active / terminal MCP requests；
- permission mode；
- 最近错误。

### 常见使用方式

如果 Web GPT 说：

```text
CWapi 没有回复
```

先看 Slack state。

如果：

```text
mcpServerStatus/list 失败
```

看 Codex executable / app-server / MCP readiness。

如果：

```text
项目调用失败
```

再检查项目配置、permission mode 和最近结构化错误。

然后根据错误码去 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。

---

## 9. 关于页面

关于页面主要用于确认：

```text
当前 CWapi 版本
发行信息
source commit / build 信息
```

排障和报告问题时，版本号和 source commit 比“我下载的是最新那个 ZIP”更可靠。

---

## 10. Cancel

只有存在真实 backend cancellation contract 时，GUI 才应该显示“可取消”。

当前 stock MCP tool relay 对 in-flight tool call 没有安全的 request-scoped cancel hook，因此不能用：

```text
GUI 状态改成 cancelled
```

冒充：

```text
本机执行真的已经停止
```

对于随包 process server 自己启动并记录的长期进程，应使用真实：

```text
process_stop(process_id)
```

处理。

---

## 11. GUI 不应该再展示的旧架构

不要把这些旧 custom Toolhost / Runner 概念当成 v1.6.0 当前能力：

```text
managed workspace platform
workspace.open
custom test.run
custom build.run
automation.run
旧 Runner task UI
```

当前架构是：

```text
Slack MCP relay
exact commit
stock Codex app-server
configured MCP server
```

---

## 12. Efficiency

GUI 自己也应保持轻量：

- background refresh 节流；
- 日志有界；
- 不默认加载大 resource；
- readiness 查询不启动模型 Turn；
- 不因为 UI 刷新反复重建 app-server / context；
- 大日志与 artifact 按请求需要展开。

GUI 的工作是让状态清楚，不是靠持续刷新把 CPU 和日志都变成装饰灯。