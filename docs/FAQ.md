# CWapi 1.6.3 FAQ

[English](FAQ.md) | [简体中文](FAQ.zh-CN.md)

## What is CWapi 1.6.3?

It is the legacy GitHub + Slack release line. Web GPT reasons about the task, uses GitHub for repository truth, and uses Slack to send structured CWapi requests to a local Windows CWapi process. CWapi executes local development work and returns real results/files through Slack.

## How is 1.6.3 different from 2.x?

They are separate product lines. 1.6.3 uses GitHub + Slack Socket Mode and an MCP v2-shaped frame over Slack. 2.x uses a different MCP/Tunnel architecture and has different configuration and workflows. Do not copy 1.6.x Slack config into 2.x. See [Version Guide](VERSION_GUIDE.md).

## Why does 1.6.3 use Slack?

Slack is the remote control and result transport between Web GPT and the local CWapi process. Socket Mode lets CWapi receive events without exposing a public Events API endpoint.

## Does 1.6.3 require MCP?

It does not require a direct ChatGPT-to-local MCP connection. The protocol payload is MCP v2-shaped, but the transport is Slack.

## Is the “MCP v2 frame” the same thing as ChatGPT directly connecting to an MCP server?

No. In 1.6.3, `[CWapi/MCP/2]` identifies the structured frame carried through Slack. ChatGPT is not directly opening a local MCP transport to CWapi.

## Do I need GitHub CLI?

For the supported private-repository authentication path, yes. The user installs GitHub CLI and authenticates with `gh auth login`. CWapi can then use the current Windows user's existing `gh auth git-credential` helper for non-interactive Git operations when available.

## Why is an exact commit required?

CWapi prepares a managed workspace for a precise repository state. A full 40-character `expected_commit` prevents local execution from silently happening against a different branch tip or checkout than Web GPT intended.

## What is the persistent workspace?

For the life of one CWapi process, the same repository reuses one managed workspace. Normal request completion releases the repository lease but keeps the workspace, allowing compatible ignored/untracked build state to be reused.

## How long does the workspace remain?

Until the current CWapi process shuts down. Normal shutdown removes repository workspaces. The next startup sweeps stale workspaces left by an earlier process. Shared bare Git mirrors are retained.

## What is the difference between SAFE and FULL?

`SAFE` is the normal mode and is restored on every application start. It constrains writable/execution behavior to CWapi's managed boundaries and permanent rules. `FULL` is a user-authorized mode that can permit a tightly controlled System fallback after a recognized sandbox permission denial; it does not disable permanent safety policy.

## Why does FULL still have a System Token?

Because `FULL` is not a blanket direct-System execution mode. `process_start` remains safe-backend-first. Only a recognized permission denial can cause CWapi to issue a short-lived, one-time token bound to the same repository, commit, executable, argv, and cwd. The caller retries that exact invocation with a new request ID.

## How are screenshots returned to ChatGPT?

For Playwright, call `browser_take_screenshot` without `filename`. That lets the MCP result contain actual image bytes. CWapi can then externalize those bytes as a Slack File in the request thread.

## How are ordinary files returned?

The underlying MCP result must provide the actual bytes/resource content that CWapi can externalize. Printing a local path does not make CWapi read and upload that file automatically.

## Why does Web GPT stop waiting after three minutes?

The workflow caps one continuous wait/poll period at three minutes. This avoids burning an entire conversation on repeated polling. If the task still runs, Web GPT reports the current state and later queries the same `process_id` with a new status request.

## What if the task is still running after three minutes?

Keep the stable `process_id`. Do not restart the task. Later call `process_status` with a new global request ID; call `process_stop` only if the task should actually be stopped.

## How do private repositories authenticate?

CWapi uses non-interactive Git and can configure an isolated credential helper that invokes the current Windows user's existing `gh auth git-credential`. It does not modify global Git config. If the GitHub CLI login/permissions are insufficient, the repository request fails with the real Git/authentication error.

## Where are Slack tokens stored?

The Slack App Token and Bot Token are stored in Windows Credential Manager for the current Windows user. The Slack Channel ID is stored in `CWapi-data/config/cwapi.json`.

## Why does the config still contain version `1.6.1`?

That is the **config schema version**, not the product release version. CWapi product version is `1.6.3`, while the current `cwapi.config.v2` schema still declares version `1.6.1`. Replacing that value with `1.6.3` would make the current code reject the config.

## Can I migrate 1.6.x configuration directly into 2.x?

No. The communication architecture and configuration model changed. Install the two release lines in separate directories and configure 2.x independently using its own `main`-branch documentation.

## Does CWapi run an AI model?

No. Web GPT does the reasoning. CWapi prepares local execution context, runs tools/processes, and transports results.
