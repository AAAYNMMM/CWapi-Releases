# CWapi 2.0 FAQ

[English](FAQ.md) | [简体中文](FAQ.zh-CN.md)

This FAQ covers the questions that most often matter before installing CWapi 2.0 or while deciding whether to keep using the 1.6.3 line.

## What is CWapi?

CWapi is a portable Windows bridge that lets ChatGPT Web work with local development resources without turning CWapi into another reasoning model.

CWapi 2.0 has two independent surfaces:

- **Coding**: ChatGPT Web uses MCP tools to operate a durable local Git workspace.
- **Agent**: local software uses CWapi's localhost OpenAI-compatible API, while Web GPT answers those model requests through Agent MCP.

CWapi itself does not run a language model.

## Should I use 2.0 or 1.6.3?

Use **2.0** for the current MCP/Tunnel architecture, Coding MCP, durable 2.0 workspaces, and the OpenAI-compatible Agent bridge.

Keep **1.6.3** if you intentionally depend on the legacy GitHub + Slack workflow and do not want to migrate that transport/configuration yet.

The two lines are not drop-in compatible. See [Version Guide](VERSION_GUIDE.md) and [Migration from 1.6](MIGRATION_FROM_1.6.md).

## What is the difference between Coding and Agent?

**Coding** is for ChatGPT Web operating a repository through these MCP tools:

```text
coding_open
coding_exec
coding_status
coding_attachment
coding_close
```

**Agent** is for local OpenAI-compatible software sending model requests to Web GPT through:

```text
agent_open
agent_exchange
agent_close
```

Agent does not automatically gain the Coding tools, and Coding does not automatically act as an OpenAI-compatible provider.

## Does CWapi 2.0 need Slack?

No. CWapi 2.0 uses MCP plus OpenAI Secure MCP Tunnel for its ChatGPT connection. Slack belongs to the 1.6.x legacy workflow.

## Does CWapi 1.6.3 need Secure MCP Tunnel?

No. The 1.6.3 release line uses the older GitHub + Slack workflow. Do not copy the 2.0 Tunnel setup into 1.6.3 documentation or configuration.

## Does Coding require a Codex login or Codex account?

No.

The bundled Codex runtime is not used as an AI account/model path for Coding. CWapi does not require Codex login/account access for normal 2.0 Coding operation.

## What does the bundled Codex runtime actually do?

CWapi bundles the official Codex runtime, currently `0.150.1`, only as a model-free `command/exec` toolhost and Windows sandbox component.

The 2.0 Coding path does not create a Codex thread, does not create a Codex turn, and does not hand the repository task to Codex Agent.

## Does Coding use Codex Agent quota?

No. Web GPT is the only reasoning agent in the Coding chain. The bundled Codex component is used as a model-free command toolhost, so CWapi Coding does not route work through Codex Agent quota.

## Why does CWapi need Secure MCP Tunnel?

CWapi intentionally binds its local MCP server to `127.0.0.1`. ChatGPT runs remotely and cannot directly reach your PC's loopback interface.

Secure MCP Tunnel provides the supported private connection path between the remote ChatGPT side and the local CWapi MCP endpoint without requiring you to publish a public inbound MCP server.

## Why can't I put `127.0.0.1` or `localhost` into ChatGPT?

Because `127.0.0.1` always means "this machine" from the program that is connecting. From ChatGPT's remote environment, it does not refer to your Windows PC.

Use the matching Secure MCP Tunnel instead.

## Can Coding and Agent run at the same time?

Yes. They are designed as isolated surfaces with separate MCP tokens, local endpoints, tool catalogs, Tunnel profiles, and runtime processes.

## Do I need two Tunnels?

If both Coding and Agent are connected to ChatGPT, yes. Use one Secure MCP Tunnel for Coding and a second independent Tunnel for Agent.

Do not reuse one Tunnel ID/profile for both surfaces.

## Can I use Cline or Roo Code with Agent?

They **may work if the client supports a custom OpenAI-compatible provider** and the request shape it sends is compatible with CWapi 2.0.

CWapi does not claim that every Cline/Roo Code version or extension configuration is guaranteed to work.

## What are SAFE and FULL?

`SAFE` is the normal Coding profile for reading, editing, building, and testing inside the managed workspace. It protects `.git` metadata from ordinary sandboxed writes.

`FULL` is for explicitly authorized operations that need `.git` metadata writes or broader host access, such as `git commit` or `git push`.

A command already running keeps the profile it started with. The next `coding_exec` uses the newly selected profile.

## Where is the Coding workspace?

Under the CWapi portable directory:

```text
CWapi-data/workspaces/<repository-hash>/repo
```

The workspace is durable. Closing a Coding session does not delete it.

## Can a new ChatGPT conversation continue an unfinished Coding task?

Yes, when the existing workspace/session is compatible.

Use the same `repository_url` and open it with:

```text
coding_open(..., resume=true)
```

The public MCP protocol does not expose a Coding session ID. CWapi maps the canonical repository URL to its internal active session.

## What happens if I open the same repository again without resume?

If the repository already has an active Coding session, `resume=false` returns:

```text
CODING_WORKSPACE_BUSY
```

This is intentional. CWapi avoids silently creating or stealing competing active state for the same repository.

## Does upgrading CWapi delete my workspace?

Not by itself.

If you move the **entire extracted directory**, including `CWapi-data`, the existing data/workspaces move with it.

If you copy only a clean `CWapi.exe`/`runtime` set into another directory, that directory creates a new `CWapi-data`, so the old workspace can appear to have "disappeared" even though it is still in the old directory.

Before upgrading, close active sessions and back up important unpushed work.

## Why can't I attach a normal source file with `coding_attachment`?

Because `coding_attachment` is deliberately image-only.

Source, Markdown, JSON, logs, PDFs, archives, DOCX, and other ordinary files should be inspected through `coding_exec`. A non-image request returns:

```text
CODING_ATTACHMENT_IMAGE_ONLY
```

CWapi 2.0 does not expose ordinary files through MCP `EmbeddedResource`.

## What images does Coding support?

Current Coding limits:

```text
maximum images:       16
maximum per image:    32 MiB
maximum batch:        64 MiB
maximum side length:  4096 px
SVG:                  unsupported
```

Supported inputs are validated raster images such as PNG, JPEG, GIF, and WebP.

## What images does Agent support?

Agent accepts raster images only through standard Chat Completions message content using:

```text
type = image_url
url  = data:...
```

Current Agent limits:

```text
maximum images:       8
maximum per image:    8 MiB
maximum batch:        16 MiB
maximum side length:  2048 px
SVG:                  unsupported
```

A generic top-level file `attachments` extension is not supported.

## Does uploading a file to the ChatGPT conversation copy it into my local workspace/client?

No. A ChatGPT conversation upload is not an automatic file-transfer path into a CWapi Coding workspace or a local Agent client.

## Does CWapi store my full prompts and answers?

Normal CWapi observability does not persist full prompts, full answers, complete tool schemas, complete tool arguments, or complete tool results. CWapi does not create a conversation transcript store.

`coding_exec` stdout/stderr is returned as a bounded result for the current command rather than kept as a complete long-term command-output history.

## Where is the Agent Provider API key stored?

The Agent Provider API key is generated and stored in:

```text
CWapi-data/config/cwapi.json
```

The GUI exposes/masks it for local client configuration.

## Where are the Tunnel Runtime API keys stored?

In Windows Credential Manager, separately for Coding and Agent:

```text
CWapi/2.0/OpenAI/Tunnel/APIKey
CWapi/2.0/OpenAI/Tunnel/Agent/APIKey
```

They are not stored in `cwapi.json`.

## Where are the MCP tokens stored?

The Coding and Agent MCP bearer tokens are stored in:

```text
CWapi-data/config/cwapi.json
```

The Coding/Agent Tunnel IDs and other non-secret runtime configuration are also stored there.

## How does private Git repository authentication work?

Coding uses the current Windows user's existing Git/GitHub credential environment for private clone/fetch/push.

CWapi does not use a Codex account as a Git identity. If private Git authentication fails, fix the Windows Git/GitHub credential setup. Do not paste personal GitHub tokens into prompts or commit them into the repository.

## What local Agent API should I configure?

Use:

```text
Base URL: http://127.0.0.1:<agent-port>/v1
Model:    cwapi-web-gpt
API key:  the Agent API key shown by CWapi
```

CWapi 2.0 implements:

```text
GET  /v1/models
POST /v1/chat/completions
```

See [Agent Guide](AGENT_GUIDE.md) for request, streaming, tool-call, and error behavior.

## Related documentation

- [Getting Started](GETTING_STARTED.md)
- [Coding Guide](CODING_GUIDE.md)
- [Agent Guide](AGENT_GUIDE.md)
- [Troubleshooting](TROUBLESHOOTING.md)
- [Version Guide](VERSION_GUIDE.md)
- [Migration from 1.6](MIGRATION_FROM_1.6.md)
- [Security](SECURITY.md)
