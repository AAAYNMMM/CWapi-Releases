# CWapi 2.0 Private Codex Toolhost

CWapi uses the bundled official `codex.exe app-server` only as a model-free development toolhost. Coding never starts a Codex thread or turn, never invokes the Codex agent, and never uses Codex login/account APIs.

## Call chain

```text
Web GPT
  -> Coding MCP coding_exec
  -> CWapi final executable/argv/CWD resolution
  -> permanent executionpolicy.Check
  -> private codex.exe app-server
  -> command/exec
  -> Codex code-mode host / Windows sandbox
  -> bounded stdout/stderr result
```

`coding_open` prepares and reserves the durable Git workspace. `coding_exec` is the only Coding execution operation. The Web GPT remains the planner: it calls `rg`, PowerShell, Git, compilers and test runners through exact `command + argv` requests.

## Account isolation

Each command gets a new `CWapi-data/temp/codex-executions/<process-id>` CODEX_HOME containing only CWapi's execution config and defense-in-depth rules. The app-server environment removes OpenAI/Codex API keys, does not read the current user's `~/.codex`, and deletes the execution home when the command ends.

CWapi calls only app-server initialization, Windows sandbox readiness/setup, and `command/exec`. No `thread/start`, `turn/start`, auth, account, model, history or rate-limit method is used by the 2.0 Coding path.

## Read/write behavior

`command/exec` is the official app-server operation for running one command under the Codex sandbox without creating a thread. Therefore no content-forwarding agent or second model is needed.

- read: `rg`, `git diff`, `Get-Content`, language tools；
- write: PowerShell cmdlets, formatters, generators and repository scripts；
- verify: compilers, linters and tests；
- Git: bundled MinGit is added to the private command PATH。

The remote command and CWD use forward-slash syntax and are resolved to a real executable and a directory inside the prepared repository. Output tails are bounded before returning through MCP.

## Sandbox mapping

```text
safe command                       -> command/exec workspaceWrite
full, every permitted command      -> command/exec dangerFullAccess
```

CWapi checks the resolved top-level target before `StartCommand`; permanent denials are profile-independent. Runtime roots are physically confined beneath the workspace before host-side Temp/cache creation. SAFE commands receive workspace-local Temp/cache/profile directories, isolated Git configuration and default-off network access. FULL sends every permanently permitted command through `dangerFullAccess` and restores the current Windows user profile/AppData environment. Guarded Git metadata operations still require the trusted bounded-PATH Git and argument validation. Direct FULL push additionally requires the independent network capability, rejects force/delete/custom receive-pack/local transports, limits Git transport to HTTPS/SSH, and is the only path that restores host Git configuration/credential-helper discovery. Both profiles use the private empty CODEX_HOME and never enable a Codex account.

## Runtime integrity

Current pin: official Codex `0.150.1`. Version, source commit, archive hash and executable hash are recorded in `config/codex-runtime.lock.json` and `config/portable-runtime.lock.json`. The runtime gate performs a real model-free `command/exec` read/write smoke with no model response.
