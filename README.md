# CWapi 1.6.3

[English](README.md) | [简体中文](README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/AAAYNMMM/chatgpt-work-api-Releases?filter=v1.6.3&style=flat-square&label=Release)](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/tag/v1.6.3)
![Windows](https://img.shields.io/badge/Windows-11%20x64-0078d4?style=flat-square)
![Transport](https://img.shields.io/badge/Transport-GitHub%20%2B%20Slack-4A154B?style=flat-square)

**CWapi 1.6.x is the legacy GitHub + Slack release line. Current release: `1.6.3`.**

CWapi 1.6.3 lets ChatGPT Web use GitHub for source truth and Slack as a control/data channel to run real development work on a local Windows machine. Web GPT does the reasoning. CWapi does not run an AI model.

> Looking for CWapi 2.x? Use the [`main` branch](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main). The 2.x MCP/Tunnel/Coding/Agent architecture is a separate product line. Do not copy 1.6.x Slack configuration into 2.x.

## How it works

```text
ChatGPT Web
   │ GitHub + Slack
   ▼
Slack control channel
   ▼
CWapi 1.6.3
   ▼
Local Windows development tools
   ▼
Slack response / files
```

ChatGPT reads and changes the remote repository through GitHub, obtains an exact 40-character commit SHA, then sends a structured CWapi request through the configured Slack channel. CWapi prepares that exact commit in its local managed workspace and runs local build, test, browser, script, or process tools. Results return through Slack.

CWapi 1.6.3 uses an **MCP v2-shaped message frame over Slack**. This is not a direct ChatGPT-to-local MCP connection and it does not require an OpenAI Secure MCP Tunnel.

## Why use 1.6.3

- Use ChatGPT Web as the reasoning agent while keeping execution on your Windows PC.
- Execute against an exact GitHub commit instead of an ambiguous local checkout.
- Reuse a persistent repository workspace for the life of the CWapi process, preserving useful untracked/ignored build state and caches.
- Run long tasks with `process_start`, `process_status`, and `process_stop`.
- Return screenshots and other tool-produced files through Slack.
- Keep normal work in `SAFE`; use `FULL` only when a task genuinely needs broader local permission.
- Keep Slack App/Bot tokens out of the JSON config and store them in Windows Credential Manager.

## Choose the right release line

| Release line | Transport | Main use |
| --- | --- | --- |
| **1.6.x** | GitHub + Slack Socket Mode | Legacy Web GPT local-development workflow |
| **2.x** | MCP + OpenAI Secure MCP Tunnel | Separate current architecture; see `main` |

Read the [Version Guide](docs/VERSION_GUIDE.md) before switching lines.

## 5-minute quick start

1. Download [`CWapi-v1.6.3.zip`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/tag/v1.6.3) and fully extract it.
2. Run `CWapi.exe` from the extracted directory.
3. Install [GitHub CLI](https://cli.github.com/) and run `gh auth login`, then `gh auth status`.
4. Create a Slack App with Socket Mode, the required scopes, and a dedicated control channel. Follow [Slack Setup](docs/SLACK_SETUP.md).
5. In CWapi, save the App Token, Bot Token, and Channel ID.
6. Connect GitHub and Slack in ChatGPT.
7. Give Web GPT your repository and ask it to follow [Web GPT Entry](docs/WEB_GPT_ENTRY.md) and [ChatGPT Workflow](docs/CHATGPT_WORKFLOW.md).
8. Confirm the first CWapi response appears in the same Slack request thread.

For the full first-run path, use [Getting Started](docs/GETTING_STARTED.md).

## Core concepts

### GitHub is source truth

Repository-scoped requests carry both `repository_url` and `expected_commit`. The commit must be a full 40-hex Git commit available to the managed mirror. ChatGPT should update GitHub first when tracked source must persist, then run local validation against the resulting exact commit.

### Persistent workspace

Within one CWapi process, the same repository reuses one managed workspace. Each new repository request can resync tracked source to a new exact commit while leaving compatible ignored/untracked derived state in place. The workspace is removed during normal shutdown or startup cleanup of stale workspaces; shared Git mirrors are retained.

### SAFE and FULL

`SAFE` is the default and is reset on every CWapi start. `FULL` does not remove permanent safety rules. A System fallback is only available after a recognized permission denial and uses a short-lived, one-time System Token bound to the original invocation.

### Long-running processes

`process_start` starts repository-scoped work. If it is still running, keep the returned `process_id` and query it with new global `process_status` request IDs. Use `process_stop` to stop it. Web GPT should not continuously wait longer than three minutes in one stretch; if the task is still running, report the current state and continue later with status checks.

## Typical uses

- Build and test a GitHub project on the user's real Windows environment.
- Run localhost services and browser automation.
- Search source in the prepared repository workspace, then read or edit exact files through GitHub.
- Produce screenshots or generated files and return them through Slack.
- Keep a long build/server process alive while Web GPT checks status separately.

## Security and privacy

Slack App/Bot tokens are stored in the current Windows user's Credential Manager. Only the Slack Channel ID is stored in `CWapi-data/config/cwapi.json`. Treat the configured Slack channel as a trusted control boundary. CWapi redacts known secrets from ordinary runtime logs, but raw transport/current-session response storage may retain a short-lived System Token so duplicate delivery can work.

Private Git access can use the current Windows user's existing `gh auth git-credential` helper. CWapi does not modify global Git configuration.

See [Security](docs/SECURITY.md) for the detailed boundary.

## Documentation

- [Getting Started](docs/GETTING_STARTED.md)
- [User Guide](docs/USER_GUIDE.md)
- [Slack Setup](docs/SLACK_SETUP.md)
- [Web GPT Entry](docs/WEB_GPT_ENTRY.md)
- [ChatGPT Workflow](docs/CHATGPT_WORKFLOW.md)
- [FAQ](docs/FAQ.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Version Guide](docs/VERSION_GUIDE.md)
- [Protocol](docs/PROTOCOL.md)
- [Security](docs/SECURITY.md)
- [Architecture](docs/ARCHITECTURE.md)

## Development vs release repository

This repository is the release-facing snapshot and documentation for CWapi. Development happens in [`AAAYNMMM/CWapi`](https://github.com/AAAYNMMM/CWapi). The 1.6.x branch intentionally excludes development history, tests, build automation, and other release-irrelevant material.
