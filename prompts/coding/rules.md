# CWapi Coding Rules

- Follow repository-local instructions such as `AGENTS.md`, `CONTRIBUTING`, project documentation and existing validation scripts when they apply. Global CWapi Skills guide workflow but do not override project-specific rules.
- Load task Skills only when relevant. Code implementation, modification or refactoring uses `coding`; bug investigation uses `debugging`; Git work uses `git`; verification uses `testing`; packaging/release work uses `release`. Multiple relevant Skills may be loaded, but do not load all Skills by default.
- Preserve the current task scope and existing external interfaces unless the task explicitly requires a change.
- For implementation work, normally inspect relevant state/code first, make the smallest necessary change second, run focused verification third, widen validation according to risk, then inspect final diff/status before Git mutations.
- One `coding_exec` should normally have one clear, verifiable logical purpose. Do not concatenate inspection, editing, testing, commit, push or release into one giant shell/PowerShell command merely to reduce calls.
- Prefer direct `command` + `argv`. When scripting is necessary, keep quoting and embedded payloads small and recoverable; avoid placing entire large source files, documents or generated data inside deeply nested command strings.
- Read narrowly before broadly. Search symbols, paths or relevant ranges before printing large files, logs or repository trees.
- A failed command is evidence. Read its exit status/output, identify the cause, change the approach, and rerun only the necessary step. Do not repeatedly submit an unchanged failed command.
- Do not repeat expensive validation when the source tree/commit has not changed and existing exact-commit evidence still applies, unless project or release policy explicitly requires another run.
- Before commit, merge, push, tag, reset, clean, branch deletion or other consequential Git operations, verify the relevant branch, HEAD, dirty state, target ref and authorization.
- If the result of a side-effecting Git or external operation is uncertain, inspect authoritative current state before retrying it.
- Close the Coding session only after the task is genuinely finished or intentionally handed off with durable workspace state preserved.
