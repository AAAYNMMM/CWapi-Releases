# CWapi 1.6.3 Troubleshooting

[English](TROUBLESHOOTING.md) | [简体中文](TROUBLESHOOTING.zh-CN.md)

Use this guide by symptom. For Slack setup details, see [Slack Setup](SLACK_SETUP.md). For protocol rules, see [ChatGPT Workflow](CHATGPT_WORKFLOW.md).

## CWapi does not start

**Symptom**

`CWapi.exe` closes immediately, reports a startup/config error, or the GUI never becomes usable.

**Likely cause**

The portable package is incomplete, the directory is not writable, the config is invalid, or a required bundled runtime/data-root check failed.

**What to check**

- Confirm the whole ZIP was extracted.
- Confirm `CWapi.exe` is not being run from inside the ZIP.
- Check `CWapi-data/config/cwapi.json` and the latest runtime error.
- Remember that the product is 1.6.3 but the current `cwapi.config.v2` schema version is still `1.6.1`.

**How to fix**

Re-extract the complete `CWapi-v1.6.3.zip` into a clean user-writable directory. Do not manually change the config schema version to `1.6.3`; the current code expects `1.6.1` for that schema field.

## Slack shows degraded or keeps reconnecting

**Symptom**

Slack briefly connects and then becomes degraded, or the Socket Mode connection repeatedly reconnects.

**Likely cause**

Invalid/revoked token, missing `connections:write`, wrong Workspace, network interruption, or channel-readiness failure.

**What to check**

- App Token begins with `xapp-...`.
- Socket Mode is enabled.
- App-Level Token includes `connections:write`.
- Bot Token belongs to the same Workspace.
- Channel ID is correct.
- Bot is in the configured channel.

**How to fix**

Correct the Slack App settings, reinstall the app after scope changes, regenerate/re-enter revoked tokens, and save the candidate credentials again in CWapi.

## CWapi receives no ChatGPT request

**Symptom**

ChatGPT posts to Slack, but CWapi never claims the request.

**Likely cause**

Wrong channel, bot not subscribed to the correct event, malformed frame, or the first line is not `+++`.

**What to check**

- ChatGPT is using the same Slack control channel configured in CWapi.
- Public channel uses `message.channels`; private channel uses `message.groups`.
- The frame begins exactly with:

```text
+++
[CWapi/MCP/2][MCP_REQUEST][REQUEST_ID]
```

- JSON schema/protocol are v2.

**How to fix**

Send the request in the configured channel using a complete MCP v2 frame. Reinstall the Slack App after adding/changing event subscriptions.

## CWapi can receive requests but cannot reply

**Symptom**

CWapi claims or processes the request but no response appears in Slack.

**Likely cause**

Missing `chat:write`, revoked Bot Token, Slack API failure, or channel membership changed.

**What to check**

- `chat:write` is present.
- Bot Token is valid.
- Bot is still a member of the configured channel.
- Slack runtime error details.

**How to fix**

Add/fix the scope, reinstall the app, restore channel membership, and retry with a new request ID when the previous request did not produce a usable terminal response.

## File upload fails

**Symptom**

Text responses work but screenshots or other files do not appear.

**Likely cause**

Missing `files:write`, the underlying tool returned only a text path, or the binary result could not be externalized.

**What to check**

- `files:write` is installed on the Bot Token.
- The tool result actually contains binary/resource content.
- For Playwright screenshot, no `filename` was passed.

**How to fix**

Add `files:write` and reinstall the app. For screenshots call `browser_take_screenshot` without `filename`. Do not expect a printed local path to be uploaded automatically.

## Bot is not in the channel

**Symptom**

Readiness or posting reports that the bot is not a member, or channel operations fail despite correct scopes.

**Likely cause**

The bot was never invited, was removed, or the configured Channel ID points to another channel.

**What to check**

Compare the configured `C...` Channel ID with the actual control channel and inspect channel membership.

**How to fix**

Invite the CWapi bot to the exact configured channel, then re-run the real communication test.

## `missing_scope`

**Symptom**

Slack returns `missing_scope`.

**Likely cause**

The Slack App configuration is missing one of the permissions needed for the attempted operation, or the app was not reinstalled after the scope was added.

**What to check**

Public-channel baseline:

```text
connections:write
channels:read
channels:history
chat:write
files:write
message.channels
```

Private channel additionally:

```text
groups:read
groups:history
message.groups
```

**How to fix**

Add the missing scope/event and reinstall the app to the Workspace.

## Private repository cannot be accessed

**Symptom**

Mirror fetch/clone/update fails for a private GitHub repository.

**Likely cause**

The current Windows user's GitHub CLI login is missing, expired, or lacks repository permission; the credential helper cannot be resolved; or the repository URL is invalid.

**What to check**

Run:

```powershell
gh auth status
```

Confirm the user can access the repository and that the URL is a normal GitHub HTTPS repository URL.

**How to fix**

Run `gh auth login` again or grant the authenticated account access. CWapi can use the existing `gh auth git-credential` helper; it does not repair the GitHub account's permissions for you.

## Exact commit is rejected or unavailable

**Symptom**

Repository preparation fails because `expected_commit` is invalid, missing, or cannot be found in the mirror.

**Likely cause**

Short SHA, typo, branch name supplied instead of a commit, commit not fetched/available from the repository, or repository/commit mismatch.

**What to check**

- `expected_commit` is exactly 40 hexadecimal characters.
- The commit belongs to the requested repository.
- GitHub shows the same commit.

**How to fix**

Resolve the exact commit from GitHub again and send a new request with the correct repository URL + full commit pair.

## `process invocation could not be resolved`

**Symptom**

`process_start` cannot resolve the requested executable/script.

**Likely cause**

The command is not in CWapi's frozen runtime PATH, a guessed absolute path is wrong, or a repository script was referenced only by basename/cwd assumptions.

**What to check**

- CWapi-managed runtime/tool paths first.
- What the running CWapi process can actually resolve via `where`/`Get-Command` or another read-only probe.
- Repository-relative script path.

**How to fix**

Use the real resolved executable path or the explicit repository-relative script path. If the tool is genuinely absent, install it only with user-authorized `FULL` or manually, then probe it again.

## SAFE permission failure

**Symptom**

A command is blocked because it needs access outside the SAFE execution boundary.

**Likely cause**

The invocation needs broader filesystem/system permission than SAFE allows, or it violates a permanent policy rule.

**What to check**

Distinguish a structured `PERMISSION_DENIED` from an ordinary program/test failure. Check whether the requested top-level command itself is permanently blocked.

**How to fix**

If the task legitimately needs broader permission, the user can switch to `FULL` and retry through the documented fallback flow. Permanent-policy blocks cannot be bypassed by FULL.

## FULL is enabled but System execution still does not happen

**Symptom**

A command fails in FULL but CWapi does not issue/accept a System Token.

**Likely cause**

The failure is not a recognized sandbox permission denial, the invocation changed, the token expired/was used, the token is in the wrong JSON location, or permanent policy blocks the command.

**What to check**

- Original response is `blocked + PERMISSION_DENIED`.
- Token is top-level and still within its 60-second lifetime.
- Retry uses a new `request_id` but the same repository, commit, executable, argv, and cwd.
- Token has not already been consumed.

**How to fix**

Repeat the permission flow correctly. If the program itself failed, fix the program instead of trying to convert a normal failure into a permission bypass.

## Process remains `running`

**Symptom**

`process_start` returns a `process_id` and the task does not finish quickly.

**Likely cause**

The process is genuinely long-running, waiting on its own work, or is a server/watcher designed to stay alive.

**What to check**

Call `process_status` using a new global request ID and inspect stdout/stderr tails and state.

**How to fix**

Keep querying the same `process_id` at reasonable intervals. Do not resend `process_start`. Stop it only when appropriate with `process_stop`.

## Three-minute waiting limit reached

**Symptom**

Web GPT reports that the task is still running instead of continuing to wait.

**Likely cause**

The workflow deliberately caps one continuous wait/poll window at three minutes.

**What to check**

Confirm whether a stable `process_id` or request state already exists.

**How to fix**

Keep that state and check again later with a new status request. Do not chain many shorter polls just to bypass the three-minute total.

## Playwright page/session state disappeared

**Symptom**

A later browser action cannot find the expected page, locator, or session state from an earlier request.

**Likely cause**

Unrelated stock MCP requests should not be assumed to share browser page/tab/locator/session state.

**What to check**

Whether the browser steps were split across separate request-scoped contexts.

**How to fix**

Keep tightly coupled navigate/fill/click/assert/screenshot work together when the tool requires a live session, or explicitly rebuild the required page state in the new request.

## Screenshot only returns a path

**Symptom**

The response contains something like `./screenshot.png`, but ChatGPT receives no image file.

**Likely cause**

The tool was asked to write to a filename and returned text instead of image content.

**What to check**

Whether `browser_take_screenshot` was called with `filename`.

**How to fix**

Call it again without `filename` so the MCP result includes image bytes for CWapi to upload.

## Duplicate request behavior is unexpected

**Symptom**

Repeating a Slack message returns an old stored response, or the same request ID causes a conflict.

**Likely cause**

CWapi fingerprints requests. The same request ID + same fingerprint is idempotent and can replay the stored response; the same request ID + different fingerprint is rejected.

**What to check**

Compare the full canonical request content and request ID.

**How to fix**

Use a fresh request ID for every new action/state query. Reuse an ID only when intentionally retrying the exact same request.

## Workspace preparation fails

**Symptom**

CWapi cannot prepare/resync the repository workspace or reports a workspace root/integrity problem.

**Likely cause**

Git/authentication failure, tracked/untracked path conflict, filesystem/reparse issue, stale incompatible derived state, or managed root integrity failure.

**What to check**

- Git/authentication error detail.
- Whether ignored/untracked state conflicts with the new tracked tree.
- `CWapi-data/workspaces` root integrity.
- Whether a symlink/reparse point replaced a managed root.

**How to fix**

Fix authentication or filesystem integrity first. If project-derived state conflicts with the new commit, clean only the necessary project build/cache directories using the project's own commands. Do not manually delete random CWapi internals while the program is running.

## Need more diagnostic data

Use the GUI's latest runtime/structured execution error information and the bounded runtime logs. Ordinary logs redact known credential/token shapes. Do not paste Slack tokens or System Tokens into issues or public logs while debugging.
