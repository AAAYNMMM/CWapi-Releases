# CWapi v1.6.1 Web GPT Workflow

CWapi 的远程调用方通过配置的 Slack channel 发送 MCP v2 frame。CWapi 不使用 project registry；每个 repository request 自带 GitHub URL 与 exact commit。

## 标准流程

1. 调用方取得目标 GitHub repository URL 与完整 40 位 commit。
2. 在 Slack 发送唯一 `request_id` 的 `[CWapi/MCP/2][MCP_REQUEST]`。
3. CWapi 在 claim 前校验 protocol、route、scope、Token 位置与参数 shape。
4. repository 调用检查 gh readiness，准备共享 mirror 和 request-unique detached worktree。
5. stock MCP 调用进入 request-scoped ephemeral Codex thread；process tool 由 Go Core 直接处理。
6. CWapi 保存 terminal response，再投递到原 Slack thread。
7. 同 id 重投相同 fingerprint 会返回已保存 response；刷新 process 状态使用新 id。

## Process success

```text
process_start (new request id, repo+commit)
  -> Codex safe execution
  -> completed process record
  -> process_status / process_stop (new global request ids)
```

start 在 700ms 内完成则直接返回 terminal record，否则返回 stable process_id。长进程后续用 status 刷新；stop 最多同步等待 4 秒，owned cleanup 即使超时也继续。

## Permission fallback

```text
full_access local mode
  -> Codex structured PERMISSION_DENIED
  -> blocked response + 60s System Token
  -> caller sends same repo/commit/process args with new request_id + Token
  -> Core re-resolves final invocation in original dirty tree
  -> binding/policy pass, one-time consume
  -> System backend process record
```

binding mismatch 不消费 Token，修正后仍必须换新 request_id。第 4 个 active Token 返回 `SYSTEM_TOKEN_LIMIT_REACHED`，不会驱逐前三个。

## Stock MCP

- status-list 是 global 且 params 为空。
- resource/tool 可 global 或 repository。
- caller 不提供 `threadId`；CWapi 管理 ephemeral context。
- `server=cwapi` 只支持 process_start/status/stop，不进入 stock relay。

## 调用方责任

- 为每个新动作/状态快照生成新 request_id；
- Windows path 在协议中使用 `/`；
- 只在 configured channel 传递短期 Token；
- 不把 Slack response 里的 Token复制到 issue、日志或长期文档；
- System fallback 前确认该直接命令确实需要当前 Windows 用户权限。
