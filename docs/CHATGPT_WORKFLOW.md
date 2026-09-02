# CWapi 2.0 ChatGPT Workflow

## First-contact operating manual

2.0.5 将 first-contact prompt 分成全局文件，而不是把大量经验硬编码进 Go 常量。CWapi 启动时扫描并缓存一次 `prompts/`：Coding 与 Agent 各有独立 Core/Rules，共享 Skills。MCP initialize 的 `instructions` 只拼接当前 mode 的 Core + Rules + Skill 清单（ID/name/Description），Skill body 由 `load_skill(name)` 按 Rules 需要加载。

Core 只描述通信协议与工具使用；Rules 描述 mode-specific 行为和 Skill routing；Skills 保存 coding/debugging/git/testing/release 等任务经验。修改 Rules/Skills/Core 后必须重启 CWapi。没有 workspace-specific Skill、Profile、热加载、数据库或 GUI 管理。

单个 Skill 损坏时跳过并记录 warning；对应 enabled mode 的 Core/Rules 缺失或不可读时，该 mode 启动明确失败。协议正确性、授权、request correlation 和安全边界仍由 Go code 强制执行。

## Coding GPT

Connect a custom MCP to the Coding endpoint only. The Coding surface exposes exactly five tools:

- `coding_open`
- `coding_exec`
- `coding_status`
- `coding_close`
- `load_skill`

Coding MCP has no file or image transfer tool. Read inspectable repository text through exact `coding_exec` calls.

A typical turn is:

1. `coding_open(repository_url,target_ref,expected_commit?,resume?)`；
2. inspect/read/edit/verify with exact `coding_exec(repository_url,command,argv,cwd?,timeout_seconds?)` calls；
3. inspect HEAD/dirty/divergence with `coding_status(repository_url)` when Git truth is needed；
4. `coding_close(repository_url)` when the task is genuinely finished。

The Coding GPT is the only reasoning agent. CWapi sends exact commands to the bundled private Codex app-server `command/exec` development tool; it never sends an instruction to a Codex agent. Pass arguments as an argv array, not as one shell-quoted command string. In SAFE, prefer PowerShell cmdlets and repository tools that work under Windows constrained language mode.

### Repository-scoped session lifecycle

A ChatGPT conversation ending is not observable by CWapi and therefore does not automatically close an active Coding session. Web GPT does not receive or remember a random Coding session ID. The public stable identity is the repository URL.

CWapi still creates a unique internal session ID and keeps `canonical repository -> active internal session` ownership. That internal identity remains responsible for concurrent-operation exclusion, cancellation, closing, stale-operation protection and logging; removing the public ID does not remove the internal lifecycle generation.

At the start of a Coding conversation/task, call `coding_open`. If the repository has no active session, CWapi prepares or resumes the durable workspace and creates an internal session. If the same repository already has an active compatible session, call `coding_open(..., resume=true)`; CWapi reuses that internal session and returns `resumed=true` instead of requiring the caller to recover an old ID. If a command is already running, the returned state may be `busy`.

`resume=false` keeps repository ownership protection: an already-active repository returns `CODING_WORKSPACE_BUSY`. A workspace still opening, a session closing, or an incompatible ref/expected commit is not silently adopted.

Continue all later Coding calls with the same `repository_url`. Do not open a second workspace for the same repository.

### Files and images

CWapi 2.0.5 does not transfer files or images through Coding MCP. There is no attachment tool and the MCP layer does not emit `ImageContent` or `EmbeddedResource`.

Read source, Markdown, JSON, logs, configuration and other text directly through `coding_exec`, using exact tools such as `rg`, `git show`, PowerShell text reads or repository-specific commands. Prefer bounded, relevant output rather than moving whole files when only part of a file is needed.

### Efficiency and safety

Inspect before editing. When several reads/searches are independent, prefer one information-rich command or a small number of grouped commands instead of many tiny round trips. Do not repeatedly re-read unchanged files without new evidence. After edits, verify with the narrowest useful test first, then broaden validation when needed. Use `coding_status` when Git/workspace truth is actually needed rather than after every small action.

Use SAFE when the task should remain confined to the owned workspace. SAFE has workspace-lifetime caches but a synthetic profile and isolated host credentials/configuration. Select FULL only when the user intends the Agent to use the current Windows user's normal development environment; FULL permits normal Git/GitHub CLI, SDK, package-manager, hooks/signing and process-control behavior, including commands invoked from PowerShell/cmd/build scripts. CWapi does not parse arbitrary shell text as a security boundary in FULL.

Network access is selected independently in either profile. Remote Git Rewrite is a separate advanced capability and defaults OFF; enable it only for an intended force/delete remote update. Unsafe push transports, receive-pack injection, CWapi recovery-ref publication, protected internal paths and automatic elevation remain denied. Potentially destructive direct local Git operations create recovery refs when possible.

For ordinary bounded work, omit `action` and use foreground `run`. For a development server, watcher, GUI or browser-auth flow, use `action=start`, retain `process_id`, inspect with bounded `action=status`, and finish with `action=stop`. A terminal process state ends polling. Closing the Coding session also stops that workspace's persistent processes.

Call `coding_close(repository_url)` when the task is genuinely finished. Closing releases only the repository active session owner. It does not reset or clean Git, delete uncommitted changes, or delete the durable workspace. If a conversation disappears before close, a later conversation uses the same repository with `coding_open(..., resume=true)`.

## Agent GPT

Connect a separate custom MCP to the Agent endpoint. The Agent surface exposes `agent_open`、`agent_exchange`、`agent_close` and shared `load_skill`。

1. `agent_open()` opens or resumes the logical bridge；
2. use `load_skill(name)` only when Agent Rules require a task Skill；
3. call `agent_exchange(capacity=4)` and process every returned request by exact `request_id`；
4. return structured responses/events and optional progress；
5. continue until structured completion, or `agent_close()` when the bridge itself should detach。

Web GPT is the sole planner. CWapi owns heartbeat, broker/request lifecycle and protocol conversion; local software executes tools. Natural-language silence is not liveness, progress is not completion, and completion is never inferred from words in assistant text.

Returned requests expose a compatibility `state=claimed` plus explicit `lifecycle_state`. A request may move through `QUEUED / CLAIMED / RUNNING / WAITING_TOOL / COMPLETED / FAILED_RETRYABLE / FAILED_FINAL`. A malformed tool call produces a structured retryable error and same-ID redelivery rather than stranding the request. Correct the call and continue.

A `delivery` greater than 1 is the same request. On bridge interruption, preserve completed steps and use `previous_state/resume_reason/last_activity`; do not restart work merely because a new internal bridge generation was created. `agent_close` detaches the bridge but does not discard active requests.

Tool-call arguments may be native JSON or OpenAI JSON string. For streamed arguments, CWapi concatenates all fragments before parsing once. Prefer several bounded tool calls over giant nested PowerShell/HTML/JS payloads because small calls are easier to retry and less fragile on Windows.

When a tool itself fails, return that failure as the normal tool result so Web GPT can decide the next action. A tool error is not automatically a final Agent failure. The local OpenAI client creates the next request containing `tool_result`; that request is tagged as such by CWapi.

Every exchange returns structured activity including revision, pending/inflight compatibility fields, queued/active request counts, heartbeat/progress observations and bounded-wait state. `no_request` means only that no new OpenAI request arrived during that wait. It does not prove a third-party process is still running.

Local clients may attach top-level `metadata`; `task_id` and `correlation_id` remain optional stable client-supplied identities. CWapi does not turn per-request IDs into fake command sessions. Independent function calls may still be returned together as standard parallel `tool_calls` when their arguments are already known.

### Agent files and media

Agent accepts text and tool JSON only. A top-level `attachments` field is rejected with `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`; any non-text message content part, including `image_url`, is rejected with `AGENT_MEDIA_INPUT_UNSUPPORTED` before broker admission. `agent_exchange` emits no MCP file or image content. There is no reverse path that writes a ChatGPT conversation upload into local software.
