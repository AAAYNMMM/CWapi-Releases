# CWapi 2.0 Project Principles

```text
简单
稳定
高效
```

## Product rules

- one executable, one MCP listener, two isolated app routes；
- Coding 与 Agent 只保留各自当前所需的 runtime state，不引入额外业务模式状态机；
- reuse upstream Codex/Git behavior instead of duplicating their policy engines；
- durable workspace is the only Coding mutable source of truth；
- Agent is a bounded control bridge with request-scoped attachments, not a data lake, bulk sync channel or long-term file store；
- Desktop state is factual and bounded；
- config mutations are Service-owned, atomic and rollback-capable；
- release package contains runtime only, never user data。

## Engineering rules

- add abstraction/config/migration only for direct product value；
- validate at boundaries and use stable structured error codes；
- keep listener, request, child and workspace ownership explicit；
- test races, timeout, disconnect, rollback and shutdown paths；
- real E2E evidence complements unit tests；
- every candidate artifact is tied to one clean exact implementation commit。

## Change control

2.0 uses strict `cwapi.config.v3 / 2.0.0`; mismatched config is rejected rather than migrated implicitly.

Routine development may create commits and test-only `refs/heads/cwapi-e2e/*` branches when a guarded E2E explicitly requires them. Merge to `main`, tag, Release, force-push and remote test-ref deletion require explicit user authorization.
