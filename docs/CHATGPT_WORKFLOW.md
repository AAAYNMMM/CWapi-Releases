# CWapi 2.0 ChatGPT Workflow

## First-contact operating manual

CWapi exposes a concise operating manual through each MCP server's `instructions` field during MCP initialization. The goal is that, when Web GPT first contacts the Coding or Agent MCP in a conversation/task, it immediately learns the correct workflow and the main efficiency rules, then keeps using those rules for the continuous task without requiring the same guidance to be repeated on every tool call.

The server instructions are intentionally concise. They contain cross-tool workflow, lifecycle, recovery, and round-trip reduction guidance. Individual tool descriptions remain focused on the local contract of that tool. Protocol correctness, validation, authorization boundaries, request correlation, and other safety-critical behavior remain enforced in Go code rather than relying on instructions.

A client may reconnect at the transport layer and initialize the MCP again; correctness must not depend on a particular TCP/HTTP connection remaining alive. The instructions describe how to use CWapi for the current conversation/task, while durable Coding workspace state and Agent bridge/request state remain owned by CWapi.

## Coding GPT

Connect a custom MCP to the Coding endpoint only. The Coding surface exposes exactly five tools:

- `coding_open`
- `coding_exec`
- `coding_status`
- `coding_attachment`
- `coding_close`

`coding_attachment` is **image-only**. It exists because native MCP `ImageContent` has been verified to work on the tested ChatGPT surface. It is not a generic file upload/export tool.

A typical turn is:

1. `coding_open(repository_url,target_ref,expected_commit?,resume?)`；
2. inspect/read/edit/verify with exact `coding_exec(repository_url,command,argv,cwd?,timeout_seconds?)` calls；
3. use `coding_attachment(repository_url,paths)` only when a raster image from the workspace must be shown to Web GPT；
4. inspect HEAD/dirty/divergence with `coding_status(repository_url)` when Git truth is needed；
5. `coding_close(repository_url)` when the task is genuinely finished。

The Coding GPT is the only reasoning agent. CWapi sends exact commands to the bundled private Codex app-server `command/exec` development tool; it never sends an instruction to a Codex agent. Pass arguments as an argv array, not as one shell-quoted command string. In SAFE, prefer PowerShell cmdlets and repository tools that work under Windows constrained language mode.

### Repository-scoped session lifecycle

A ChatGPT conversation ending is not observable by CWapi and therefore does not automatically close an active Coding session. Web GPT does not receive or remember a random Coding session ID. The public stable identity is the repository URL.

CWapi still creates a unique internal session ID and keeps `canonical repository -> active internal session` ownership. That internal identity remains responsible for concurrent-operation exclusion, cancellation, closing, stale-operation protection and logging; removing the public ID does not remove the internal lifecycle generation.

At the start of a Coding conversation/task, call `coding_open`. If the repository has no active session, CWapi prepares or resumes the durable workspace and creates an internal session. If the same repository already has an active compatible session, call `coding_open(..., resume=true)`; CWapi reuses that internal session and returns `resumed=true` instead of requiring the caller to recover an old ID. If a command is already running, the returned state may be `busy`.

`resume=false` keeps repository ownership protection: an already-active repository returns `CODING_WORKSPACE_BUSY`. A workspace still opening, a session closing, or an incompatible ref/expected commit is not silently adopted.

Continue all later Coding calls with the same `repository_url`. Do not open a second workspace for the same repository.

### Images versus files

Live ChatGPT testing showed a clear split:

- raster image through MCP `ImageContent`: works on the tested surface；
- text through MCP `EmbeddedResource`: failed；
- binary ZIP through MCP `EmbeddedResource.Blob`: failed。

CWapi therefore keeps only the path that has been verified useful: **images may travel as `ImageContent`; ordinary files do not travel as MCP resources.**

`coding_attachment` accepts workspace image paths only. If any requested item resolves to a non-image, the call is rejected with `CODING_ATTACHMENT_IMAGE_ONLY`. CWapi's MCP content layer no longer emits `EmbeddedResource` for text or binary files.

Read source, Markdown, JSON, logs, configuration and other text directly through `coding_exec`, using exact tools such as `rg`, `git show`, PowerShell text reads or repository-specific commands. Prefer bounded, relevant output rather than moving whole files when only part of a file is needed.

### Efficiency and safety

Inspect before editing. When several reads/searches are independent, prefer one information-rich command or a small number of grouped commands instead of many tiny round trips. Do not repeatedly re-read unchanged files without new evidence. After edits, verify with the narrowest useful test first, then broaden validation when needed. Use `coding_status` when Git/workspace truth is actually needed rather than after every small action.

Use SAFE for normal read/edit/test work. If the requested task includes commit/push, the local operator must select FULL before the relevant Git metadata operation. Never delete remote refs or force-push unless the user explicitly requests it.

Call `coding_close(repository_url)` when the task is genuinely finished. Closing releases only the repository active session owner. It does not reset or clean Git, delete uncommitted changes, or delete the durable workspace. If a conversation disappears before close, a later conversation uses the same repository with `coding_open(..., resume=true)`.

## Agent GPT

Connect a separate custom MCP to the Agent endpoint only:

1. `agent_open()` to open or resume the single active bridge；
2. call `agent_exchange(capacity=4)`; CWapi automatically uses the current internal bridge；
3. process every returned request independently, using `request_id` as the correlation key；
4. call one `agent_exchange` with all completed `responses`; the same call waits for and returns the next batch；
5. continue the exchange loop or `agent_close()`。

Web GPT never receives or stores `bridge_id`. CWapi keeps an internal bridge generation and uses it for request ownership, receipts, lease handling and stale-operation protection. Open the bridge once for the continuous task. Do not close/reopen merely because one exchange returns `no_request`. Use the full returned batch; independent requests may be reasoned about together when practical, but correctness must not depend on parallel scheduling. `request_id` remains required because multiple independent requests can be in flight at once.

For one OpenAI-compatible request, when several third-party function calls are independent and all required arguments are already known, return them together in the same assistant `tool_calls` array. Do not split independent calls across extra model turns merely to inspect intermediate results. Keep calls sequential only when a later call genuinely depends on an earlier tool result. This reduces repeated full-prompt/tool-schema turns in OpenAI-compatible agent loops without changing the protocol.

A `delivery` greater than 1 is a retry of the same request, not new work. Re-send the same response safely when an exchange result was lost; never send a different response for an already completed request ID. A rejected item remains available for correction without rolling back successful siblings. Tool calls are returned to the third-party software; the Agent GPT does not execute that software's local tool itself.

### Agent images

Local software may include inline raster images only through the standard Chat Completions `image_url` data-URI form. CWapi validates the image, removes the raw bytes from the request JSON before broker admission, stores the image only for the request lifetime, and returns metadata plus native MCP `ImageContent` from `agent_exchange`.

CWapi no longer accepts the top-level generic `attachments` extension for text/PDF/archive/document files. Such requests return `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`. MCP `EmbeddedResource` is not used for Agent file delivery.

On redelivery, associate an image with the same `request_id`; it is the same temporary request data, not a new upload. There is no reverse path that writes a ChatGPT conversation upload into local software.

## Image limits

Agent allows at most 8 inline images, 8 MiB per image and 16 MiB per request; images are limited to 2048 px per side. PNG, JPEG, GIF and WebP are validated as raster images. SVG is deliberately rejected with `ATTACHMENT_IMAGE_FORMAT_UNSUPPORTED`. Agent temporary image files are removed on completion, timeout, client disconnect, bridge close, broker shutdown and the next CWapi startup.

Coding keeps its existing image bounds for workspace image export. Ordinary file transfer is not part of either surface.

## OpenAI Secure MCP Tunnel

The portable includes the official OpenAI `tunnel-client`. Configure the Coding and Agent panels separately with the matching Tunnel ID and Runtime API key from the OpenAI Tunnel service. Each client forwards its own `main` channel to its local MCP endpoint. CWapi stores only the non-secret IDs in config and keeps each Runtime API key in a separate Windows Credential Manager entry.

The implementation follows the setup shape documented by [chatgpt-local-coder](https://github.com/hoangcoderr/chatgpt-local-coder): one generated profile per tunnel, `api_key: env:CONTROL_PLANE_API_KEY`, and one local MCP `server_urls.main` binding per profile. Do not copy either key into `.env`, `cwapi.json`, a profile, or a command line.

## Exposure boundary

CWapi still binds its MCP listener to localhost. The local `127.0.0.1` URLs are not valid ChatGPT Server URL inputs because ChatGPT cannot reach the developer machine directly. Select **Tunnel** when creating each app: the Coding tunnel is the Coding exposure path, and the Agent tunnel is the Agent exposure path.

If both apps are needed, create two independent OpenAI Secure MCP Tunnels. Keep the two bearer tokens, Tunnel IDs and Runtime API keys separate, and expose only the endpoint needed by that GPT.
