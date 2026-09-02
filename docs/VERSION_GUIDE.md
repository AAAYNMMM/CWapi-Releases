# CWapi Version Guide

[English](VERSION_GUIDE.md) | [简体中文](VERSION_GUIDE.zh-CN.md)

CWapi has two intentionally separate release lines. They solve the same broad problem, but their transport, configuration, and day-to-day workflow are different enough that they should be treated as separate products.

## Quick choice

| Version | Transport | Main use |
| --- | --- | --- |
| **2.x** | MCP + OpenAI Secure MCP Tunnel | Coding MCP + OpenAI-compatible Agent bridge |
| **1.6.x** | GitHub + Slack | Legacy Web GPT local-development workflow |

For a new installation, **2.x is the recommended line** unless you specifically need the existing 1.6.x Slack workflow.

## Detailed comparison

| Area | CWapi 2.x | CWapi 1.6.x |
| --- | --- | --- |
| Current release | `2.0.4` | `1.6.3` |
| Release branch | [`main`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main) | [`1.6.x`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/1.6.x) |
| ChatGPT connection | MCP through OpenAI Secure MCP Tunnel | Legacy Slack-mediated workflow |
| Coding transport | Direct Coding MCP tool surface | GitHub + Slack workflow |
| Local provider | Yes: localhost OpenAI-compatible `/v1` Agent Provider | Not part of the 1.6.3 workflow described by that release line |
| Slack | Not required | Required by the legacy workflow |
| Durable workspace | Yes, under `CWapi-data/workspaces/<repository-hash>/repo` | 1.6.x has its own persistence/workspace model; do not treat it as 2.0-compatible storage |
| GitHub role | Repository source/remote used by Coding; private auth uses the Windows user's Git credentials | Core part of the legacy GitHub + Slack development workflow |
| Coding agent reasoning | Web GPT only; bundled Codex is model-free command/exec toolhost | Follow the 1.6.3 documentation for that line's workflow |
| OpenAI-compatible Agent bridge | Yes | No equivalent 2.0 Agent Provider contract in 1.6.3 |
| Secure MCP Tunnel | Coding and Agent use separate Tunnels when both are connected | Not required for the 1.6.3 Slack route |
| Migration difficulty | Fresh setup recommended; configuration schemas are not compatible | Staying on 1.6.3 avoids migration but keeps the legacy transport |
| Recommended users | New users, MCP users, users who want local Coding or an OpenAI-compatible Agent bridge | Existing users who deliberately depend on the established 1.6.3 Slack workflow |

## Choose 2.x when

- you want ChatGPT Web to operate a local Git workspace through a small MCP tool catalog;
- you want durable Coding state that can be resumed from a later ChatGPT conversation;
- you want a localhost OpenAI-compatible Provider backed by Web GPT;
- you want Coding and Agent as separate, independently tunneled surfaces;
- you do not want Slack in the 2.0 path.

Start with [Getting Started](GETTING_STARTED.md).

## Stay on 1.6.3 when

Stay on the `1.6.x` line when your current workflow depends on its GitHub + Slack transport and it is more important to preserve that known setup than to adopt the 2.0 MCP/Tunnel architecture immediately.

The 1.6.3 documentation on the `1.6.x` branch is the source of truth for that release. Do not use 2.0 Coding/Agent configuration instructions against 1.6.3.

## Important compatibility boundaries

### Slack is not the 2.0 transport

CWapi 2.0 does not use Slack for Coding or Agent. Mentions of Slack in 2.0 documentation are limited to version comparison and migration context.

### Secure MCP Tunnel is not a 1.6.3 requirement

The 1.6.3 route predates the 2.0 direct MCP/Tunnel design. Adding a 2.0 Tunnel does not turn a 1.6.3 installation into 2.0.

### Configuration is not portable between lines

Do not copy the complete old `CWapi-data`, old config, Slack credentials, or workspace directories over a 2.0 installation. Reconfigure 2.0 explicitly.

### Workspaces are not migration artifacts

Both generations have persistent-work concepts, but the implementations and directory/config contracts differ. Preserve important work through Git/backup rather than assuming the old workspace directory is a supported 2.0 workspace import format.

## Release links

- CWapi 2.0.4 portable: [`CWapi-v2.0.4.zip`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/releases/download/v2.0.4/CWapi-v2.0.4.zip)
- 2.x source/docs: [`main`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main)
- 1.6.x source/docs: [`1.6.x`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/1.6.x)

If you are moving an existing installation, continue with [Migration from 1.6](MIGRATION_FROM_1.6.md).
