# CWapi 1.6.3 Web GPT Entry

[English](WEB_GPT_ENTRY.md) | [简体中文](WEB_GPT_ENTRY.zh-CN.md)

Use this page as the short operating entry for Web GPT. Read [ChatGPT Workflow](CHATGPT_WORKFLOW.md) for the full rules.

## Role split

```text
Web GPT
  -> reasons about the task
  -> uses GitHub for repository truth and source changes
  -> uses Slack to send CWapi control frames and read responses

CWapi 1.6.3
  -> prepares the exact local repository commit
  -> runs local tools/processes
  -> returns real results/files through Slack
```

CWapi does not run an AI model. Its MCP v2 frame is transported through Slack; it is not a direct ChatGPT-to-local MCP connection.

## Before a repository action

Web GPT must know:

1. the GitHub HTTPS `repository_url`;
2. the full 40-character `expected_commit`;
3. what local action should run;
4. a fresh `request_id`.

GitHub remains the source of truth for tracked source, commits, branches, PRs, issues, reviews, Actions/CI, and releases.

## MCP v2 frame

A complete frame starts and ends with `+++`:

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2",...}
+++
```

The first line must be `+++`. Use `/` in Windows paths inside JSON when practical. If a backslash is required, escape it according to JSON rules.

## Repository workflow

```text
GitHub: resolve repository + exact commit
        ↓
Slack: send repository-scoped CWapi request
        ↓
CWapi: prepare/reuse persistent workspace at exact commit
        ↓
CWapi: search / build / test / browser / process work
        ↓
Slack: return response/file
        ↓
Web GPT: inspect result and choose next action
```

If tracked source changes, commit those changes through GitHub first, obtain the new exact commit, then validate locally against that commit.

## Source search

When repository code must be located, prefer a small read-only search command in the prepared workspace. Return only the useful path/line/match/context. Do not clone or fetch the same repository again just to search it.

After locating a file, read or edit the full file through GitHub when GitHub-native source work is needed.

## Process handling

Use:

```text
cwapi/process_start
cwapi/process_status
cwapi/process_stop
```

`process_start` is repository-scoped. If it returns `running`, keep the `process_id`. Each later `process_status` or `process_stop` uses a new global request ID. Do not resend the original start request.

Do not continuously wait/poll for more than three minutes. If the process is still running, report that fact and its current state rather than silently extending the wait forever.

## Persistent workspace rule

Treat a request as an execution step, not as the workspace lifetime. In one CWapi process, later requests for the same repository re-enter the same managed workspace.

Tracked source may be resynced to `expected_commit`; ignored/untracked derived state is the main cross-request reusable state.

## SAFE / FULL

Stay in `SAFE` for normal work. `FULL` is only for explicit user-authorized tasks that need broader local permission. Permanent safety rules still apply.

A System Token is only valid for a recognized permission-denied fallback. Never invent, reuse, or move it into nested params. The fallback request uses a new `request_id` while keeping the original repository/commit/invocation unchanged.

## Screenshots and files

For Playwright screenshot delivery, call `browser_take_screenshot` without `filename` so the MCP result can return actual image content for CWapi to upload to Slack.

A printed local path is not a file transfer. CWapi only externalizes content actually returned by the underlying MCP result.

## Start here

For a new user, follow [Getting Started](GETTING_STARTED.md). For complete execution behavior, follow [ChatGPT Workflow](CHATGPT_WORKFLOW.md).
