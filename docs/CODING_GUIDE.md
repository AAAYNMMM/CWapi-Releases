# CWapi 2.0 Coding Guide

[English](CODING_GUIDE.md) | [简体中文](CODING_GUIDE.zh-CN.md)

CWapi Coding gives Web GPT a small MCP surface for operating one durable local Git workspace per canonical repository. Web GPT remains the planner and reasoning agent throughout the task.

## Architecture

```text
Web GPT
  ↓ reasoning + exact tool calls
Coding MCP
  ↓
CWapi Coding service
  ↓
Durable Git workspace
  ↓
Bundled Codex app-server command/exec
  ↓
Windows tools / Git / build / tests
```

The bundled Codex runtime is **not** a second coding agent. CWapi uses app-server initialization, Windows sandbox readiness/setup, and model-free `command/exec`. It does not create `thread/start` or `turn/start`, does not invoke a Codex model, does not read a Codex account, and does not require Codex login.

## Coding tools

```text
coding_open
coding_exec
coding_status
coding_close
```

Web GPT never receives or stores a public Coding session ID. Every later operation selects the active internal session through the same canonical `repository_url`.

## Recommended end-to-end workflow

```text
User supplies repository URL / task
        ↓
coding_open
        ↓
Inspect repository and relevant source
        ↓
coding_exec for search/read
        ↓
Edit files through exact commands
        ↓
Build / test / verify
        ↓
coding_status when Git truth matters
        ↓
If authorized and needed: switch FULL for .git metadata operation
        ↓
commit / push if requested
        ↓
coding_status final check
        ↓
coding_close
```

`coding_close` closes the active handle only. It does not reset Git, clean files, remove uncommitted work, or delete the durable workspace.

## `coding_open`

Conceptual input:

```text
repository_url
 target_ref
 expected_commit?  # optional full 40-hex SHA
 resume?           # default false
```

### `repository_url`

Use the GitHub repository URL for the repository you want CWapi to manage. CWapi canonicalizes repository identity, so later calls should keep using the same repository URL instead of inventing alternate local paths.

### `target_ref`

`target_ref` is required and must resolve as a valid branch. CWapi canonicalizes it to `refs/heads/<branch>`, fetches that branch from `origin`, and prepares a detached worktree at the resolved commit.

Examples:

```text
main
1.6.x
refs/heads/feature/example
```

A tag or arbitrary commit is not a substitute for the branch `target_ref` in the current workspace contract.

### `expected_commit`

This is an optional exact-baseline guard. When provided it must be a complete 40-character hexadecimal SHA.

CWapi fetches the target branch and independently resolves its remote commit. If that result differs from `expected_commit`, open fails with a mismatch instead of silently checking out another revision.

Use it when Web GPT already knows the exact GitHub commit that must be tested or modified.

### First open, later open, and dirty workspaces

With `resume=false`:

- first use clones the repository with a managed `origin`;
- later use verifies repository identity and fetches the requested branch;
- tracked dirty state is rejected with `WORKSPACE_DIRTY`;
- local commits ahead of the fetched branch are rejected with `WORKSPACE_LOCAL_COMMITS`;
- diverged local history is rejected with `WORKSPACE_DIVERGED`;
- CWapi does not silently discard user work to make the baseline look clean.

### `resume=true`

Use `resume=true` only when intentionally continuing an existing compatible workspace/session.

CWapi requires:

- the workspace already exists;
- repository identity matches;
- workspace metadata is valid;
- `target_ref` matches the existing context;
- `expected_commit`, if supplied, matches the stored resolved commit.

Resume does **not** perform a fresh fetch/resync. It returns the existing HEAD and tracked-dirty state and marks the result `resumed=true`.

This is exactly what makes a new ChatGPT conversation able to continue unfinished local work safely.

## Active session ownership and `CODING_WORKSPACE_BUSY`

One canonical repository can have at most one active Coding session.

If that repository is already active:

- compatible `coding_open(..., resume=true)` reuses the existing internal session;
- `resume=false` returns `CODING_WORKSPACE_BUSY`;
- opening/closing state, incompatible target ref, or incompatible expected commit is not silently adopted;
- if a `coding_exec` is already running, the resumed/open result may report a `busy` state.

Do not work around this by changing URL spelling or cloning the same repository elsewhere. Use the same `repository_url` and resume the real active session.

## Durable workspace location and lifetime

The workspace root is adjacent to the portable installation:

```text
CWapi-data/workspaces/<sha256-derived-repository-key>/repo
```

Metadata lives beside it as `workspace.json`.

The workspace is intentionally durable across Coding session closes and ChatGPT conversation changes. `coding_close` does not delete it.

The Desktop maintenance action can delete a selected durable workspace when the user intentionally wants to rebuild it. Deleting a workspace loses local/uncommitted work in that workspace.

## `coding_exec`

`coding_exec` runs exactly one development command in the active repository workspace.

Conceptual input:

```text
repository_url
command
argv[]
cwd?             # optional directory inside the prepared repository
timeout_seconds?
```

Pass executable and arguments separately. Do not put a shell-quoted mega-command into `command` unless the executable itself is intentionally a shell such as `pwsh`.

Good:

```text
command = rg
argv    = ["-n", "AgentAPIKey", "internal", "docs"]
```

Also valid when shell semantics are genuinely needed:

```text
command = pwsh
argv    = ["-NoProfile", "-Command", "Get-Content README.md | Select-Object -First 80"]
```

The `cwd` must remain inside the prepared repository. CWapi resolves the actual executable and working directory before passing the invocation to the sandbox/toolhost.

## Inspect before editing

A good Coding turn usually starts with a small number of information-rich reads rather than dozens of one-line tool calls.

Examples:

```text
rg -n "symbol|error|config" src tests docs

git status --short --branch

git show HEAD:path/to/file
```

When several searches are independent, group them sensibly. Avoid repeatedly reading an unchanged file unless new evidence makes another read useful.

## Reading ordinary text

Source, Markdown, JSON, config, logs, and other inspectable text should be read through `coding_exec`.

Coding MCP has no file or image transfer tool and does not emit MCP `ImageContent` or `EmbeddedResource` content.

For large files, prefer bounded output: a relevant range, search matches with context, or a project-specific query. This reduces MCP round trips and avoids flooding the conversation with irrelevant bytes.

## Editing files

Web GPT can edit files by running exact commands/scripts in the workspace. The important rules are:

1. inspect the file and surrounding behavior first;
2. make the smallest coherent change;
3. keep paths inside the managed repository;
4. run the narrowest useful verification immediately after the edit;
5. broaden validation only when needed.

CWapi does not provide a separate “AI edit” model. The edit is whatever deterministic command/script Web GPT requested.

## Build and test

Use project-native tooling through `coding_exec`, for example:

```text
go test ./...
cargo test
npm test
pytest
```

The actual available tools come from CWapi's bundled/visible Windows environment and the repository. Do not assume a compiler or package manager exists just because it exists on another machine.

The bundled portable includes its own Git runtime and the Codex command toolhost. Other project runtimes are project/host dependent.

## `coding_status`

Use `coding_status(repository_url)` when you need Git/workspace truth, especially:

- after meaningful edits;
- before commit/push;
- before final handoff;
- when recovery/resume state is unclear.

It reports the active workspace's current state, including target ref, resolved commit, current HEAD, tracking head, tracked dirty state, and divergence when available.

It is an inspection operation; it does not fetch or mutate Git just to make the report look newer.

## SAFE

Use `SAFE` for ordinary read/edit/build/test work.

Coding maps SAFE to the bundled Codex sandbox's workspace-write behavior. This permits normal work in the repository while protecting `.git` metadata from ordinary sandboxed writes.

SAFE is not “read-only”. Editing source files is expected to work.

## FULL

Switch to `FULL` only after the user has authorized the operation and the command genuinely needs `.git` metadata writes or broader host access.

Typical examples:

```text
git commit
git push
other explicitly authorized Git metadata operations
```

Changing SAFE/FULL is hot-swappable while a Coding session remains open. A `coding_exec` already running keeps the profile selected when it started; the next `coding_exec` gets the new profile.

FULL does not mean “ignore every safety rule”. Permanent CWapi execution policy remains in force. Never force-push or delete remote refs unless the user explicitly requested that action.

## Git operations

Before committing:

```text
git status --short
git diff --check
```

Before pushing, confirm:

- the intended branch/ref;
- the staged file set;
- no unexpected source/user-data files;
- the user actually asked for the remote write.

Because new non-resume preparation refuses local commits/divergence, an unfinished local commit should normally be resumed rather than reopened as a fresh task.

## Private Git repositories

Private repository clone/fetch/push uses the current Windows user's existing Git/GitHub credential setup. CWapi does not require a separate Codex identity for Git authentication.

If authentication fails, verify the Windows user's GitHub/Git credential setup outside CWapi. Do not place GitHub tokens in prompts or repository files.

## Files and images

Coding MCP does not transfer files or images. Inspect source, Markdown, JSON, configuration, logs, and other text with bounded `coding_exec` commands. Binary files and images remain in the workspace and are not copied into the ChatGPT conversation.

## Closing and later resuming

At real task completion:

```text
coding_status(repository_url)
coding_close(repository_url)
```

Closing:

- releases the active internal session owner;
- does not clean Git;
- does not reset files;
- does not delete uncommitted changes;
- does not delete the durable workspace.

A later task can intentionally resume the existing compatible workspace with `resume=true`. If you want a clean new baseline instead, first ensure the durable workspace is clean/appropriate or explicitly delete/rebuild it using CWapi's maintenance flow.

## Example: complete bug-fix flow

```text
1. User: fix a bug in https://github.com/acme/widget on main.
2. Web GPT: coding_open(repository_url, target_ref="main", expected_commit=<known SHA>).
3. Web GPT: coding_exec rg/search for the failure path.
4. Web GPT: coding_exec bounded reads of relevant files.
5. Web GPT: coding_exec edit script/command.
6. Web GPT: coding_exec narrow test.
7. Web GPT: coding_exec broader test if justified.
8. Web GPT: coding_status.
9. If user requested commit/push, operator switches FULL.
10. Web GPT: coding_exec git status/diff --check, then authorized git commit/push.
11. Web GPT: coding_status final verification.
12. Web GPT: coding_close.
```

## Related documentation

- [Getting Started](GETTING_STARTED.md)
- [Agent Guide](AGENT_GUIDE.md)
- [Troubleshooting](TROUBLESHOOTING.md)
- [Codex Toolhost](CODEX_TOOLHOST.md)
- [Protocol](PROTOCOL.md)
