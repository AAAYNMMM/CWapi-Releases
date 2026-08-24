# CWapi 项目原则

```text
简单
稳定
高效
```

## 简单

- 单用户、单 Windows 主机、单 Slack channel。
- Web 调用方负责选择 repository、commit、MCP tool 与命令。
- CWapi 负责 protocol、exact workspace、Codex/System lifecycle、idempotency 与 delivery。
- 不建设 project registry、RBAC、多租户、migration framework、第二套 Git/Build/Test tool 平台。
- process tool 是三个 Gateway virtual tools，不维护 Node process server。

## 稳定

- config v2 shape 固定，旧版拒绝而非猜测迁移。
- invalid route/Token 在 request claim 和 workspace side effect 前拒绝。
- 每个 repository request 独立 mutable tree；终止时唯一 owner cleanup。
- Codex/System 共用一个 process registry 与 canonical child environment。
- duplicate request 不重复副作用；状态刷新显式使用新 id。
- Service 只在 SingleInstanceLock 成功后构造。
- stale cleanup 不跟随 reparse point；局部失败降级而非杀死无关功能。

## 高效

- bare mirror 按 repository identity 共享；Git mutation 由一个粗 mutex 串行。
- global stock MCP permission generation/thread 可复用。
- repository thread 只存在于本次 request，避免跨 tree 状态污染。
- 进程只保存 8KiB stdout/stderr tail，不写 per-process 完整日志。
- registry 只保留 8 active + 48 terminal。
- GUI snapshot 和 refresh 不重启 Codex 或执行 Git。

## 不做的事

- 不解析 nested shell/script 语义；
- 不从 GitHub 自动修改 repository；
- 不把 gh 当 secret store 或打进 portable；
- 不跨 restart 恢复 Token/process/request；
- 不自动 tag、Release 或发布；
- 不以企业化抽象替代清晰的小型 Go 定义。
