# CWapi 2.0 Security

## Trust boundaries

- MCP and Provider listeners bind only `127.0.0.1`；
- Coding/Agent routes use independent high-entropy bearer tokens；
- Provider uses an independent bearer API key with constant-time comparison；
- public HTTPS exposure, if needed by ChatGPT, is an explicit external deployment boundary；
- Coding creates an isolated empty CODEX_HOME per command and never reads native Codex identity/credentials。

## Coding

SAFE maps to upstream Codex `workspaceWrite`. Codex protects `.git` within writable roots, so SAFE is intended for read/edit/test. FULL maps to upstream `dangerFullAccess` and is required for authorized Git metadata changes such as commit/push.

CWapi validates exact command/argv/CWD, resolves the final executable, and invokes only the private app-server `command/exec` development operation. It never calls Codex thread/turn/auth/account/model APIs. Command children receive a bounded environment without OpenAI/Codex/GitHub token variables. Git may still use the current Windows user's existing Git credential helper.

SAFE/FULL are the complete CWapi 2.0 Coding access profiles. CWapi does not issue an additional reusable elevation credential.

The Coding MCP public surface uses `repository_url` as stable identity and does not expose the random internal session ID. CWapi still keeps that internal ID and exact repository ownership mapping for cancellation, close races and stale-operation protection.

`coding_attachment` is limited to raster images and returns only native MCP `ImageContent`. Non-image workspace paths are rejected with `CODING_ATTACHMENT_IMAGE_ONLY`; ordinary files are not exposed as MCP `EmbeddedResource`. Workspace text needed by Web GPT is read through bounded `coding_exec` output.

Workspace maintenance resolves targets under the managed root and is available only from the local Desktop confirmation flow.

## Agent

- HTTP request body hard limit 24 MiB, including inline image base64 overhead；
- image limits: 8 images, 8 MiB per image, 16 MiB total and 2048 px per image side；
- active queue hard limit 16；
- internal bridge IDs remain random and generation-scoped; public `request_id` remains random and exact-correlated；
- stale lease, timeout, disconnect and close terminalize requests once；
- malformed messages/tools/responses are rejected before delivery；
- only standard `image_url` data-URI raster images enter the attachment path；top-level generic file attachments return `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`；
- inline image bytes are validated, stripped from broker JSON and exposed only as request-scoped native MCP `ImageContent`；
- temporary image files use sanitized names, integrity metadata and a dedicated `CWapi-data/temp/agent-attachments` root；all terminal paths, broker shutdown and startup remove them；
- Agent does not emit MCP `EmbeddedResource` for text, PDF, archive, document or other ordinary files；
- image flow is only from local software to Web GPT；ChatGPT conversation uploads are not imported into local software；
- normal observability stores metadata only, not full conversation payloads。

Server Instructions may guide workflow and efficiency, but they are not a security boundary. Authorization, input validation, request correlation, duplicate handling and terminal-state rules remain enforced by Go code.

## Config and package

Config contains local bearer secrets and must remain inside adjacent `CWapi-data`. It is never copied into candidate stage. Runtime archives/executables are SHA-256 pinned; package audit scans names and content for user data, credentials, logs, machine identity and absolute build paths.

Tunnel Runtime API keys are stored in Windows Credential Manager and injected only into their matching tunnel-client child process environment. They are not written into `cwapi.json` or generated Tunnel profiles.

## Reporting

Security issues should include affected version/commit, exact local route or component, reproduction, impact, and whether the issue requires SAFE or FULL. Do not include real tokens or credentials in reports.
