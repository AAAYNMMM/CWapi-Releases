# CWapi 2.0 Runtime Observability

CWapi 2.0 keeps observability intentionally bounded. It does not create a conversation transcript store or a persistent command-output log.

## Desktop snapshot

The Service exposes current structured state only:

- Service/MCP listener state and source commit；
- Codex runtime/access profile/active Coding handles；
- workspace count and maintenance summaries；
- Agent Provider address/state；
- bridge Offline/Ready/Busy, pending/claimed/completed；
- latest bounded error code/message。

## Coding output

`coding_exec` returns bounded stdout/stderr for that command. `coding_status` returns local Git/workspace truth such as HEAD, tracking head, dirty, divergence and last error. Full app-server traffic, model reasoning and credentials are not copied into an application log.

## Agent output

Broker state records request IDs, counts, lifecycle state and bounded error codes. Normal operation does not persist full `messages`, answer content, tool schemas, tool arguments or tool results.

Agent attachment bytes are request-scoped temporary data, not observability output. Runtime snapshots expose only bounded attachment metadata carried by the active exchange; no attachment body is copied into logs or transcripts. Terminal request paths, broker shutdown and the next startup remove the temporary attachment tree.

## MCP instructions

First-contact Server Instructions are static product guidance, not runtime conversation storage. They do not contain user prompt content, tool results, repository content or credentials.

## Redaction

GUI masks MCP tokens and Agent API key. Errors and package evidence must never echo bearer secrets. Candidate privacy audit rejects config, credentials, logs, workspaces and machine-specific paths.

## Diagnostics

Development gate output is written only to local ignored validation directories such as `.cwapi-v2-gate/` or an explicit external output root. These artifacts are never included in portable staging.
