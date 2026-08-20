# CWapi v1.6.0 运行与维护

## Startup

1. 加载 CWapi config，并清空上一进程的 request/log/error 会话 state；
2. 加载 Credential Manager；
3. 初始化 Slack；
4. 验证 packaged stock Codex executable；
5. 启动 Wails UI。

app-server 按需启动。readiness 查询不需要制造模型 Turn。

## First MCP call

1. 读取当前 permission mode 和项目列表；
2. 生成/刷新 CWapi `CODEX_HOME`；
3. 启动 `codex app-server --stdio`；
4. initialize experimental app-server API；
5. 创建 ephemeral MCP context thread；
6. 在 `thread/start` 传入 `cwd` + `permissions`；
7. 转发 stock MCP request。

## Context reuse

permission profile + project roots 的 fingerprint 未变化时复用 app-server；无 project 的 context 同步复用，exact-worktree context 按调用释放。fingerprint 变化时等待现有调用结束再关闭旧 client/context 并重建，避免“设置已变、旧 thread 还活着”的 split-brain。

## Recovery

- app-server 退出：后续调用重建；
- ambiguous tool failure：不自动 replay；
- 同一进程内 Slack 断线：保留 terminal response，只补游标后的消息；
- CWapi 重启：建立新会话，不拾取上一进程任务或既有 Slack history；
- duplicate request：不启动第二次调用。

## Shutdown

- 停止 Slack；
- 释放 Codex process scope；
- 只结束 CWapi 拥有的 app-server process tree；
- 关闭 state store。

## Logs

保留两类：

- structured MCP execution / delivery log；
- CWapi runtime log。

大日志按需读取，不默认上传整份内容。

## Security operations

排查权限问题时看：

1. 当前 `permission_mode`；
2. 生成的 `CODEX_HOME/config.toml`；
3. `rules/default.rules`；
4. thread/start 是否选择正确 profile；
5. 目标 MCP server 是否真正支持/运行于 Codex sandbox 权限边界。

不要从 Slack 消息文本反推权限状态。
