# CWapi 2.0 Operations

## Start

Extract the portable to any user-writable directory and run `CWapi.exe`. No Go, Node, Git or Wails installation is required. The first launch creates `CWapi-data/config/cwapi.json` and starts the loopback MCP listener plus the Agent Provider when enabled. Each bundled OpenAI Secure MCP Tunnel starts only after its own configuration is complete.

Coding uses the bundled Codex app-server only for model-free `command/exec` and creates a private empty CODEX_HOME per command. No Codex login is needed. Private Git repositories still use the current Windows user's existing Git/GitHub credential setup.

## Connect

The GUI's local endpoints are for software running on the same Windows machine:

```text
Coding: http://127.0.0.1:<port>/mcp/coding/<token>
Agent:  http://127.0.0.1:<port>/mcp/agent/<token>
```

Do not paste these `127.0.0.1` URLs into ChatGPT's **Server URL** connection mode. ChatGPT cannot reach the local machine directly. Use the **Tunnel** connection mode for each ChatGPT MCP app.

Remote ChatGPT custom MCP requires an operator-selected HTTPS exposure layer. Keep Coding and Agent URLs separate; never reuse their tokens or tunnel credentials.

For the bundled OpenAI Secure MCP Tunnel, open the matching Coding or Agent panel and enter only:

```text
Tunnel ID:       tunnel_...
Runtime API key: <your Tunnel Runtime API key>
```

CWapi writes the matching ID to the v3 config and stores the matching Runtime API key in a separate Windows Credential Manager entry. It generates profiles at `CWapi-data/tunnel/coding/openai-tunnel.yaml` and `CWapi-data/tunnel/agent/openai-tunnel.yaml`; each profile contains an environment reference, not the key. Coding's `main` channel forwards Coding MCP only, and Agent's `main` channel forwards Agent MCP only.

A tunnel-client that fails its initial child-process launch or exits unexpectedly is retried locally up to three times with exponential backoff. The GUI shows `正在重连`; disconnecting or reconfiguring cancels a pending restart.

If both Coding and Agent will be connected as separate ChatGPT apps, create two OpenAI Secure MCP Tunnels and configure one in each panel. Do not put the Agent tunnel ID or key into the Coding panel.

Configure local software with:

```text
Base URL: http://127.0.0.1:<agent-port>/v1
API key:  <Agent API key from GUI>
Model:    cwapi-web-gpt
```

## Images and files

CWapi 2.0 only transports raster images to Web GPT. It does not provide a general-purpose file transfer channel.

For Coding, call `coding_attachment` only for repository-relative image paths such as PNG/JPEG/GIF/WebP. Any non-image path is rejected with `CODING_ATTACHMENT_IMAGE_ONLY`. Source, Markdown, JSON, logs and other readable text should be inspected through `coding_exec`; PDF, ZIP, DOCX and other ordinary files are not instantiated as ChatGPT MCP file resources.

For Agent, local software may send a standard Chat Completions `image_url` data URI. CWapi validates and temporarily stores the raster image, removes the raw bytes from broker JSON, and returns native MCP `ImageContent` through `agent_exchange`. A top-level generic `attachments` file array is rejected with `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`.

The direction remains local-to-Web-GPT only. A file or image uploaded in the ChatGPT conversation is not written into the local workspace or Agent software.

Coding image limits remain 16 images, 32 MiB each, 64 MiB total and 4096 px per image side. Agent image limits are 8 images, 8 MiB each, 16 MiB total and 2048 px per image side. SVG is unsupported. Agent temporary images are removed automatically on every terminal request path, broker shutdown and the next startup.

## Access profile

- SAFE: ordinary source inspection, edits and tests；
- FULL: authorized operations that need `.git` writes or broader host access。

SAFE/FULL may be switched while Coding sessions remain open. An already running coding_exec keeps the sandbox selected when that command started; the next coding_exec uses the newly selected profile. Other service-restart configuration mutations remain blocked while Coding work is active.

## Workspace recovery

An interrupted Coding task leaves the durable workspace intact. A ChatGPT conversation ending does not automatically close the CWapi Coding session. If the same repository already has an active session, a new conversation should call compatible `coding_open(..., resume=true)`; CWapi reuses the active internal session without preparing a second workspace or requiring a public session ID.

`resume=false` against an active repository still returns `CODING_WORKSPACE_BUSY`. A clean workspace with metadata interrupted during write is repaired by the next non-resume `coding_open`; resume still refuses to guess a corrupt context. Use the Desktop maintenance overlay only when the repository should be deleted/rebuilt; this loses uncommitted local work in that selected workspace.

## Move or upgrade

Moving the entire extracted directory moves its adjacent `CWapi-data`. Moving only the clean program/runtime creates a fresh data root in the new location.

Before replacing a version, close active sessions and back up any unpushed workspace changes. Do not copy `CWapi-data` into a release ZIP.

## Failure triage

- The GUI shows only the current bounded error code. It deliberately omits raw paths, credentials and log history；
- MCP not reachable: confirm app runtime state, port and exact token URL；
- OpenAI Tunnel blocked: confirm Tunnel ID and Runtime API key; verify the bundled tunnel-client is present in `runtime/tunnel/current`；
- OpenAI Tunnel failed after automatic retries: inspect the GUI state and reconnect; do not put the Runtime API key into `cwapi.json` or an environment file；
- Codex unavailable: verify the bundled runtime lock/hash and Windows sandbox readiness；
- private clone/push fails: verify current-user GitHub credentials；
- `CODING_ATTACHMENT_IMAGE_ONLY`: the requested Coding attachment is not a supported raster image; read text with `coding_exec` instead；
- `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`: local Agent software attempted generic file transfer; only inline raster images are supported；
- Agent 503: open Agent MCP bridge；
- Agent 429: wait for pending/claimed work to finish；
- Agent 504: Web GPT did not answer before request timeout；
- config mutation rejected: close active Coding/Agent work first。
