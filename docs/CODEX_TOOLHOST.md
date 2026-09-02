# CWapi 2.0 Private Codex Toolhost

CWapi uses the bundled official `codex.exe app-server` only as a model-free development toolhost. Coding never starts a Codex thread or turn, never invokes the Codex agent, and never uses Codex login/account APIs.

## Call chain

```text
Web GPT
  -> Coding MCP coding_exec
  -> CWapi final executable/argv/CWD resolution
  -> Permanent Safety Guard
  -> SAFE/FULL profile + optional capabilities
  -> private codex.exe app-server
  -> command/exec
  -> Codex code-mode host / Windows sandbox
  -> bounded stdout/stderr result
```

`coding_open` prepares and reserves the durable Git workspace. `coding_exec` remains the only Coding execution operation; its compatible foreground `run` plus additive `start/status/stop` actions cover bounded commands and CWapi-owned persistent processes. The Web GPT remains the planner: it calls `rg`, PowerShell, Git, compilers and test runners through exact `command + argv` requests.

## Account isolation

Each command gets a new `CWapi-data/temp/codex-executions/<process-id>` CODEX_HOME containing only CWapi's execution config and defense-in-depth rules. The app-server environment removes OpenAI/Codex API keys, does not read the current user's `~/.codex`, and deletes the execution home when the command ends.

CWapi calls only app-server initialization, Windows sandbox readiness/setup, and `command/exec`. No `thread/start`, `turn/start`, auth, account, model, history or rate-limit method is used by the 2.0 Coding path.

## Read/write behavior

`command/exec` is the official app-server operation for running one command under the Codex sandbox without creating a thread. Therefore no content-forwarding agent or second model is needed.

- read: `rg`, `git diff`, `Get-Content`, language tools；
- write: PowerShell cmdlets, formatters, generators and repository scripts；
- verify: compilers, linters and tests；
- Git: bundled MinGit is added to the private command PATH。

The remote command and CWD use forward-slash syntax and are resolved to a real executable and a directory inside the prepared repository. Batch bridges are created only at `CWapi-data/runtime/process/<process-id>/bridge/bridge.cmd`; repository contents cannot replace them. Output tails are bounded before returning through MCP.

## Sandbox mapping

```text
safe command                       -> command/exec workspaceWrite
full, every permitted command      -> command/exec dangerFullAccess
```

CWapi checks the resolved top-level target before `StartCommand`; permanent denials are profile-independent and limited to catastrophic disk/boot/elevation behavior, protected CWapi internals, trusted-Git identity and unsafe remote Git transports. SAFE commands receive command-scoped Temp/profile plus reusable cache paths at `CWapi-data/runtime/workspaces/<hash>/cache`; host Git/npm/GitHub identity is isolated. FULL sends every permitted command through `dangerFullAccess`, starts from the current Windows user environment, strips CWapi/OpenAI/Codex internal secrets and keeps normal developer configuration. Neither profile writes `.cwapi-runtime` or `.cwapi-process-bridge.cmd` into a repository.

Network access is an independent capability in both profiles. Remote Git Rewrite is another independent capability and defaults OFF. OFF denies direct force/delete push forms; ON admits those forms but still rejects receive-pack injection, local/ext push transports, `refs/cwapi/*` publication and internal path abuse. Direct local-destructive Git operations create bounded recovery refs before execution when possible. Both profiles use the private empty CODEX_HOME and never enable a Codex account.

Foreground commands own an app-server command/job lifetime and clean the entire tree on completion, cancellation or timeout. Persistent start transfers that ownership to the Host process manager; status is non-blocking, stop closes the owned job, and workspace close/app shutdown stop all matching processes. Process metadata and bridge state remain command-scoped under `CWapi-data/runtime/process`.

## Runtime integrity

Current pin: official Codex `0.150.1`. Version, source commit, archive hash and executable hash are recorded in `config/codex-runtime.lock.json` and `config/portable-runtime.lock.json`. The runtime gate performs a real model-free `command/exec` read/write smoke with no model response.
