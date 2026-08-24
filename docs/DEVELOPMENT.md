# CWapi v1.6.1 开发规则

最高优先级：**简单、稳定、高效**。

## Architecture contract

- Web GPT：理解需求、读写 GitHub、决定测试和修改方案；
- Slack：只做 transport；
- CWapi Go Core / Gateway：MCP v2 校验、repository identity、exact-commit worktree、幂等、process lifecycle、permission fallback、delivery；
- stock Codex MCP：request-scoped ephemeral MCP context；
- System backend：仅在 `full_access` 下发生真实结构化权限拒绝，并通过短期一次性 System Token 授权后执行。

不要重新引入已删除的 project registry、`project_id`、Node CWapi process MCP、旧 MCP v1 或自建 Git/Build/Test 平台。

## Repository contract

repository-scoped request 使用：

```text
repository_url
expected_commit  # full 40hex
```

CWapi 按 GitHub owner/repo identity 维护共享 mirror，为每个 repository request 创建独立 detached worktree。

caller 不提供本机 worktree path，也不提供 Codex thread id。

## Process contract

Go Core 直接提供：

```text
process_start
process_status
process_stop
```

- `process_start` repository-scoped；
- status/stop global；
- Codex/System 共用同一个 process registry；
- 不在 Slack 层解释 shell 命令危险性；
- executable 解析、workspace scope、reparse、安全环境和 System fallback 必须在 Core 边界完成；
- child process 不继承 CWapi/Slack/Codex secret。

## Permission contract

产品权限只有：

```text
safe
full_access
```

每次 Service 启动先原子恢复 `safe`；`full_access` 只在当前运行中存在。

FULL 不等于无保护：

```text
full_access
-> Codex-first
-> structured PERMISSION_DENIED
-> short-lived System Token
-> final invocation binding
-> System backend
```

System Token 最多 3 个、60 秒、一次性使用，不得通过日志、GUI snapshot 或普通文档暴露。

## Runtime discovery

外部工具发现遵循：

```text
CWapi-managed runtime/tools/cache
-> local installed environment visible to CWapi
-> absent: FULL install or user manual install
-> re-detect executable/version
```

不要把维护者机器上的固定 Python、Node、JDK 等路径写进通用实现或文档。

## Slack delivery

terminal truth 先 durable，再发 Slack。Slack delivery 失败不得自动重跑可能有副作用的 tool。

Slack File 只外置 MCP 已经返回的内容，例如 image/blob/resource/大文本。普通日志或文本里出现本地 path/URI 不触发任意文件读取。

## Stock MCP / Playwright

stock MCP context 是 request-scoped ephemeral。不要设计依赖跨 request 浏览器 session 连续性的功能。

多步 E2E 如需连续页面状态，应在一次 MCP 调用中完成或显式重建状态。

## Stability

优先保证：

- request claim 前严格验证 v2 route/scope/shape；
- same request id + same fingerprint 幂等；
- ambiguous side effect 不自动 replay；
- request-unique worktree 正确清理；
- owned-process-only cleanup；
- bounded process registry / logs / tails；
- startup reset 顺序稳定；
- Slack reconnect 只恢复当前 runtime session；
- secret redaction 与 child env isolation。

## Code modularity

- 人工维护 production/frontend/automation 目标 `<=400` 行；
- 硬上限 `500` 行；
- generated/vendor 文件可例外，但不放手写业务逻辑；
- 可取消/超时操作传播 `context.Context`；
- shared mutable state 有明确 owner；
- config/protocol/process record 使用强类型合同。

## GUI

v1.6.1 GUI 是固定 `375 × 690` 单页窗口。

不要恢复 Settings / Diagnostics / About / Projects 等旧路由。GUI 只观察真实 Core/Slack/Codex/process 状态，并通过 typed Service mutation 修改 permission 与 Slack 配置。

## Validation

按改动范围执行必要回归。涉及正式 portable/Release 时，使用 v1.6.1 source、Codex、GUI、package、privacy 与真实 Slack gate。

外部 gate/Slack/process 单次连续等待最多 3 分钟。到上限报告当前状态，不通过重复短轮询无限延长等待。

完整维护者入口见 [`LOCAL_VALIDATION.md`](LOCAL_VALIDATION.md) 与 [`RELEASE_CHECKLIST.md`](RELEASE_CHECKLIST.md)。
