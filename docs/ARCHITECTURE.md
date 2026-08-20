# CWapi v1.6.0 架构

## 1. 核心定位

CWapi 是 Web GPT、Slack 与本机 stock Codex app-server 之间的轻量 MCP Relay。

```text
Web GPT
  ├─ GitHub
  └─ Slack
      │
      ▼
    CWapi
      │ JSON-RPC
      ▼
stock Codex app-server
      │
      ▼
configured MCP servers
```

CWapi 不承担 Agent planning，也不实现第二套 Git/Test/Build/File 语义平台。packaged `cwapi` MCP server 只实现透明 command/process lifecycle。

## 2. CWapi 职责

- Slack Socket Mode / Web API；
- MCP envelope 与 schema；
- source/channel 校验；
- duplicate / idempotency；
- request/result/delivery state；
- stock Codex runtime 校验与 lifecycle；
- MCP context thread lifecycle；
- 三层权限模式选择；
- logs / diagnostics；
- Credential Manager。

## 3. Codex 职责

CWapi 启动：

```text
codex app-server --stdio
```

初始化时启用 app-server experimental API。CWapi 只把以下 stock MCP 方法暴露给 Slack relay：

```text
mcpServerStatus/list
mcpServer/resource/read
mcpServer/tool/call
```

内部控制：

```text
thread/start
thread/unsubscribe
```

MCP call 必须绑定真实 `threadId`。CWapi 创建 `ephemeral=true` 的 context thread，但**不调用 `turn/start`**，因此不需要模型 Turn 来转发 MCP。

## 4. Permission context

每个 context thread 由 CWapi 传入：

```text
cwd
permissions
```

可选 profile：

```text
cwapi-safe
cwapi-full-access
```

profile 定义写入 CWapi 自己的 `CODEX_HOME/config.toml`，永久基础命令规则写入 `CODEX_HOME/rules/default.rules`。

权限模式或项目列表变化时，CWapi 不修改正在使用的 thread 语义，而是在下一次 MCP 调用前关闭旧 context 并创建新 context。

## 5. Project configuration

项目列表仍是 CWapi 的产品配置。当前它至少用于：

- GUI 展示；
- 为 `cwapi-safe` 生成允许写入的 Codex workspace roots；
- 后续需要时提供项目级路由上下文。

CWapi 不再为每个项目维护 custom managed MCP workspace 状态机。

## 6. State

SQLite 保存当前 CWapi 进程会话需要的事实：

- request identity / fingerprint；
- terminal response；
- delivery state；
- bounded observability data。

应用启动先清空上一进程的 request、execution、runtime log、error 和 Slack cursor，再建立新会话。项目配置、Credential Manager 凭据和 workspace/cache 不属于任务会话，不受影响。SQLite 不保存 custom Tool/workspace 执行平台状态。

## 7. Failure semantics

- app-server process 失效：丢弃 client/context，下次调用重建；
- MCP tool 调用发生不确定 transport/process failure：不自动 replay；
- 同一进程内 Slack delivery failure：保留 terminal response，只重发结果，不重跑调用；应用重启不恢复旧结果。

## 8. Security boundary

Slack 做 transport/access 校验；Codex 做本地执行 permission profile / sandbox / exec policy。两者不能混成一层。

任意第三方 local stdio MCP server 只有在其执行受 Codex sandbox/environment 管理，或它支持 Codex permission/sandbox-state 协议时，才能被视为受 thread permission 约束。

packaged `cwapi/process_start` 是明确例外：它接受自由 executable `command + argv`，初始 CWD 绑定 detached exact-commit workspace，但子进程以当前 Windows 用户权限运行，可以访问 workspace 之外的位置。CWapi 不解析命令语义、不管理 runtime；它只向子进程提供不含 CWapi/Slack/Codex secret 的受限环境，并跟踪 stdout/stderr、状态与 owned process tree。

## 9. Runtime

使用 stock `rust-v0.144.4` source tree，不维护 CWapi 私有 Codex fork 功能。CWapi 只固定并验证 packaged executable hash。
