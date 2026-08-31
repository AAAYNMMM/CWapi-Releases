# CWapi 2.0 Troubleshooting

[English](TROUBLESHOOTING.md) | [简体中文](TROUBLESHOOTING.zh-CN.md)

Use this guide from the symptom you can actually see. Check [Getting Started](GETTING_STARTED.md) first if the setup has never worked at all.

## ChatGPT does not show the MCP tools

**Symptom**

The Coding app does not expose `coding_open` / `coding_exec` / `coding_status` / `coding_attachment` / `coding_close`, or the Agent app does not expose `agent_open` / `agent_exchange` / `agent_close`.

**Likely cause**

The wrong MCP app/Tunnel is connected, the Tunnel is not running, or the ChatGPT workspace has not enabled the required custom MCP capability.

**What to check**

- Coding and Agent use separate Tunnels.
- CWapi shows the corresponding Tunnel as connected/running rather than reconnecting.
- The ChatGPT app is connected through Secure MCP Tunnel, not a direct `127.0.0.1` Server URL.
- The discovered tool list matches the exact public tools above.

**How to fix**

Reconnect the correct Tunnel/app. If discovery still fails, restart only the affected Tunnel connection and verify its Tunnel ID and Runtime API key before touching repository state.

## Tunnel cannot connect

**Symptom**

The Coding or Agent Tunnel never reaches a connected state.

**Likely cause**

Invalid Tunnel ID, invalid/missing Runtime API key, blocked outbound connectivity, or a missing/broken bundled tunnel runtime.

**What to check**

- Tunnel ID belongs to the intended Coding or Agent Tunnel.
- Runtime API key is the one authorized for that Tunnel.
- `runtime/tunnel/current/tunnel-client.exe` exists in the portable.
- Local firewall/proxy policy allows the tunnel client to make its outbound connection.

**How to fix**

Re-enter the matching Tunnel ID and Runtime API key in the correct CWapi panel. Do not paste the Runtime API key into `cwapi.json`; CWapi stores it in Windows Credential Manager.

## Tunnel keeps reconnecting

**Symptom**

The GUI repeatedly shows reconnecting after an initial start or after an unexpected tunnel-client exit.

**Likely cause**

The tunnel process starts but cannot remain authenticated/connected, or the local target/profile is invalid.

**What to check**

- The reconnecting surface is the one you intended to configure.
- Coding Tunnel points only to Coding MCP; Agent Tunnel points only to Agent MCP.
- The saved Tunnel ID is correct.
- The corresponding Runtime API key still exists in Windows Credential Manager.

**How to fix**

Correct the affected Tunnel configuration and reconnect it. Do not copy one surface's Tunnel settings into the other.

## Runtime API key is rejected

**Symptom**

The tunnel client fails authentication even though the Tunnel ID looks correct.

**Likely cause**

Wrong, expired, revoked, or insufficiently authorized Runtime API key.

**What to check**

Coding key target:

```text
CWapi/2.0/OpenAI/Tunnel/APIKey
```

Agent key target:

```text
CWapi/2.0/OpenAI/Tunnel/Agent/APIKey
```

**How to fix**

Create/use a valid Runtime API key for the intended Tunnel and save it through the matching CWapi Tunnel panel so it replaces the correct Credential Manager entry.

## `CODING_WORKSPACE_BUSY`

**Symptom**

`coding_open(..., resume=false)` returns `CODING_WORKSPACE_BUSY`.

**Likely cause**

That canonical repository already has an active Coding session.

**What to check**

Call `coding_status(repository_url)` and confirm that this is the repository/task you intend to continue.

**How to fix**

If continuing the same compatible task, use:

```text
coding_open(..., resume=true)
```

If the old task is genuinely finished, close it first with `coding_close`. Do not work around ownership by changing URL spelling.

## Codex runtime unavailable

**Symptom**

Coding opens but `coding_exec` cannot start because the bundled Codex command toolhost/runtime is missing or unavailable.

**Likely cause**

The portable was only partially copied/extracted, runtime files were removed, or security software blocked the bundled executable.

**What to check**

- You extracted the complete release, not only `CWapi.exe`.
- The pinned Codex runtime exists under `runtime/codex/current`.
- Security software did not quarantine it.

**How to fix**

Re-extract the complete official `CWapi-v2.0.0.zip` into a clean user-writable directory. Do not replace the bundled runtime with an arbitrary Codex installation.

## Private Git clone/fetch/push fails

**Symptom**

Public repositories work, but a private repository returns Git authentication errors.

**Likely cause**

The current Windows user does not have working Git/GitHub credentials for that repository.

**What to check**

Verify the same Windows user can authenticate to the repository with normal Git tooling outside CWapi.

**How to fix**

Repair the Windows user's Git/GitHub credential setup. CWapi does not use a Codex login as Git authentication. Do not put GitHub tokens in prompts or repository files.

## Commit or push fails in SAFE

**Symptom**

Normal edits/tests work, but `git commit`, `git push`, or another Git metadata operation fails under SAFE.

**Likely cause**

SAFE intentionally protects `.git` metadata and broader host access.

**What to check**

Confirm the user explicitly authorized the Git write and inspect the intended staged files first.

**How to fix**

Switch Coding to `FULL`, then run the exact authorized Git command. Switch back to SAFE afterward for normal work. A command already running keeps the profile it started with.

## A new ChatGPT conversation cannot resume the workspace

**Symptom**

`coding_open(..., resume=true)` does not resume the expected workspace.

**Likely cause**

The repository URL/target ref/expected commit is incompatible with the existing workspace metadata, the workspace was moved without `CWapi-data`, or the previous active state was closed and the requested resume contract no longer matches.

**What to check**

- Use the same canonical repository URL.
- Use the same compatible `target_ref`.
- If `expected_commit` is supplied, it matches the stored resolved commit.
- The current CWapi directory contains the original `CWapi-data/workspaces` tree.

**How to fix**

Resume using matching parameters. If you intentionally want a new baseline, first preserve any important local work, then rebuild the workspace through CWapi's maintenance flow rather than deleting files blindly.

## `CODING_ATTACHMENT_IMAGE_ONLY`

**Symptom**

`coding_attachment` rejects a requested path.

**Likely cause**

At least one requested item is not a supported raster image or violates image limits.

**What to check**

Coding limits are 16 images, 32 MiB each, 64 MiB per batch, 4096 px per side, raster only; SVG is unsupported.

**How to fix**

Use `coding_attachment` only for validated raster images in the workspace. Read source, Markdown, JSON, logs and other text through `coding_exec`.

## `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`

**Symptom**

A local Agent request is rejected with `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`.

**Likely cause**

The client sent CWapi's unsupported generic top-level `attachments` file extension.

**What to check**

Inspect the client request shape. Agent supports raster images only through standard Chat Completions `image_url` data URIs.

**How to fix**

Remove generic file attachments. Send supported inline raster images with `image_url`, or let the local application provide text/files through its own messages/tools workflow.

## Agent Provider returns 401 `invalid_api_key`

**Symptom**

`GET /v1/models` or `POST /v1/chat/completions` returns HTTP 401.

**Likely cause**

The local client is missing the Agent Provider API key or is using an old/wrong key.

**What to check**

The API key is the Agent key shown by CWapi and stored in `CWapi-data/config/cwapi.json`. It is not the Tunnel Runtime API key.

**How to fix**

Update the local client's Bearer/API-key field with CWapi's current Agent Provider API key.

## Agent Provider returns 429 `AGENT_BUSY`

**Symptom**

The local client receives HTTP 429 `AGENT_BUSY`.

**Likely cause**

The bounded Agent broker is full/busy. Defaults are up to 4 in-flight requests and a queue of 16.

**What to check**

Confirm the ChatGPT Agent bridge is actively running `agent_exchange` and previous requests are being completed.

**How to fix**

Reduce local request concurrency and let pending/claimed requests finish. Do not solve queue pressure by opening multiple competing Agent bridges.

## Agent Provider returns 503

**Symptom**

The local client receives HTTP 503, commonly `AGENT_BRIDGE_UNAVAILABLE` / unavailable.

**Likely cause**

There is no usable active Agent MCP bridge, the Agent Tunnel/app is disconnected, or the exchange loop is not running.

**What to check**

- Agent Tunnel is connected to Agent MCP.
- ChatGPT exposes `agent_open`, `agent_exchange`, `agent_close`.
- `agent_open()` has opened/resumed the bridge.
- The conversation continues calling `agent_exchange` during the continuous task.

**How to fix**

Restore the Agent Tunnel/app connection, call `agent_open`, and keep the exchange loop active.

## Agent Provider returns 504 `AGENT_REQUEST_TIMEOUT`

**Symptom**

A local Chat Completions request eventually returns HTTP 504.

**Likely cause**

The request did not receive a completed Web GPT response before the current default 180-second request timeout.

**What to check**

Confirm `agent_exchange` actually received the request and that the response was returned under the exact `request_id`.

**How to fix**

Keep the Agent exchange loop active and finish each request within the deadline. Reduce oversized/overly complex client turns when practical.

## Local software cannot find model `cwapi-web-gpt`

**Symptom**

The client reports that the model is missing, or its model picker is empty.

**Likely cause**

The Base URL is wrong, the client does not use the custom provider you configured, or authentication fails before it can read `/v1/models`.

**What to check**

Use:

```text
Base URL: http://127.0.0.1:<agent-port>/v1
Model:    cwapi-web-gpt
```

CWapi implements `GET /v1/models` and `POST /v1/chat/completions`.

**How to fix**

Correct the custom OpenAI-compatible provider settings and Agent API key. If the client insists on unsupported OpenAI endpoints/fields, that client version may not be compatible with CWapi 2.0.

## Coding and Agent Tunnels were configured backwards

**Symptom**

The connection exists but ChatGPT discovers the wrong tool catalog, or Coding/Agent behavior appears swapped.

**Likely cause**

A Coding Tunnel was pointed at Agent MCP, or an Agent Tunnel was pointed at Coding MCP.

**What to check**

Coding must expose exactly five `coding_*` tools. Agent must expose exactly three `agent_*` tools.

**How to fix**

Correct each Tunnel to its own local MCP target and use separate Tunnel IDs/Runtime API keys/profiles.

## Workspace seems missing after moving the portable

**Symptom**

CWapi starts in a new directory but previous workspaces are not listed/available.

**Likely cause**

Only the clean program/runtime files were moved, so the new directory created a new `CWapi-data`.

**What to check**

Look in the old extracted directory for:

```text
CWapi-data/workspaces/
```

**How to fix**

Do not delete either data root until you identify the correct one. For a portable move that preserves state, move the complete extracted directory including `CWapi-data`.

## Can I delete `CWapi-data`?

**Symptom**

You want to reset CWapi or reclaim space and are considering deleting `CWapi-data`.

**Likely cause**

`CWapi-data` contains more than disposable cache: config, workspace metadata, durable repositories, and runtime state live there.

**What to check**

Before deletion, inspect whether any workspace contains uncommitted or unpushed work and note that MCP tokens, Agent API key and Tunnel IDs are also stored in config.

**How to fix**

Prefer CWapi's targeted maintenance actions for individual workspaces. If you intentionally delete the whole `CWapi-data`, treat it as a destructive reset: back up required work first and expect to reconfigure CWapi afterward.

## Still stuck?

Collect only the relevant error code, CWapi/Tunnel state, `coding_status` output when applicable, and the exact local HTTP status for Agent. Avoid posting secrets such as MCP tokens, Agent API keys, Tunnel Runtime API keys, or private repository credentials.

See also [FAQ](FAQ.md), [Coding Guide](CODING_GUIDE.md), [Agent Guide](AGENT_GUIDE.md), and [Operations](OPERATIONS.md).
