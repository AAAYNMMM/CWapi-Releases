# CWapi 2.0 ChatGPT Workflow

## First-contact operating manual

CWapi exposes a concise operating manual through each MCP server's `instructions` field during MCP initialization. The goal is that, when Web GPT first contacts the Coding or Agent MCP in a conversation/task, it immediately learns the correct workflow and the main efficiency rules, then keeps using those rules for the continuous task without requiring the same guidance to be repeated on every tool call.

The server instructions are intentionally concise. They contain cross-tool workflow, lifecycle, recovery, and round-trip reduction guidance. Individual tool descriptions remain focused on the local contract of that tool. Protocol correctness, validation, authorization boundaries, request correlation, and other safety-critical behavior remain enforced in Go code rather than relying on instructions.

A client may reconnect at the transport layer and initialize the MCP again; correctness must not depend on a particular TCP/HTTP connection remaining alive. The instructions describe how to use CWapi for the current conversation/task, while durable Coding workspace state and Agent bridge/request state remain owned by CWapi.

## Coding GPT

Connect a custom MCP to the Coding endpoint only. The Coding surface exposes exactly four tools:

- `coding_open`
- `coding_exec`
- `coding_status`
- `coding_close`

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

CWapi 2.0.3 does not transfer files or images through Coding MCP. There is no attachment tool and the MCP layer does not emit `ImageContent` or `EmbeddedResource`.

Read source, Markdown, JSON, logs, configuration and other text directly through `coding_exec`, using exact tools such as `rg`, `git show`, PowerShell text reads or repository-specific commands. Prefer bounded, relevant output rather than moving whole files when only part of a file is needed.

### Efficiency and safety

Inspect before editing. When several reads/searches are independent, prefer one information-rich command or a small number of grouped commands instead of many tiny round trips. Do not repeatedly re-read unchanged files without new evidence. After edits, verify with the narrowest useful test first, then broaden validation when needed. Use `coding_status` when Git/workspace truth is actually needed rather than after every small action.

Use SAFE for normal read/edit/test work. If the requested task includes add/commit/push, the local operator must select FULL before that direct Git metadata operation; wrappers and unrelated commands remain `workspaceWrite`. Network access is selected independently and must be explicitly enabled for FULL push. Destructive Git operations, credential extraction, remote-ref deletion and force-push are not available through Coding.

Call `coding_close(repository_url)` when the task is genuinely finished. Closing releases only the repository active session owner. It does not reset or clean Git, delete uncommitted changes, or delete the durable workspace. If a conversation disappears before close, a later conversation uses the same repository with `coding_open(..., resume=true)`.

## Agent GPT

Connect a separate custom MCP to the Agent endpoint only:

1. `agent_open()` to open or resume the single active bridge；
2. call `agent_exchange(capacity=4)`; CWapi automatically uses the current internal bridge；
3. process every returned request independently, using `request_id` as the correlation key；
4. call one `agent_exchange` with all completed `responses`; non-tool terminal responses are acknowledged immediately as `state=responses`, while `tool_calls` keep the same exchange in a bounded wait for the tool-result batch；
5. continue the exchange loop or `agent_close()`。

Web GPT is the sole planner and decision-maker. CWapi is a request broker and the local software is a tool executor; neither continues planning after Web GPT stops. Maintain a concrete `pending -> running -> completed/failed -> verified` checklist in the task context. A process exit or explicit PASS/FAIL is a terminal transition: stop polling that command session and immediately select the next pending verification or closeout step.

Web GPT never receives or stores `bridge_id`. CWapi keeps an internal bridge generation and uses it for request ownership, receipts, lease handling and stale-operation protection. Open the bridge once for the continuous task. Do not close/reopen merely because one exchange returns `no_request`. Use the full returned batch; independent requests may be reasoned about together when practical, but correctness must not depend on parallel scheduling. `request_id` remains required because multiple independent OpenAI transactions can be in flight at once; it is never a third-party command/session ID.

Every exchange returns an `activity` snapshot with a monotonic broker revision, whether it changed since the preceding returned exchange, pending/inflight counts, consecutive unchanged idle count, wait duration, last broker state and a next-action hint. Progress reporting should use this structured request-plane state plus concrete third-party process/artifact evidence, not language-model memory or message arrival timing.

For one OpenAI-compatible request, when several third-party function calls are independent and all required arguments are already known, return them together in the same assistant `tool_calls` array. Do not split independent calls across extra model turns merely to inspect intermediate results. Keep calls sequential only when a later call genuinely depends on an earlier tool result. This reduces repeated full-prompt/tool-schema turns in OpenAI-compatible agent loops without changing the protocol.

A `delivery` greater than 1 is a retry of the same request, not new work. Re-send the same response safely when an exchange result was lost; never send a different response for an already completed request ID. A rejected item remains available for correction without rolling back successful siblings. Tool calls are returned to the third-party software; the Agent GPT does not execute that software's local tool itself.

Function `arguments` may be supplied either as the OpenAI JSON string or directly as a native JSON object. JSON response `content` may also be native. CWapi canonicalizes these to the OpenAI-compatible string form before returning them locally, which avoids fragile JSON-inside-JSON and Windows backslash escaping. Equivalent native/string tool arguments share a canonical idempotency fingerprint.

Local clients may attach standard top-level `metadata`. Stable string `task_id` and `correlation_id` values are surfaced separately on each returned request while the whole bounded metadata object remains in the request payload. This is opt-in context supplied by the software that actually owns the task; CWapi never invents a stable task ID from unrelated request IDs.

`no_request` has one narrow meaning: no new local OpenAI request arrived before the bounded exchange wait expired. It is not evidence that a local process is still running and is not, by itself, a reason to call `agent_exchange` again. Continue waiting only when a separately known active process, external condition, or user-requested monitor exists. After two unchanged idle exchanges, reassess the task checklist, actual process status and expected artifacts before any further wait. If an expected release artifact already exists but status delivery was ambiguous, verify its manifest/hash/content and advance instead of polling a terminal session.

### Agent files and media

Agent accepts text and tool JSON only. A top-level `attachments` field is rejected with `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`; any non-text message content part, including `image_url`, is rejected with `AGENT_MEDIA_INPUT_UNSUPPORTED` before broker admission. `agent_exchange` emits no MCP file or image content.

There is no reverse path that writes a ChatGPT conversation upload into local software.
