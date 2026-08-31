# Getting Started with CWapi 1.6.3

[English](GETTING_STARTED.md) | [简体中文](GETTING_STARTED.zh-CN.md)

This guide starts from a clean Windows machine and ends with a real CWapi response returning through Slack.

## 1. Download and extract

Download `CWapi-v1.6.3.zip` from the [v1.6.3 release](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/tag/v1.6.3). Extract the whole archive into a user-writable directory, then run `CWapi.exe` from that directory.

Do not run the executable inside the ZIP and do not copy only `CWapi.exe`. CWapi is a portable package with managed runtime files beside the executable.

On first start CWapi creates its local data tree beside the application, including `CWapi-data/config/cwapi.json`, runtime state/log storage, Git mirrors/workspaces, temporary data, and managed runtime state as needed. Slack secrets are not stored in this JSON file.

## 2. Install and authenticate GitHub CLI

Install [GitHub CLI](https://cli.github.com/) yourself, then run:

```powershell
gh auth login
gh auth status
```

CWapi does not perform a separate `gh` login preflight. For private repositories it can configure Git to use the current Windows user's existing `gh auth git-credential` helper when that helper is available. If authentication is invalid, the repository request fails with the real Git/authentication error.

## 3. Create the Slack App

CWapi 1.6.3 uses Slack Socket Mode. A public dedicated control channel is the simplest setup.

**App-Level Token scope**

```text
connections:write
```

**Bot Token scopes**

```text
channels:read
channels:history
chat:write
files:write
```

**Bot event**

```text
message.channels
```

Enable Socket Mode, generate an App Token (`xapp-...`), install/reinstall the app, and obtain the Bot User OAuth Token (`xoxb-...`). Add the bot to the control channel and copy that channel's ID (`C...`).

For a private channel, also configure `groups:read`, `groups:history`, and the `message.groups` event.

The complete Slack walkthrough is in [Slack Setup](SLACK_SETUP.md).

## 4. Save Slack configuration in CWapi

Open the Slack configuration area in CWapi and enter:

```text
App Token   = xapp-...
Bot Token   = xoxb-...
Channel ID  = C...
```

CWapi validates the candidate credentials before replacing the stored pair. The App/Bot tokens are stored in Windows Credential Manager for the current user. The Channel ID is stored in `CWapi-data/config/cwapi.json`.

A healthy/connected Slack state confirms the basic connection, but the first real message/upload is still the best proof that every required scope is present.

## 5. Connect ChatGPT to GitHub and Slack

In ChatGPT, connect the GitHub repository access you want Web GPT to use and connect Slack so Web GPT can send and read messages in the CWapi control channel.

The `xapp-...` and `xoxb-...` values belong only in CWapi. Do not paste them into ChatGPT prompts.

## 6. Understand the first request

A repository-scoped CWapi request needs:

- the GitHub HTTPS repository URL;
- the exact 40-character Git commit SHA;
- a new `request_id`;
- a CWapi MCP v2 frame sent through the configured Slack channel.

CWapi prepares that exact commit in its managed repository workspace. This is why Web GPT first uses GitHub to obtain the repository URL and exact commit.

## 7. First CWapi communication test

A minimal global catalog request can use `mcpServerStatus/list` with empty params. A successful path is:

```text
Web GPT
  -> Slack [CWapi/MCP/2] request
  -> CWapi accepts and executes it
  -> CWapi posts MCP_RESPONSE in the request thread
  -> Web GPT reads the real response
```

There is no `projects/list` registry in 1.6.3.

## 8. First repository test

Use a harmless repository-scoped command first, for example a read-only source search or a project version/test command. Web GPT should:

1. obtain the repository URL and exact commit from GitHub;
2. send a repository-scoped request;
3. let CWapi prepare the managed workspace;
4. run the command;
5. read the returned response;
6. only then proceed to edits/build/test work.

Use [Web GPT Entry](WEB_GPT_ENTRY.md) and [ChatGPT Workflow](CHATGPT_WORKFLOW.md) as the operating rules.

## 9. SAFE and FULL

Leave CWapi in `SAFE` for normal work. Every application start resets permission mode to `safe` before the runtime is created.

`FULL` is for user-authorized tasks that genuinely need broader local permission. It still keeps permanent policy restrictions. A System fallback requires a recognized permission denial and a short-lived, one-time System Token; normal program failures do not qualify.

## 10. What success looks like

Your setup is working when all of these are true:

- `gh auth status` is valid for the repositories you intend to use;
- Slack shows healthy/connected in CWapi;
- the bot is present in the configured channel;
- Web GPT can send the structured request through Slack;
- CWapi returns a response in the same request thread;
- a repository-scoped request runs against the requested exact commit.

If one of those fails, use [Troubleshooting](TROUBLESHOOTING.md).
