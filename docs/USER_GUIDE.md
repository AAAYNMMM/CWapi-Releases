# CWapi 1.6.3 User Guide

[English](USER_GUIDE.md) | [简体中文](USER_GUIDE.zh-CN.md)

This guide explains normal day-to-day use after the first Slack and GitHub setup is complete.

## The working model

```text
You give Web GPT a development task
        ↓
Web GPT reads/changes GitHub and gets an exact commit
        ↓
Web GPT sends a CWapi MCP v2 frame through Slack
        ↓
CWapi prepares the local exact-commit workspace
        ↓
Local build / test / browser / process work
        ↓
CWapi returns text or files through Slack
        ↓
Web GPT decides the next step
```

Web GPT is the reasoning agent. CWapi does not run a model.

## Repository requests

Repository-scoped work must carry both:

```text
repository_url
expected_commit
```

The repository URL must be a GitHub HTTPS repository URL and the commit must be a full 40-character SHA. CWapi prepares a detached managed workspace for that exact commit and verifies the resulting HEAD.

If Web GPT changes tracked source, the reliable workflow is:

1. edit/commit through GitHub;
2. obtain the new exact commit;
3. send the next CWapi request using that commit;
4. build/test locally against the new exact commit.

Do not rely on uncommitted tracked-source changes remaining across requests. CWapi may resync tracked source on every repository prepare.

## Persistent workspace

CWapi 1.6.3 keeps one repository workspace per repository for the life of the current CWapi process. A normal terminal request releases the repository lease but does not delete the workspace.

Useful ignored/untracked state can therefore survive between requests, for example:

- `target/`
- `node_modules/`
- `.venv/`
- `build/`
- `dist/`
- compiler caches and generated files

This does not mean every cache is always valid. If a lockfile, toolchain, build configuration, target platform, or source layout changes, use the project's normal cleanup/reinstall command when the evidence says it is necessary.

Repository workspaces are removed during normal shutdown. On startup CWapi also sweeps stale workspaces left by an earlier process. Shared bare mirrors remain for reuse.

## Source searching

When Web GPT needs to locate a function, error string, type, config key, or file, it should prefer a short read-only repository-scoped search in the existing workspace. Keep output focused on path, line number, matching text, and a little context.

Do not clone the same repository again just to search it. Once the relevant files are identified, Web GPT can use GitHub to read or edit them precisely.

## Process tools

CWapi exposes these virtual process tools in 1.6.3:

```text
cwapi/process_start
cwapi/process_status
cwapi/process_stop
```

`process_start` is repository-scoped and uses the prepared workspace. If the process completes quickly, the start response may already be terminal. Otherwise it returns a stable `process_id`.

For a running process:

- keep the `process_id`;
- use a new global request ID for each `process_status` call;
- do not resend the original `process_start`;
- use `process_stop` when the task should be stopped.

Possible public process states are `starting`, `running`, `completed`, `failed`, and `stopped`.

## Three-minute waiting limit

Web GPT should not continuously wait or poll an external build, runner, Slack response, or long process for more than three minutes in one stretch.

If there is still no terminal result after three minutes, the correct behavior is to tell the user that the task is still running and report the current request/process state. Later checks should query the existing `process_id`; they must not restart the task just because humans dislike waiting.

## SAFE mode

Use `SAFE` for normal read/build/test work. In safe mode the writable scope is constrained to the managed execution root, plus the dedicated global MCP temporary root where appropriate.

CWapi resets permission mode to `safe` on every application start. The previous `FULL` state is not restored.

## FULL and System fallback

`FULL` is a user-authorized runtime mode, not a permanent “disable safety” switch. Permanent top-level execution rules still apply.

For `process_start`, CWapi remains Codex-safe-backend-first. A System fallback is only possible when the safe backend returns a CWapi-recognized structured permission denial. In that case CWapi may issue a System Token that is:

- valid for 60 seconds;
- one-time use;
- bound to the same repository, commit, executable, arguments, and working directory;
- accepted only at the top level of the fallback request.

The retry uses a new `request_id` but the original invocation must otherwise remain identical. A normal test/build/program failure does not receive a token.

## Screenshots and files

CWapi can externalize binary MCP result content to Slack using Slack's external file upload flow.

For Playwright screenshots, call `browser_take_screenshot` **without** a `filename` when the goal is to send the image back to ChatGPT. That allows the MCP result to contain actual image bytes, which CWapi can upload as a Slack File.

A plain text path such as `./image.png` is only text. CWapi does not automatically read arbitrary local files just because a tool printed their path.

Other tool-produced binary/file content can be returned when the underlying MCP result actually provides the bytes/resource content needed for CWapi to externalize it.

## GitHub authentication

CWapi uses non-interactive Git for repository preparation. For private repositories it can configure an isolated Git credential helper that calls the current user's existing `gh auth git-credential`.

CWapi does not modify global Git config and does not inherit broad Git/GitHub debug or secret environment variables into its child processes.

## Moving or updating the portable directory

CWapi stores `CWapi-data` beside the executable. If you move only the program files but leave `CWapi-data` behind, the moved copy starts with a different local data directory.

For 1.6.3 updates, extract the new portable package into a clean directory rather than mixing files from different releases. If you intentionally want to keep the same local 1.6.x data, move the whole portable directory while CWapi is stopped.

## Related guides

- [Getting Started](GETTING_STARTED.md)
- [Slack Setup](SLACK_SETUP.md)
- [Web GPT Entry](WEB_GPT_ENTRY.md)
- [ChatGPT Workflow](CHATGPT_WORKFLOW.md)
- [FAQ](FAQ.md)
- [Troubleshooting](TROUBLESHOOTING.md)
