# Getting Started with CWapi 2.0

[English](GETTING_STARTED.md) | [简体中文](GETTING_STARTED.zh-CN.md)

This guide starts from a clean Windows machine and ends with both a first Coding request and a first Agent request.

CWapi 2.0 has two independent surfaces. You can configure only the one you need.

```text
Coding: ChatGPT Web -> Secure MCP Tunnel -> Coding MCP -> local Git workspace
Agent:  local OpenAI-compatible client -> CWapi /v1 -> Agent MCP -> Secure MCP Tunnel -> ChatGPT Web
```

## 1. Download CWapi 2.0.2

Download the official Windows portable:

[`CWapi-v2.0.2.zip`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/download/v2.0.2/CWapi-v2.0.2.zip)

Fully extract the ZIP into a directory your Windows user can write to. Do not run `CWapi.exe` from inside the ZIP and do not copy only the executable; the portable includes pinned Git, Codex toolhost, and tunnel runtime files beside it.

Then run:

```text
CWapi.exe
```

## 2. What the first launch creates

CWapi stores runtime/user data beside the executable under `CWapi-data`.

The first launch creates the v3 config if it does not exist:

```text
CWapi-data/config/cwapi.json
```

Default configuration includes:

- generated Coding MCP bearer token;
- generated Agent MCP bearer token;
- generated Agent Provider API key;
- MCP loopback port, default `32124`;
- Agent Provider loopback port, default `32123`;
- Agent enabled by default;
- Coding access profile `safe`;
- empty/disabled Coding and Agent Tunnel configuration until you set it.

Repository workspaces appear only after Coding opens a repository:

```text
CWapi-data/workspaces/<repository-hash>/repo
```

Tunnel profiles appear after the corresponding Tunnel is configured:

```text
CWapi-data/tunnel/coding/openai-tunnel.yaml
CWapi-data/tunnel/agent/openai-tunnel.yaml
```

The Runtime API keys themselves are not written into those YAML files or `cwapi.json`; CWapi stores the Coding and Agent Runtime API keys in separate Windows Credential Manager entries.

## 3. Coding vs Agent

Choose **Coding** when you want ChatGPT Web itself to operate a local Git project: inspect source, edit files, run commands, build, test, and perform authorized Git operations.

Choose **Agent** when you have local software that speaks the OpenAI-compatible Chat Completions API and you want its model requests answered by Web GPT.

They are independent. Coding can be used without Agent; Agent can be used without an active Coding workspace; both can run at the same time.

## 4. Why ChatGPT cannot use `127.0.0.1`

CWapi intentionally binds its MCP server to loopback:

```text
http://127.0.0.1:<mcp-port>/mcp/coding/<coding-token>
http://127.0.0.1:<mcp-port>/mcp/agent/<agent-token>
```

Those URLs are reachable only from the Windows machine running CWapi. ChatGPT is a remote service, so pasting either loopback URL into a remote Server URL field cannot make ChatGPT reach your PC.

OpenAI's current guidance for MCP servers running on a developer machine/private network is to use **Secure MCP Tunnel** rather than expose the local MCP server to the public Internet. ChatGPT plan/workspace availability and the UI can change; use the current OpenAI Developer Mode/MCP documentation when enabling custom MCP apps.

Current OpenAI help page:

https://help.openai.com/en/articles/12584461

## 5. Create the Coding Secure MCP Tunnel

On the OpenAI Platform side, create a Secure MCP Tunnel for the Coding surface. The exact navigation labels may change, but you need two values:

```text
Tunnel ID       = tunnel_...
Runtime API key = secret key authorized to run/use that Tunnel
```

The Tunnel ID identifies the tunnel. The Runtime API key authenticates the local `tunnel-client` to the OpenAI tunnel control plane; it is transport authentication, not a Codex login and CWapi does not use it to ask an OpenAI model to perform Coding.

Use a Runtime API key with only the tunnel permissions required by your organization. Do not paste the key into ChatGPT prompts, commit it to Git, or store it in `.env` for CWapi.

## 6. Configure the Coding Tunnel in CWapi

Open the Coding Tunnel configuration in the GUI and enter:

```text
Tunnel ID
Runtime API key
```

CWapi stores the ID in `CWapi-data/config/cwapi.json` and the Runtime API key in Windows Credential Manager. It generates a Coding-only `openai-tunnel.yaml` profile whose `main` channel points to the private Coding MCP endpoint.

When the configuration is valid, the bundled `tunnel-client` starts. If its child process fails or exits unexpectedly, CWapi performs bounded local restart attempts; the GUI can show a reconnecting state.

## 7. Connect Coding in ChatGPT

Use the custom MCP app / Developer Mode flow available to your ChatGPT workspace. Do not select a direct `127.0.0.1` Server URL. Use the Secure MCP Tunnel connection path and select the same Coding Tunnel you configured in CWapi.

The goal is simple regardless of UI wording:

1. ChatGPT is allowed to use custom MCP apps/actions needed by Coding.
2. The app is connected through the same Secure MCP Tunnel.
3. The Tunnel is running on the CWapi PC.
4. ChatGPT discovers exactly these Coding tools:

```text
coding_open
coding_exec
coding_status
coding_close
```

If those tools do not appear, stop there and use [Troubleshooting](TROUBLESHOOTING.md). There is little value in heroically debugging a repository before the transport exists.

## 8. First Coding test

Use a repository you are willing to let CWapi clone into its managed workspace. A typical first request is:

```text
Open https://github.com/OWNER/REPO on branch main, inspect its current state,
and do not modify anything yet.
```

Web GPT should roughly perform:

```text
coding_open(repository_url, target_ref="main")
        ↓
coding_status(repository_url)
        ↓
optional read-only coding_exec(...)
```

`target_ref` is required and represents a branch. You may also supply a full 40-character `expected_commit` to ensure the fetched branch resolves to exactly the commit you expect.

For normal read/edit/test work, stay in `SAFE`.

## 9. How Coding work continues in a new ChatGPT conversation

CWapi does not expose a public Coding session ID. The stable key Web GPT uses is the canonical `repository_url`.

If the same repository already has a compatible active session/workspace, a later ChatGPT conversation continues with:

```text
coding_open(repository_url, target_ref, expected_commit?, resume=true)
```

CWapi reuses the internal active session and returns `resumed=true`. It does not prepare a second workspace.

If you call `resume=false` while that repository is still active, CWapi returns `CODING_WORKSPACE_BUSY` instead of silently stealing the workspace.

## 10. Configure Agent

Agent has two sides:

```text
Local client -> http://127.0.0.1:<agent-port>/v1
ChatGPT Web  -> Secure MCP Tunnel -> Agent MCP
```

The local Provider is already available when Agent is enabled, but requests cannot complete unless the Agent MCP bridge is open in ChatGPT.

Create a **second** Secure MCP Tunnel for Agent. Do not reuse the Coding Tunnel ID or Runtime API key. Configure the Agent Tunnel panel with its own:

```text
Tunnel ID
Runtime API key
```

CWapi runs a separate tunnel-client profile/process for it and points only that profile at Agent MCP.

## 11. Connect Agent MCP in ChatGPT

Create/connect a separate custom MCP app using the Agent Tunnel. It should expose exactly:

```text
agent_open
agent_exchange
agent_close
```

In the Agent Web GPT conversation:

1. call `agent_open()` once;
2. call `agent_exchange(capacity=4)` (or the smaller `max_inflight` returned by `agent_open`);
3. process every request returned in the batch;
4. submit completed responses in the next `agent_exchange` call;
5. keep the exchange loop active for the continuous task;
6. call `agent_close()` when that continuous Agent task is actually finished.

Do not close/reopen merely because one exchange returns `no_request`. It means only that no new local OpenAI request arrived before the bounded wait expired; it does not prove that a local command is running and is not, by itself, a reason to wait again.

## 12. Configure the local OpenAI-compatible client

CWapi's default Agent Provider values are:

```text
Base URL: http://127.0.0.1:32123/v1
Model:    cwapi-web-gpt
API key:  copy the Agent API key from CWapi GUI
```

If you changed the Agent port, use the GUI's actual Base URL instead.

The implemented endpoints are:

```text
GET  /v1/models
POST /v1/chat/completions
```

The Provider uses Bearer authentication. A wrong key returns HTTP 401.

Clients such as Cline or Roo Code can be tested using their custom OpenAI-compatible provider option when available. Client-side compatibility is not guaranteed across every release.

## 13. First Agent test

First confirm the bridge is open in ChatGPT. Then from the local client send a simple chat request to model `cwapi-web-gpt`.

The path is:

```text
POST /v1/chat/completions
        ↓
CWapi broker queues request_<...>
        ↓
agent_exchange returns that request to Web GPT
        ↓
Web GPT returns content and/or tool_calls for the same request_id
        ↓
CWapi returns Chat Completions response to local software
```

If the local client receives 503, the Agent MCP bridge is not available/open. If it receives 429, the bounded queue is busy. If Web GPT does not complete the request before the default 180-second request timeout, the Provider returns 504.

## 14. Files and media are not transported

Coding MCP has no file/image transfer tool. Agent accepts text and tool JSON only: top-level `attachments` returns `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`, while any non-text message content part such as `image_url` returns `AGENT_MEDIA_INPUT_UNSUPPORTED`.

A file uploaded into the ChatGPT conversation is not copied into local software or the Coding workspace.

## Next steps

- [Coding Guide](CODING_GUIDE.md)
- [Agent Guide](AGENT_GUIDE.md)
- [FAQ](FAQ.md)
- [Troubleshooting](TROUBLESHOOTING.md)
