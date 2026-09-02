# CWapi Coding Core Protocol

CWapi Coding mode lets Web GPT operate one repository-scoped durable workspace through MCP.

## Protocol

- Start or resume a repository workspace with `coding_open(repository_url, target_ref, ..., resume=true)`.
- Use the same canonical `repository_url` for the whole task. CWapi owns the internal session handle; Web GPT does not receive or store a session ID.
- `expected_commit` is an exact guard for the resolved target/baseline used to open the workspace. On a resumed durable workspace, the returned current branch, current HEAD and dirty state are the current Git truth and may legitimately differ from that original baseline.
- Execute one exact local development command with `coding_exec`. Pass the executable in `command` and each argument separately in `argv`; do not encode a shell command line when direct argv is sufficient.
- A repository has one foreground Coding operation at a time. Do not intentionally overlap multiple `coding_exec` calls for the same workspace; wait for the current operation result before starting the next repository mutation.
- Use `coding_status` only when repository/workspace truth is needed. If it reports `state=busy`, use its active command/start/elapsed metadata to distinguish a genuine long-running foreground command from stale state; do not infer failure from `busy` alone.
- Close the active repository session with `coding_close` after the task is genuinely finished. Closing does not reset Git or delete durable workspace data.
- Coding MCP does not provide file or image transfer. Repository text is inspected and changed through repository commands.
- Coding mode directly executes repository-scoped development commands through CWapi; it is not the Agent mode OpenAI tool-call relay.

## Skills

- `load_skill(name)` loads one startup-cached global Skill by its Skill ID.
- Load only Skills listed in the startup Skill inventory. A missing Skill returns `SKILL_NOT_FOUND`.
- Loading a Skill does not modify the repository or workspace.

Tool results are authoritative for command exit status and output. Returned workspace/Git state is authoritative for the state represented by that result. Protocol and safety enforcement are implemented by CWapi, not by natural-language conventions.
