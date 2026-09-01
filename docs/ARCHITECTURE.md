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
│  └─ private Codex model-free command toolhost
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
  -> bundled Codex app-server command/exec
  -> per-command empty CODEX_HOME
  -> edit/test/commit/push result
```

Coding MCP 对外暴露四个工具：`coding_open`、`coding_exec`、`coding_status`、`coding_close`。

Coding 不提供文件或图片 MCP 传输。文本、源码、JSON、日志等需要的 workspace 内容通过 exact `coding_exec` 获取。

一个 repository 同时最多一个 active coding handle。首次使用 clone，后续 fetch；`expected_commit` 如提供则必须是目标 ref 解析出的完整 SHA。新任务遇到 tracked dirty 会拒绝，`resume=true` 可保留工作树继续。

### Active handle rediscovery

ChatGPT conversation 的生命周期与 CWapi Coding session 生命周期不是同一个东西。Web GPT 对话关闭或丢失时，CWapi 收不到可靠的“这个 conversation 已结束”信号，因此不能依赖 conversation close 自动释放 repository owner。

如果同一 repository 已有 active session，新的 Web GPT conversation 使用兼容的 `coding_open(..., resume=true)` 时，Coding service 通过 canonical repository 找到并复用原 active internal session，不重复 prepare workspace。Web GPT 不接收也不恢复随机 session ID；内部 generation 仍用于并发、取消、close race 与 stale-operation 防护。

`resume=false` 仍保持 one-active-session protection 并返回 `CODING_WORKSPACE_BUSY`。正在 opening/closing 的 session 或 target ref / expected commit 不兼容的请求不会被静默接管。

Web GPT 是 Coding 链唯一的推理 agent。每次 `coding_exec` 都把严格 `command + argv + repository cwd` 发送到私有 app-server 的 `command/exec`；不创建 Codex thread/turn，不调用 auth/account/model API，也不读取用户 `~/.codex`。每条命令使用独立临时 CODEX_HOME，结束后删除。

`coding_status` 只读本地 HEAD、tracking ref、dirty 与 divergence，不执行隐式 fetch，也不读取 Codex transcript。

每条 Coding 命令在 resolver 产生最终 executable/argv/CWD 后先通过 CWapi 永久执行策略，再调用 app-server。SAFE 映射上游 `workspaceWrite` 并使用命令级 workspace-local Temp/cache/profile；FULL 对所有通过永久策略的命令映射 `dangerFullAccess`，并恢复当前 Windows 用户的 profile/AppData 环境。网络仍是独立、默认关闭的能力；只有已显式启用网络的直接 FULL push 会额外恢复宿主 Git config/credential helper。破坏性 Git、凭据读取、禁止系统工具与 protected-path 规则不会因 FULL 失效。CWapi 不实现 reusable elevation token 或 fallback。

## Agent vertical

```text
local software
  -> POST 127.0.0.1:<agent-port>/v1/chat/completions
  -> Bearer API key
  -> OpenAI Compatible Adapter
  -> CWapi canonical conversation
  -> deterministic Context Optimizer
  -> bounded broker / MCP bridge
  -> agent_exchange batch claim
  -> Web GPT
  -> agent_exchange response -> canonical completion
  -> OpenAI Compatible Adapter
  -> OpenAI-compatible JSON or keepalive SSE
```

Agent 只有一个 active bridge，重复 `agent_open` 会恢复并续租同一 bridge。Web GPT 是本地 Agent 软件实际使用的模型，也是唯一任务规划与决策主体；CWapi 只做协议转换、确定性上下文优化和 bridge/OpenAI request 状态维护，本地软件只执行工具。没有第二个本地推理模型。第三方 command/session 生命周期不属于 broker，不能从 MCP `request_id` 或 `no_request` 推断。

`internal/v2/agentprotocol` 是 2.0.3 的正式协议边界：Adapter 将外部协议映射到轻量 canonical message/tool/completion/error/stream 类型；Context Optimizer 删除未映射的客户端私有字段，规范化 metadata、JSON tool result 与可证明重复的 system/developer/tool 状态；bridge codec 再生成稳定的 MCP request，并将 Web GPT response 转回 canonical completion。Broker 不再解析客户端协议。当前注册的是 OpenAI-compatible adapter；接口允许后续增加真实需要的 adapter，但不宣称支持所有 Agent 软件。

Adapter 能力明确报告 `streaming/tools/parallel_tools=true`、`images/files=false`。不支持的文件、图片或无法关联的 tool result/call 会在协议边界返回稳定错误，不交给 Web GPT 猜测。角色与 tool call ID 在往返转换中保持；unknown fields 默认不会穿过 canonical bridge。当前 SSE 仍是 keepalive 加完成后的 buffered chunks，不伪造 token-level stream，但 canonical `StreamChunk` 与 adapter 双向转换保留未来真实流式扩展点。

每次 `agent_exchange` 原子提交上一批响应，默认最多 4 个 in-flight；已完成 response 会立即唤醒本地 HTTP request。非-tool-call终态 response 以 `state=responses` 立即向 Web GPT 确认；若任一 response 返回 `tool_calls`，当前 exchange 保持 bounded wait 并可直接领取本地工具执行后产生的 follow-up OpenAI request。只有真实 wait timeout 返回 `no_request`。claimed request 使用相同 request ID 与递增 delivery 至少一次投递。相同响应可幂等重交，不同响应冲突拒绝；一个无效响应不影响同批其他 request。bridge lease 过期、close、client disconnect 与 timeout 都有明确终态和清理，仍在处理的 claimed request 会固定 bridge lease。

Broker 维护单调 state revision，并在 exchange output 中返回 changed/pending/inflight/active/idle/waited/last-state/next-action activity。`no_request` 只表示 bounded wait 内没有新 OpenAI request；连续无 transition 的 idle count 为 Web GPT 提供防空转信号。Returned request 还包含 created/claimed/last-delivered/deadline 时间。本地 client 可通过标准 `metadata.task_id` / `metadata.correlation_id` 提供跨 request 稳定关联；CWapi 不凭语言内容伪造 task 或 process 状态。

Agent 不提供文件或图片输入通道。Provider 只接受文本与 tool JSON；顶层 `attachments` 以及 message content 中非 `text` part（包括 `image_url`）都在 broker 入队前拒绝，`agent_exchange` 不生成 MCP 文件或图片 content。

## 启动与重配置

应用在 single-instance lock 后创建 2.0 Service：

1. 读取或创建 strict v3 config；
2. 创建 workspace manager、Coding/Agent services；
3. 启动一个 MCP listener；
4. Agent enabled 时启动 Provider；
5. Coding/Agent Tunnel 各自启用时分别启动对应的 bundled tunnel-client；
6. 发布 bounded Desktop snapshot。

Desktop 的 SAFE/FULL access profile 与 Coding network capability 都使用原子配置保存 + 当前 Coding runtime 热更新，不重启 Service，也不失效 active internal Coding session；在执行中的 command 保持其启动时 sandbox/network，后续 command 使用新设置。其它 Desktop 配置修改仍先检查 active Coding 与 pending/claimed Agent，允许后执行 atomic save + Service restart；启动失败则恢复原配置并重启原 Service。
Tunnel Runtime API key 不进入 config；启用后由 Service 从各自的 Windows Credential Manager 条目读取，并只通过环境变量注入对应的 bundled tunnel child process。两个 tunnel-client 使用独立 profile、工作目录和 `main` 转发目标。
任一 tunnel-client 异常退出后，本地 Manager 会以指数退避自动重启最多 3 次；关闭或重配置会取消待执行的重启。

## Workspace maintenance

Workspace delete/rebuild 只存在于 Desktop maintenance surface，不暴露给 MCP。维护前停止 Service，确认目标位于 workspace root，再删除选定 repository；下一次 `coding_open` 自动 clone。

## Package

```text
CWapi.exe
portable-manifest.json
runtime/codex/current/...
runtime/git/...
runtime/tunnel/current/tunnel-client.exe
```

用户数据始终在 portable 旁的 `CWapi-data`，不得进入 stage/ZIP。candidate 来自 clean exact commit；runtime 来自经过 lock/hash 验证的安装树，不依赖正在运行的其他 portable 输出。
