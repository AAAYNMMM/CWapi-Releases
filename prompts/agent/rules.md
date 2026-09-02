# CWapi Agent Rules

- Load task Skills only when relevant via `load_skill(name)`. Coding/implementation uses `coding`; diagnosis uses `debugging`; Git uses `git`; verification uses `testing`; release work uses `release`. Do not load all Skills by default.
- Before each meaningful execution phase, send a concise `progress` update that states what will be done next and why. Also update progress when the plan materially changes, a root cause is found, or a blocker changes the next action. Do not narrate every trivial read or shell command.
- `progress` is human-visible task state. It must not be used as heartbeat or completion; CWapi owns heartbeat/keepalive and terminal success is structured `completion`.
- Keep complex work recoverable. One tool call should normally perform one clear logical action. Split inspection, modification, testing, Git operations and release actions into separate recoverable phases.
- Do not bundle unrelated edits, tests, Git mutations, long scripts or large text payloads into one oversized shell/PowerShell command merely to reduce tool calls.
- Prefer native file/edit/search capabilities or short repository-side scripts over deeply quoted commands containing large source files, Markdown, JSON, HTML, JavaScript, PowerShell or other nested payloads.
- Read narrowly before broadly. Search or inspect relevant ranges first instead of dumping large files, logs or repository trees into context without need.
- A failed tool call is normal tool feedback. Read the error, classify whether it is a parameter, environment, project or protocol problem, change the cause, and continue when recoverable. Do not blindly repeat the same failed command.
- Treat redelivery of the same `request_id` as resume, not new work. Use preserved state and tool history and avoid repeating completed steps.
- If a resumed/redelivered request has an uncertain side-effecting operation such as commit, push, release, deletion or external write, inspect the authoritative current state before retrying it.
- Keep requests in the same `agent_exchange` batch isolated. Never reuse one request's tool IDs, progress, result or completion for another request.
- Do not send completion while required work remains unfinished. When the request is actually terminal, send the structured completion response required by the protocol.
