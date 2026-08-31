# CWapi 1.6.3 ChatGPT Workflow

[English](CHATGPT_WORKFLOW.md) | [简体中文](CHATGPT_WORKFLOW.zh-CN.md)

This is the full operating model for Web GPT when using CWapi 1.6.3.

## 1. Transport and protocol

The remote caller sends MCP v2 frames through the configured Slack channel. CWapi 1.6.3 has no project registry; repository requests carry the GitHub repository and exact commit directly.

A complete frame is:

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

Rules:

- first line is `+++`;
- every new action/state snapshot gets a new `request_id`;
- `repository_url` and `expected_commit` appear together or not at all;
- repository URL is an ASCII GitHub HTTPS repository URL;
- `expected_commit` is a full 40-hex commit;
- use `/` for Windows paths inside JSON when possible;
- top-level `system_token` is only for the allowed process fallback path.

## 2. Standard repository flow

1. Web GPT resolves the target repository and exact commit through GitHub.
2. It sends a unique Slack MCP v2 request.
3. CWapi validates protocol, route, scope, arguments, and System Token placement before claim.
4. For repository scope, CWapi prepares Git authentication as needed, acquires the repository lease, prepares the shared mirror, and enters the repository's process-lifetime workspace.
5. Tracked source is synchronized to `expected_commit`; compatible ignored/untracked derived state is not automatically cleaned.
6. Stock MCP work runs in the request-scoped context; `cwapi/process_start/status/stop` are handled as CWapi Core virtual tools.
7. CWapi stores the terminal response and posts it to the original Slack thread.
8. A normal terminal repository request releases the repository lease but keeps the workspace until CWapi shutdown.

Same `request_id` plus the same canonical fingerprint can return the stored response again. Reusing a request ID with a different fingerprint is a conflict.

## 3. Workspace model

One CWapi process keeps one persistent workspace per repository.

### What persists between requests

Primarily ignored/untracked derived state, such as dependency directories, build products, caches, generated files, and temporary migration/build state.

### What does not safely persist

Uncommitted tracked-source edits. A later prepare can force tracked source back to the requested exact commit.

### Switching commits

A later request for the same repository may use a new `expected_commit`. CWapi fetches when needed, forces the managed worktree to that commit, verifies HEAD, and does not run an automatic `git clean`.

If old derived state conflicts with the new tracked tree, prepare can fail rather than deleting data silently. Use the project's own cleanup command when the evidence justifies it.

### Lifetime

- request terminal: release lease, keep repository workspace;
- normal CWapi shutdown: remove process-lifetime repository workspaces;
- next startup: sweep stale repository workspaces left by a prior process;
- shared bare mirrors: kept for reuse and pruned safely.

## 4. Task sizing

Prefer small independently verifiable steps. Persistent workspace means build/test/copy/upload work does not have to be crammed into one giant script merely to preserve derived state.

Combine steps only when they truly need shared in-process memory, a live browser/session, an atomic transaction, or another non-reconstructable state.

Different repositories can run independently. The same repository is serialized by its repository lease while an earlier repository task remains active.

## 5. Source search

For code discovery, Web GPT should create a read-only search command or short script and run it in the current repository workspace.

Prefer output such as:

```text
path
line
matching text
small context
```

Limit directories/file types when possible and exclude unrelated dependency/build/cache directories. Do not clone/fetch the same repository again for search.

A useful pattern is:

```text
GitHub -> exact commit
CWapi -> read-only local search
GitHub -> read exact files
Web GPT -> decide changes
GitHub -> commit changes
CWapi -> build/test new exact commit
```

## 6. Executable and environment discovery

Do not assume CWapi sees the same PATH as an interactive terminal.

When a tool is needed:

1. check CWapi-managed/portable runtime first;
2. then check what the running CWapi process can actually resolve on the host;
3. if neither exists, report it as missing/unverified;
4. install only after the user explicitly authorizes `FULL` or installs it manually;
5. after installation, probe the real executable/version again.

`process invocation could not be resolved` should first be treated as an executable-resolution failure, not as proof that the project itself is broken.

## 7. Process start/status/stop

`cwapi/process_start` is repository-scoped. It uses the exact-commit workspace and arguments containing the command, argv, and optional cwd.

If the task reaches terminal state quickly, start can return the terminal record immediately. Otherwise it returns a stable `process_id`.

For a running process:

- each status check uses a new global request ID;
- keep querying the same `process_id`;
- do not restart the original process;
- use `process_stop` when requested or necessary.

Public process states: `starting`, `running`, `completed`, `failed`, `stopped`.

## 8. SAFE, FULL, and System Token

Every service start resets permission mode to `safe` before runtime construction.

`SAFE` confines ordinary repository work to the managed writable root and applies permanent execution-policy checks.

`FULL` still starts with the safe backend. Only a CWapi-recognized structured permission denial can open the System fallback path. A normal `PROGRAM_FAILURE` does not.

A fallback System Token is:

- at most 60 seconds old;
- one-time use;
- limited by the maximum active-token cap;
- bound to repository, commit, final executable, argv, and real cwd;
- accepted only at request top level.

Retry with a new `request_id` and identical invocation binding. If the token expires, start the permission flow again. System failure does not recursively mint another token.

## 9. Stock MCP and browser state

`mcpServerStatus/list` is global with empty params. Stock resources/tools can be global or repository-scoped depending on their route.

The caller does not supply a Codex `threadId`; CWapi manages request-scoped execution context.

Do not assume unrelated stock MCP requests share browser page/tab/locator/session state. For a multi-step browser flow such as navigate -> fill -> click -> assert -> screenshot, keep the steps together when the underlying tool/session requires it, or rebuild state explicitly in the next request.

## 10. Screenshots and binary results

To return a Playwright screenshot through Slack, call `browser_take_screenshot` without `filename`. This lets the MCP result carry real image content, which CWapi can externalize through Slack's upload flow.

A text path such as `./screenshot.png` is not automatically dereferenced. CWapi does not read arbitrary local paths from text output.

## 11. Waiting rule

One continuous period of waiting/polling for a build, runner, Slack response, or other asynchronous result must not exceed **three minutes**.

At three minutes without a terminal result:

1. stop the continuous wait;
2. report “task is still running” and the current request/process state;
3. keep the stable `process_id` if one exists;
4. later use a new global status request to inspect the same process.

Do not chain repeated short polls to evade the three-minute total.

## 12. Caller responsibilities

Web GPT should:

- use a fresh request ID for every new action/state query;
- preserve exact repository/commit truth;
- avoid duplicate clones of the same managed repository;
- keep source searches read-only and focused;
- use GitHub for durable tracked-source changes;
- reuse valid derived workspace state instead of reinstalling/rebuilding without cause;
- keep Slack/System credentials out of prompts, issues, logs, and long-lived documents;
- distinguish permission denial from ordinary program failure;
- request actual binary/resource content when a file must be returned, not only a local path string.
