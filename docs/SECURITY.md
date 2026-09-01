# CWapi 2.0 Security

## Trust boundaries

- MCP and Provider listeners bind only `127.0.0.1`；
- Coding/Agent routes use independent high-entropy bearer tokens；
- Provider uses an independent bearer API key with constant-time comparison；
- public HTTPS exposure, if needed by ChatGPT, is an explicit external deployment boundary；
- Coding creates an isolated empty CODEX_HOME per command and never reads native Codex identity/credentials。

## Coding

CWapi resolves the final target executable/argv/CWD and calls `executionpolicy.Check` immediately before every private app-server `command/exec`. Destructive Git operations, Git credential plumbing, unsafe Git global redirection/configuration and forbidden system tools are permanently denied in both profiles. Generated Codex rules remain defense in depth; they are not the authoritative boundary.

SAFE maps to upstream Codex `workspaceWrite` and is intended for source read/edit/test. Each command receives workspace-local Temp, language/package caches, application-data directories and a synthetic user profile. OpenAI/Codex/GitHub token variables, host Git global/system configuration, Git hooks and interactive credential discovery are excluded. Network access is a separate default-off capability and can be changed without changing SAFE/FULL.

FULL grants upstream `dangerFullAccess` to every command that first passes CWapi permanent policy and restores the current Windows user profile/AppData environment. It still does not bypass permanent denials: destructive Git, credential commands, forbidden system tools, protected CWapi paths, repository Git lookalikes for guarded metadata operations, and amend/force/delete/receive-pack/local-transport forms remain rejected. A direct FULL push additionally requires explicit network access, limits transport to HTTPS/SSH, and is the only command allowed to restore host Git configuration/credential-helper discovery. API tokens remain stripped from the child environment, and `git credential` plus credential-manager executables remain permanently denied.

SAFE/FULL are the complete CWapi 2.0 Coding access profiles. CWapi does not issue an additional reusable elevation credential. Workspace-local runtime roots are checked against symlink/junction redirection before the host creates Temp/cache files. Commands launched by repository test/build scripts remain contained by the selected upstream sandbox; CWapi's exact-invocation policy is applied to the resolved top-level target.

The Coding MCP public surface uses `repository_url` as stable identity and does not expose the random internal session ID. CWapi still keeps that internal ID and exact repository ownership mapping for cancellation, close races and stale-operation protection.

Coding MCP has no file or image transfer tool and emits no MCP file/resource/image content. Workspace text needed by Web GPT is read through bounded `coding_exec` output.

Workspace maintenance resolves targets under the managed root and is available only from the local Desktop confirmation flow.

## Agent

- HTTP request body hard limit 1 MiB；
- active queue hard limit 16；
- internal bridge IDs remain random and generation-scoped; public `request_id` remains random and exact-correlated；
- `request_id` is transaction-only and is never promoted to third-party command/session identity；optional `metadata.task_id` / `metadata.correlation_id` is bounded and client-supplied；
- stale lease, timeout, disconnect and close terminalize requests once；
- malformed messages/tools/responses are rejected before delivery；
- exchange activity reports only broker revision and queue truth；`no_request` never asserts external process state；
- top-level `attachments` is rejected with `AGENT_FILE_ATTACHMENTS_UNSUPPORTED`；
- any non-text message content part, including `image_url`, is rejected with `AGENT_MEDIA_INPUT_UNSUPPORTED` before broker admission；
- Agent emits no MCP file/resource/image content and ChatGPT conversation uploads are not imported into local software；
- normal observability stores metadata only, not full conversation payloads。

Server Instructions may guide workflow and efficiency, but they are not a security boundary. Authorization, input validation, request correlation, duplicate handling and terminal-state rules remain enforced by Go code.

## Config and package

Config contains local bearer secrets and must remain inside adjacent `CWapi-data`. It is never copied into candidate stage. Runtime archives/executables are SHA-256 pinned; package audit scans names and content for user data, credentials, logs, machine identity and absolute build paths.

Tunnel Runtime API keys are stored in Windows Credential Manager and injected only into their matching tunnel-client child process environment. They are not written into `cwapi.json` or generated Tunnel profiles.

## Reporting

Security issues should include affected version/commit, exact local route or component, reproduction, impact, and whether the issue requires SAFE or FULL. Do not include real tokens or credentials in reports.
