# CWapi v1.6.0 GUI

这份文档只解释 **CWapi Desktop 每个页面是做什么的**。Slack App 创建见 [`SLACK_SETUP.md`](SLACK_SETUP.md)，权限实现见 [`SECURITY.md`](SECURITY.md)，运行恢复见 [`OPERATIONS.md`](OPERATIONS.md)。

CWapi Desktop 使用 Wails v2 + React + TypeScript。

## 1. 页面总览

当前主要页面：

```text
控制台
项目
设置
诊断
关于
```

第一次使用推荐顺序：

```text
设置 Slack
→ 添加项目
→ 保持 safe 权限
→ 回控制台确认状态
→ 有问题再看诊断
```

## 2. 控制台

控制台用来回答：

```text
CWapi 现在正常吗？
Slack 连上了吗？
Codex / MCP ready 吗？
刚才 request 成功还是失败？
```

主要状态包括：

- CWapi Desktop；
- Go Core / MCP Relay；
- Slack Transport；
- Codex app-server / MCP Relay；
- CWapi process MCP。

下面两块日志分别是：

```text
结构化执行日志
CWapi 运行日志
```

结构化执行日志更适合看某个 request / tool 的状态和耗时；运行日志更适合看 Slack、Codex、MCP 和 runtime 自己的错误。

日志是诊断面，不是完整项目历史数据库；大日志按需看。

## 3. 项目页面

点击：

```text
＋ 添加项目
```

当前表单主要填写：

```text
项目名称
本地路径
Git 地址
```

保存后项目卡片会显示：

```text
项目 ID
本地路径
Git 地址
```

`project_id = prj-...` 由 CWapi 维护。正常 Web GPT 会通过 `projects/list` / discovery 自动取得，不要求用户每次手抄。

项目执行时 CWapi 会另外准备 exact-commit worktree；GUI 里填写的本地项目路径不是要求 Web GPT 直接操作的临时 worktree 路径。

## 4. 设置页面

设置页目前主要有三类内容。

### 界面

可以调整：

```text
日志字号
自动滚动到最新日志
```

### 权限

当前两个模式：

```text
安全权限 / safe
完全访问权限 / full_access
```

第一次使用保持 `safe` 即可。

这里切换的是 **Codex-managed execution** 的默认权限 profile。自由 command/process MCP 的真实边界不同，完整说明见 [`SECURITY.md`](SECURITY.md)。

### Slack

显示当前：

```text
Workspace
控制频道名称
Channel ID
Credential Store
```

点击：

```text
更换 Slack 配置
```

会出现：

```text
App Token
Bot Token
Channel ID
验证并保存
```

第一次不知道这些值从哪里来，不要在 GUI 文档里猜，直接按 [`SLACK_SETUP.md`](SLACK_SETUP.md) 配置。

验证成功后 token 存 Windows Credential Manager，不显示明文保存值。

## 5. 诊断页面

这是出问题时优先看的页面。

主要检查：

- CWapi version / source commit；
- Slack state；
- Codex executable / version / SHA；
- app-server / MCP readiness；
- permission mode；
- 活动错误。

常见顺序：

```text
Slack 没回复
→ 先看 Slack state

MCP 调不动
→ 看 Codex / MCP readiness

项目 request 失败
→ 看项目配置 + 活动错误
```

然后根据错误码去 [`TROUBLESHOOTING.md`](TROUBLESHOOTING.md)。

## 6. 关于页面

用于确认：

```text
CWapi 版本
source commit
运行平台
Stock Codex 版本
```

报告问题时，`version + source commit` 比“我下载了最新 ZIP”可靠得多。

## 7. 关于 Cancel / Stop

当前 stock MCP relay 没有一个可以安全映射为“任意 in-flight tool 已真正取消”的 request-scoped cancel contract，因此 GUI 不应靠改状态伪装取消成功。

如果是 `cwapi/process_start` 启动并记录的长期进程，真正停止它使用：

```text
process_stop(process_id)
```

## 8. GUI 不代表旧架构

v1.6.0 当前核心是：

```text
Slack MCP relay
→ exact commit
→ stock Codex app-server
→ configured MCP server
```

旧版 Runner、custom Toolhost、`workspace.open`、`test.run`、`build.run`、`automation.run` 等不要从历史截图或旧文档里当成当前 GUI 能力。

## 9. GUI 设计原则

GUI 只展示用户真正需要的状态，不复制 backend state machine：

- background refresh 节流；
- 日志有界；
- readiness 查询不启动模型 Turn；
- 不默认加载大 resource；
- 不因为 UI 刷新反复重建 app-server / context。

需要使用步骤看 [`USER_GUIDE.md`](USER_GUIDE.md)，不要让 GUI 文档再次长成第二份用户手册。