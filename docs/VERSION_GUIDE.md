# CWapi Version Guide

[English](VERSION_GUIDE.md) | [简体中文](VERSION_GUIDE.zh-CN.md)

CWapi 1.6.x and 2.x are maintained as separate release lines because their transport, configuration, and workflow models are substantially different.

| Release line | Transport | Main use |
| --- | --- | --- |
| **2.x** | MCP + OpenAI Secure MCP Tunnel | Current Coding MCP and OpenAI-compatible local-provider architecture; documented on `main` |
| **1.6.x** | GitHub + Slack Socket Mode | Legacy Web GPT local-development workflow |

## Choose 1.6.x when

- you already have the GitHub + Slack workflow configured and want to keep it stable;
- you rely on the 1.6.x exact-commit Slack control flow;
- you want the legacy persistent workspace/process model without migrating right now.

Current legacy release: `1.6.3`.

## Choose 2.x when

- you want the current product line;
- you want ChatGPT to connect through CWapi's 2.x MCP/Tunnel workflow instead of Slack control messages;
- you want the separate 2.x local OpenAI-compatible provider functionality described on the `main` branch.

Use the [`main` documentation](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main) for exact 2.x behavior. Do not infer 2.x setup from this branch.

## Important differences

### ChatGPT connection

**1.6.x:** ChatGPT connects to GitHub and Slack. Slack carries CWapi request/response frames.

**2.x:** uses its own MCP/Tunnel connection model. Slack is not the 2.x control transport.

### GitHub role

**1.6.x:** GitHub is directly part of the standard workflow. Web GPT obtains the repository URL/exact commit and handles durable source changes through GitHub.

**2.x:** use the `main` documentation; do not assume the 1.6.x exact-commit Slack protocol applies.

### Local provider

**1.6.x:** no localhost OpenAI-compatible provider is part of this release line.

**2.x:** has a separately documented local provider architecture.

### Slack

**1.6.x:** required for the normal remote control/result flow.

**2.x:** do not configure it by copying 1.6.x Slack settings.

### Persistent workspace

**1.6.x:** one repository workspace per repository for the life of the CWapi process; tracked source is resynced to exact commits while compatible ignored/untracked derived state can remain.

**2.x:** has a different durable Coding workspace model; read the `main` Coding documentation for its lifetime and resume rules.

### Configuration migration

The configuration schemas are not interchangeable. Do not overwrite a 1.6.x portable directory with 2.x and copy the old config wholesale.

A safe migration pattern is:

1. keep the existing 1.6.3 directory intact;
2. extract 2.x into a separate directory;
3. configure 2.x from scratch using its own documentation;
4. verify the new workflow independently;
5. keep or retire 1.6.x only after you know which workflow you want.

## Branches

- [`1.6.x`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/1.6.x): CWapi 1.6.x legacy documentation/releases.
- [`main`](https://github.com/AAAYNMMM/chatgpt-work-api-Releases/tree/main): CWapi 2.x current line.
