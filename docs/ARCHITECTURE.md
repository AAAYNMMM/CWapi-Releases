# CWapi 2.0 架构

本文只描述 CWapi 2.0 当前实现目标。

## 产品边界

CWapi 2.0 只面向一个 Windows 用户、一个设备和一个本地进程。长期维护优先消除实际故障、资源泄漏、无恢复终态与重复开销；不为多用户、多租户、集群、远程管理、企业审计或假设性的额外安全边界增加 abstraction、registry、state machine 或配置。现有 loopback、凭据隔离和 bounded queue 等直接运行边界继续保持。

## 进程结构

```text
CWapi.exe (Wails)
├─ Desktop facade
├─ Config owner
├─ loopback MCP listener
│  ├─ /mcp/coding/<token> -> isolated Coding mcp.Server
│  └─ /mcp/agent/<token>  -> isolated Agent mcp.Server
├─ bundled OpenAI tunnel-client (Coding, optional)
│  └─ main -> /mcp/coding/<token>
├─ bundled OpenAI tunnel-client (Agent, optional)
│  └─ main -> /mcp/agent/<token>
├─ Coding service
│  ├─ durable workspace manager
│  ├─ layered security + Git recovery manager
│  └─ private Codex model-free command/process toolhost
└─ Agent service
   ├─ OpenAI-compatible protocol adapter
   ├─ canonical conversation + context optimizer
   ├─ bounded broker / MCP bridge
   └─ loopback OpenAI-compatible Provider
```

Coding 与 Agent 共享进程生命周期和 authoritative config，但不共享 MCP token、tool catalog 或业务状态。Codex 不可用不会阻塞 Agent；Agent bridge 不存在也不会阻塞 Coding。

## Coding vertical

```text
Web GPT
  -> Coding MCP
  -> canonical repository reservation
  -> bundled MinGit clone/fetch/inspect
  -> CWapi-data/workspaces/<hash>/repo
  -> local target branch tracking origin
  -> Permanent Guard -> SAFE/FULL -> capabilities
  -> bundled Codex app-server command/exec
  -> per-command empty CODEX_HOME
  -> edit/test/commit/push result
```

Coding MCP 对外暴露五个工具：`coding_open`、`coding_exec`、`coding_status`、`coding_close`、`load_skill`。

Coding 不提供文件或图片 MCP 传输。文本、源码、JSON、日志等需要的 workspace 内容通过 exact `coding_exec` 获取。

一个 repository 同时最多一个 active coding handle。首次使用 clone，后续 fetch；`expected_commit` 如提供则必须是目标 ref 解析出的完整 SHA。新任务遇到 tracked dirty 会拒绝，`resume=true` 可保留工作树继续。

### Active handle rediscovery

ChatGPT conversation 的生命周期与 CWapi Coding session 生命周期不是同一个东西。Web GPT 对话关闭或丢失时，CWapi 收不到可靠的“这个 conversation 已结束”信号，因此不能依赖 conversation close 自动释放 repository owner。

如果同一 repository 已有 active session，新的 Web GPT conversation 使用兼容的 `coding_open(..., resume=true)` 时，Coding service 通过 canonical repository 找到并复用原 active internal session，不重复 prepare workspace。Web GPT 不接收也不恢复随机 session ID；内部 generation 仍用于并发、取消、close race 与 stale-operation 防护。

`resume=false` 仍保持 one-active-session protection 并返回 `CODING_WORKSPACE_BUSY`。正在 opening/closing 的 session 或 target ref / expected commit 不兼容的请求不会被静默接管。

Web GPT 是 Coding 链唯一的推理 agent。每次 `coding_exec` 都把严格 `command + argv + repository cwd` 发送到私有 app-server 的 `command/exec`；不创建 Codex thread/turn，不调用 auth/account/model API，也不读取用户 `~/.codex`。每条命令使用独立临时 CODEX_HOME，结束后删除。省略 `action` 是 foreground `run`；`start/status/stop` 将长期命令交给 Host process manager，workspace close 与应用退出统一回收。

`coding_status` 只读本地 HEAD、tracking ref、dirty 与 divergence，不执行隐式 fetch，也不读取 Codex transcript。

每条 Coding 命令在 resolver 产生最终 executable/argv/CWD 后经过四层：Permanent Safety Guard、SAFE/FULL profile、Network/Remote Git Rewrite capabilities、execution。Permanent Guard 只覆盖磁盘/启动、自动提权、CWapi 敏感内部路径、可信 Git、unsafe push transport/receive-pack/safety-ref 等灾难边界，不再包含普通进程控制、credential plumbing、正常 Git 或 shell 文本语义 parser。

SAFE 映射上游 `workspaceWrite`，隔离宿主身份与配置；cache 在 `CWapi-data/runtime/workspaces/<hash>/cache` 按 workspace 复用，Temp/bridge/profile 在 `CWapi-data/runtime/process/<process-id>` 按命令清理。FULL 映射 `dangerFullAccess`，从宿主环境开始仅剥离 CWapi/OpenAI/Codex 内部 secret，保留正常 Git/GitHub CLI/SSH/hooks/signing、package manager 与 SDK 环境。GitHub CLI identity 统一位于 `CWapi-data/auth/github`。Network 对两个 profile 都正交；Remote Git Rewrite 默认关闭，只控制 direct force/delete remote update。

Workspace prepare 使用 local tracking branch，不以 detached HEAD 作为正常状态；clean/no-local-history 时仅 fast-forward。dirty、local commits 或 divergence 均不自动覆盖。可能丢失本地内容的 direct Git 操作前创建最多 32 个 `refs/cwapi/safety/*` 恢复点；该 namespace 永不允许 push。

## Agent vertical

```text
local software
  -> OpenAI-compatible Provider
  -> Adapter -> canonical conversation -> deterministic optimizer
  -> Broker request lifecycle + runtime heartbeat
  -> Agent MCP -> Web GPT
  -> structured tool_call/completion/error
  -> local tool result -> next OpenAI request
```

`internal/v2/agentprotocol` remains the formal external-protocol boundary. 2.0.5 adds stream argument assembly so tool-call argument deltas are accumulated completely before one canonical JSON parse.

Broker request and bridge lifetimes are separate. Request states include `QUEUED / CLAIMED / RUNNING / WAITING_TOOL / COMPLETED / FAILED_RETRYABLE / FAILED_FINAL`; old pending/inflight/claimed fields remain additive compatibility views. Runtime heartbeat renews active request liveness independently from Web GPT natural-language output and does not masquerade as business progress.

Bridge close/stale detaches active requests instead of final-failing them. Reopen creates/reuses a valid bridge generation, rebinds preserved requests, and redelivers the same `request_id` with `delivery++` plus resume metadata. Operations bound to an old generation remain stale and cannot complete a request through the new generation.

Tool-call parse/mapping errors carry structured retryable identity and move the request to `FAILED_RETRYABLE`; valid corrected responses can continue. Tool calls enter `WAITING_TOOL`, release the current local HTTP waiter, and the local executor's tool result arrives in the subsequent OpenAI request. One tool failure is therefore normal task feedback rather than automatic broker failure.

A brand-new OpenAI request still requires an active bridge and fails fast with HTTP 503 while offline. This keeps client behavior compatible while allowing already-existing work to survive bridge interruption.

Exchange output separates heartbeat, progress, tool_call, tool_result, completion and error concepts and exposes `queued_requests/active_requests`, heartbeat/progress observations and lifecycle/resume timing alongside legacy activity fields. `no_request` continues to mean only bounded wait timeout without a new local OpenAI request.

Agent accepts text/tool JSON only; files/images remain rejected before broker admission. Current SSE keeps transport keepalive and buffered completion encoding; it does not claim token-level realtime streaming.

## 启动与重配置

应用在 single-instance lock 后创建 2.0 Service：

1. 读取或创建 strict v3 config；
2. 创建 workspace manager、Coding/Agent services；
3. 启动一个 MCP listener；
4. Agent enabled 时启动 Provider；
5. Coding/Agent Tunnel 各自启用时分别启动对应的 bundled tunnel-client；
6. 发布 bounded Desktop snapshot。

Desktop 的 SAFE/FULL access profile、Coding network 与 Remote Git Rewrite capability 都使用原子配置保存 + 当前 Coding runtime 热更新，不重启 Service，也不失效 active internal Coding session；在执行中的 command 保持其启动时配置，后续 command 使用新设置。Remote Git Rewrite 开启需要应用内确认。其它 Desktop 配置修改仍先检查 active Coding 与 pending/claimed Agent，允许后执行 atomic save + Service restart；启动失败则恢复原配置并重启原 Service。
Tunnel Runtime API key 不进入 config；启用后由 Service 从各自的 Windows Credential Manager 条目读取，并只通过环境变量注入对应的 bundled tunnel child process。两个 tunnel-client 使用独立 profile、工作目录和 `main` 转发目标。
任一 tunnel-client 异常退出后，本地 Manager 会以指数退避自动重启最多 3 次；关闭或重配置会取消待执行的重启。

## Workspace maintenance

Workspace delete/rebuild 只存在于 Desktop maintenance surface，不暴露给 MCP。维护前停止 Service，确认目标位于 workspace root，再删除选定 repository 与对应的 `runtime/workspaces/<hash>` cache；下一次 `coding_open` 自动 clone。

## Package

```text
CWapi.exe
portable-manifest.json
prompts/...
runtime/codex/current/...
runtime/git/...
runtime/tunnel/current/tunnel-client.exe
```

用户数据始终在 portable 旁的 `CWapi-data`，不得进入 stage/ZIP。candidate 来自 clean exact commit；runtime 来自经过 lock/hash 验证的安装树，不依赖正在运行的其他 portable 输出。
