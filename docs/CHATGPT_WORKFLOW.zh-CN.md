# CWapi 1.6.3 ChatGPT 工作流

[English](CHATGPT_WORKFLOW.md) | [简体中文](CHATGPT_WORKFLOW.zh-CN.md)

这是 Web GPT 使用 CWapi 1.6.3 时应遵循的完整执行规则。

## 1. 通信与协议

远程调用方通过配置好的 Slack channel 发送 MCP v2 frame。1.6.3 没有 project registry，repository request 直接携带 GitHub 仓库地址和 exact commit。

完整 frame：

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

规则：

- 第一行必须是 `+++`；
- 每个新动作/新状态快照都使用新的 `request_id`；
- `repository_url` 与 `expected_commit` 要么一起出现，要么都不出现；
- repository URL 是 ASCII GitHub HTTPS 仓库地址；
- `expected_commit` 必须是完整 40 位 hex commit；
- JSON 里的 Windows path 尽量使用 `/`；
- 顶层 `system_token` 只用于允许的 process fallback。

## 2. 标准 repository 流程

1. Web GPT 先通过 GitHub 确认目标 repository 和 exact commit。
2. 在 Slack 发送唯一 request ID 的 MCP v2 request。
3. CWapi 在 claim 前校验 protocol、route、scope、arguments 和 System Token 位置。
4. repository scope 下，CWapi 按需准备 Git 认证，获取 repository lease，准备 shared mirror，并进入该 repository 的 process-lifetime workspace。
5. tracked source 同步到 `expected_commit`；兼容的 ignored/untracked 衍生物不会自动清理。
6. stock MCP 进入 request-scoped context；`cwapi/process_start/status/stop` 由 CWapi Core 作为 virtual tools 处理。
7. CWapi 保存 terminal response，再发回原 Slack thread。
8. 普通 repository request terminal 后只释放 lease，workspace 保留到 CWapi shutdown。

同一个 `request_id` 配合同一个 canonical fingerprint 重投时，可以返回已保存 response；同 ID 却换了内容会产生冲突。

## 3. Workspace 模型

一个 CWapi 进程内，同一 repository 只有一个 persistent workspace。

### 跨 request 可以保留什么

主要是 ignored/untracked 衍生状态，例如依赖目录、构建产物、缓存、生成文件、迁移中间目录等。

### 什么不应该依赖保留

未提交的 tracked source 修改。后续 prepare 可能强制把 tracked 文件恢复到请求的 exact commit。

### 切换 commit

同一 repository 的下一条 request 可以带新的 `expected_commit`。CWapi 按需 fetch，把受管 worktree 强制同步到新 commit，校验 HEAD，而且不会自动执行 `git clean`。

如果旧衍生物与新 tracked tree 冲突，prepare 可以直接失败，而不是偷偷删掉本地状态。确认确实需要清理后，使用项目自己的 cleanup 命令。

### 生命周期

- request terminal：释放 lease，保留 repository workspace；
- CWapi 正常退出：删除 process-lifetime repository workspace；
- 下次启动：清理上一进程遗留 workspace；
- shared bare mirror：继续保留并做安全 prune。

## 4. 任务怎么拆

默认拆成可以独立验证的小步骤。因为 persistent workspace 能保存有效衍生状态，不需要为了保留缓存就把 build、test、copy、upload 等所有动作硬塞进一个巨型脚本。

只有确实需要同一进程内存、活 browser/session、原子事务或其它不可重建状态时，才优先合并步骤。

不同 repository 可以独立并行；同一 repository 在前一个 lease 未释放时会串行等待。

## 5. 搜索源码

Web GPT 需要定位源码时，在当前 repository workspace 里执行只读搜索命令或短脚本。

推荐返回：

```text
path
line
匹配文本
少量上下文
```

尽量限定目录和文件类型，排除无关依赖/build/cache。已经有受管 workspace 后，不要为了搜索再 clone/fetch 同一个仓库。

推荐流程：

```text
GitHub -> exact commit
CWapi -> 本地只读搜索
GitHub -> 精确读取命中文件
Web GPT -> 决定修改
GitHub -> commit 修改
CWapi -> 对新 exact commit build/test
```

## 6. 可执行文件与环境发现

不要假设 CWapi 看到的 PATH 与用户交互式终端一样。

需要某个工具时：

1. 先检查 CWapi 自带/管理的 runtime；
2. 再检查当前 CWapi 进程实际能解析到的本机环境；
3. 两边都没有，才报告为缺失或未验证；
4. 安装只在用户明确授权 `FULL` 后进行，或者由用户手动安装；
5. 安装完成后重新探测真实 executable/version。

`process invocation could not be resolved` 首先应当理解成 executable resolution failure，不要直接宣布项目代码坏了。

## 7. process_start / status / stop

`cwapi/process_start` 是 repository-scoped，在 exact-commit workspace 里执行 command/argv/cwd。

任务很快结束时，start 可以直接返回 terminal record；长任务返回稳定 `process_id`。

进程还在运行时：

- 每次 status 使用新的 global request ID；
- 一直查询同一个 `process_id`；
- 不重复启动原进程；
- 需要停止时用 `process_stop`。

公开状态：`starting`、`running`、`completed`、`failed`、`stopped`。

## 8. SAFE、FULL 与 System Token

每次 Service 启动都会在 runtime 构造前把权限重置为 `safe`。

`SAFE` 把普通 repository 写入限制在受管 writable root，并执行永久 execution policy。

`FULL` 也仍然先走 safe backend。只有 CWapi 认可的结构化 permission denial 才能进入 System fallback，普通 `PROGRAM_FAILURE` 不行。

fallback System Token：

- 最长 60 秒；
- 一次性；
- 受 active token 数量限制；
- 绑定 repository、commit、final executable、argv、real cwd；
- 只能放在 request 顶层。

重试换新的 `request_id`，其它 invocation binding 不变。Token 过期就重新走权限流程；System backend 自己失败不会递归签发第二枚 Token。

## 9. Stock MCP 与浏览器状态

`mcpServerStatus/list` 是 global 调用，params 为空。其它 stock resource/tool 根据 route 可以是 global 或 repository-scoped。

调用方不提供 Codex `threadId`；CWapi 管理 request-scoped execution context。

不要假设两个无关的 stock MCP request 会共享 browser page/tab/locator/session 状态。需要“导航 -> 填表 -> 点击 -> 断言 -> 截图”这种连续浏览器流程时，如果底层工具依赖活 session，就在同一次可保持状态的调用里完成，或者下一次明确重建页面状态。

## 10. 截图和二进制结果

Playwright 截图要经 Slack 返回时，调用 `browser_take_screenshot` 不传 `filename`，让 MCP result 携带真实 image content，CWapi 再通过 Slack 上传。

`./screenshot.png` 这种文本路径只是文本。CWapi 不会根据任意输出路径自动读取本机文件。

## 11. 3 分钟等待规则

单次连续等待/轮询 build、Runner、Slack response 或其它异步结果，累计不得超过 **3 分钟**。

3 分钟仍未 terminal：

1. 停止继续等待；
2. 告诉用户“任务仍在运行”并给当前 request/process 状态；
3. 已经有 `process_id` 就保留；
4. 以后用新的 global status request 查询同一进程。

不能用很多次 30 秒轮询来绕过 3 分钟累计上限。

## 12. 调用方责任

Web GPT 应当：

- 每个新动作/状态查询用新的 request ID；
- 始终使用准确 repository/commit；
- 不重复 clone 当前受管的同一 repository；
- 源码搜索保持只读、聚焦；
- tracked source 的长期修改通过 GitHub 保存；
- 有效的 workspace 衍生状态可以复用，不无理由重复安装/构建；
- Slack/System credential 不进入 prompt、issue、日志和长期文档；
- 区分 permission denial 与普通程序失败；
- 需要返回文件时要求底层工具返回真实 binary/resource content，而不是只打印路径。
