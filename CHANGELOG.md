# Changelog

## 2.0.5 — 2026-09-02 — Current implementation

2.0.5 聚焦 Agent 长任务可靠性、提示词分层和开发运行时回归：

- Agent request 生命周期扩展为 QUEUED/CLAIMED/RUNNING/WAITING_TOOL/COMPLETED/FAILED_RETRYABLE/FAILED_FINAL，并保留旧 pending/inflight/claimed 字段作为兼容视图；
- heartbeat 由 CWapi runtime 独立维护，progress/tool_call/tool_result/completion/error 分离；模型自然语言沉默不再等价于 request 死亡；
- bridge 与 request 生命周期解耦，bridge detach/reopen 后 active request 以相同 request_id、递增 delivery 和 resume metadata 继续；无 bridge 的新 HTTP request 仍快速 503；
- tool-call parse/mapping error 返回带 request/tool identity 的结构化 retryable error，不再把 request 永久留在 CLAIMED；本地 tool error 可作为正常 tool_result 进入下一轮并最终 completion；
- OpenAI-compatible function.arguments 保持 native object/JSON string 单次 canonicalize；新增 stream assembler，所有 delta 完整拼接后才做一次 JSON parse，覆盖 Windows path、CRLF、quotes/backslashes、Unicode 与大文本；
- MCP instructions 从 Go 内置经验拆成全局 `prompts/`：Coding/Agent 独立 Core + Rules，共享 Skills；启动只缓存一次，initial context 只含 Core + Rules + Skill list，按需通过 `load_skill(name)` 载入具体 Skill；
- portable/candidate/privacy gate 正式携带并审计 `prompts/`，避免开发环境可运行而发布包缺失 prompt resources；
- 修复开发中实际复现的 2.0.4 Coding foreground owner 残留：终态命令立即释放 busy owner，连续命令不再要求重启，真实并发仍返回 CODING_COMMAND_ACTIVE；
- `coding_status` 在真实 busy 期间返回 active action/executable/start time/elapsed seconds，帮助区分正常长命令与 stale owner；为避免泄露命令行中的 token 或其它敏感参数，不回显 argv；
- 修复 CWapi 自身深层 runtime TEMP 下 Windows Git workspace 测试撞 MAX_PATH 的回归夹具，保持生产 durable workspace/hash 不变；
- config schema 继续为 `cwapi.config.v3`，valid 2.0.4 原子迁移到 2.0.5 并保留现有 Remote Git Rewrite 等用户设置；GUI/package metadata 同步为 2.0.5。

发行包对应开发仓库源码提交：

```text
176d32e6d3caa6e069f0b73e1ab86c2604ce8915
```

## 2.0.4 — 2026-09-02

2.0.4 将 Coding 安全管理从集中式命令特判重构为明确的分层边界：

- 新增 `internal/security`，按 Permanent Safety Guard、SAFE/FULL profile、Network/Remote Git Rewrite capabilities 与 execution 分责；`executionpolicy` 只保留兼容 facade；
- Permanent Guard 缩减为磁盘/启动、自动提权、CWapi 敏感内部路径、可信 Git、unsafe push transport/receive-pack/safety-ref 等灾难级保护；移除普通进程控制、credential plumbing、正常 Git 参数与 shell 文本语义黑名单；
- SAFE 保持 Codex `workspaceWrite`、隔离身份与配置；Go/npm/pip/Cargo/Gradle/XDG cache 改为 workspace 生命周期，Temp/profile/bridge 保持 command 生命周期；
- FULL 使用 `dangerFullAccess`，从宿主 Windows 用户环境开始仅剥离 CWapi/OpenAI/Codex 内部 secret，恢复正常 Git/GitHub CLI/SSH/hooks/signing、npm/pip/Cargo/Gradle 与 SDK/toolchain 行为；
- GitHub CLI identity 统一迁移到 `CWapi-data/auth/github`，跨命令、跨 Coding workspace 复用；runtime/cache/bridge 全部移到 `CWapi-data`，不再污染 Git working tree；
- 正常 Git 改为 denylist 语义并支持 `git -C`、branch rename、signed/annotated tags、amend 与脚本内调用；Remote Git Rewrite 作为默认 OFF 的独立高级能力，继续保护 force/delete remote history；
- direct local-destructive Git 操作前创建有界 `refs/cwapi/safety/*` recovery refs；workspace prepare 改为 local tracking branch，并继续拒绝隐式覆盖 dirty/local/diverged history；
- `coding_exec` 保持默认 foreground `run` 兼容，并新增 `start/status/stop` persistent process 生命周期；foreground、workspace close 与 app shutdown 都有明确进程树回收；
- config schema 保持 `cwapi.config.v3`，valid 2.0.3 config 原子迁移到 2.0.4 且 Remote Git Rewrite 默认关闭；GUI、协议、文档、测试与 package metadata 同步为 2.0.4。

发行包对应开发仓库源码提交：

```text
7b5e51725f6f253f957237ce6847e7e2f32f08a1
```

## 2.0.3 — 2026-09-01

2.0.3 将 Agent 外部协议转换从 broker/runtime 核心中抽离，并建立轻量的正式协议边界：

- 新增 canonical conversation/message/tool definition/tool call/tool result/completion/error/stream chunk 类型，稳定保留 role、tool call ID 与 task/correlation metadata；
- 新增 OpenAI-compatible Adapter 与 canonical MCP bridge codec，broker 不再直接解析客户端协议；
- 新增确定性 Context Optimizer，只规范化 metadata、JSON tool result 和可证明重复的 system/developer/tool 状态，不调用第二个 AI、不删除重复 user task；
- `/v1/models` 明确报告 adapter capabilities：streaming/tools/parallel tools 支持，images/files 不支持；
- SSE 继续使用 keepalive + buffered completion chunks，不伪造 token-level streaming，同时保留未来真实流式扩展边界；
- 产品、配置、GUI 与发行源码版本统一为 `2.0.3`。

发行包对应开发仓库源码提交：

```text
8941aa5d41768993c01e7798678a485f56331691
```

## 2.0.2 — 2026-09-01

2.0.2 集中修复长任务中的状态推进、MCP 交互和防空转行为：

- 明确 Web GPT 是唯一任务控制器；CWapi 只维护 bridge/OpenAI request 状态，本地软件只执行工具；
- `agent_exchange` 对不含 tool call 的终态 response 以 `state=responses` 立即确认；含 `tool_calls` 时在同一 exchange 内继续 bounded wait，以便直接领取 tool-result follow-up request；
- `no_request` 只由真实 wait timeout 产生，仅表示等待窗口内没有新的本地 OpenAI request，不能用于推断本地 command 正在运行；
- exchange 增加 revision、changed、pending、inflight、active、idle count、wait time、last state/error 与 next action 等结构化 `activity`；
- request 交付增加 timing/state 信息，并支持可选 `metadata.task_id` / `metadata.correlation_id`；`request_id` 明确不是第三方 command session；
- assistant function arguments 与 JSON content 支持原生 JSON 输入并规范化，语义相同的 native/string tool arguments 使用 canonical fingerprint 保持幂等；
- Coding 正式 catalog 固定为 `coding_open`、`coding_exec`、`coding_status`、`coding_close`；Coding/Agent 都不提供文件或图片传输；
- 产品、配置、前端和 portable/candidate manifest 版本统一为 `2.0.2`。

发行包对应开发仓库源码提交：

```text
05068c482a617c6beb2acd5c6d2ff15cfedc7598
```

## 2.0.1 — 2026-09-01

- 收紧 Coding 永久执行策略，使 resolver 后的最终 executable/argv/CWD 在进程启动前再次校验；
- FULL 对通过永久策略的命令使用 `dangerFullAccess`，SAFE 继续使用隔离的 `workspaceWrite`；两种 profile 都不绕过破坏性 Git、凭据读取、禁止系统工具和 protected-path 规则；
- 修复 Windows SAFE 下 Go、npm、Vite/Vitest 的 Temp/cache/profile 与子进程兼容性；
- 移除 Coding/Agent 文件和图片传输，删除 `coding_attachment`、Agent inline media/attachment 临时存储与 MCP file/image content 输出；
- 增加 Windows CI 和相关 execution policy、MCP、前端 regression。

## 2.0.0 — 2026-08-31

CWapi 2.0 正式成为发行主线。

主要变化：

- 新增彼此隔离的 Coding MCP 与 Agent MCP；
- Coding 采用 durable repository workspace，可按 repository URL 恢复，并支持 exact commit guard；
- Web GPT 直接负责规划与编码，bundled Codex 仅作为 model-free `command/exec` 工具宿主；
- Agent 提供 localhost OpenAI-compatible `/v1/models` 与 `/v1/chat/completions`，把本地软件请求桥接给 Web GPT；
- Coding / Agent 使用独立 token、独立 MCP server、独立 tool catalog 与独立 OpenAI Secure MCP Tunnel 配置；
- `coding_attachment` 与 Agent inline attachment 边界收缩为 raster image；普通文本和文件不作为通用 MCP 文件资源传输；
- 配置升级为 `cwapi.config.v3 / 2.0.0`；
- portable 内置 OpenAI Codex `0.150.1`、MinGit `2.55.0.windows.4` 与 OpenAI `tunnel-client 0.0.10`；
- 发行包不包含用户数据，并通过 portable privacy gate；
- 发行仓库改为双路线：`main` 维护 2.x，`1.6.x` 保留 1.6.3 及旧版历史。

发行包对应开发仓库源码提交：

```text
d904ae80428c90717e050a151c65fa35b6b83c63
```

## 1.6.x

1.6.3 及更早发行仍保留在 `1.6.x` 分支和已有 GitHub Releases 中。2.x 与 1.6.x 不共享同一工作流或配置结构。
