# CWapi v1.6.1 验收标准

当前状态：v1.6.1 任务已完成，P0-P7 为 `DONE`。本文件保存回归验收标准，不是当前待办列表。

只有用户明确要求生成新的正式 portable/Release 时，该新交付才重新执行完整验收：所有结果来自同一 clean source commit 和固定 packaged runtime，且真实 Windows/Codex/Slack 证据不能由文档或单元测试替代。文档差异或仅开发者 source gate 的结果不能单独改变当前 `DONE` 状态。

## Portable 用户交付约定

- ZIP 解压到任意用户可写路径后可直接运行 `CWapi.exe`，不依赖盘符、目录名或安装位置；
- portable 自带 Codex、MinGit、Node、Playwright MCP 与 browser runtime；不要求用户安装 Go、Node、Git、Wails 或设置项目环境变量；
- 用户只提供 Slack App/Bot Token 与 channel ID，并安装 GitHub CLI；private repository 复用当前 Windows 用户已有的 `gh` 凭据；
- `CWapi-data` 在程序旁创建，整体移动解压目录后使用新位置自己的数据与 bundled runtime；
- 正式 ZIP 不含用户配置、凭据、Token、数据库、日志、仓库、浏览器 profile、用户名、机器名或构建机绝对路径；stage 与 ZIP 的目录白名单、敏感文件名、credential shape、private key、active secret 和 build identity 扫描全部 PASS。

## Source gate

- gofmt、`go vet ./...`、`go test ./...`、`git diff --check`；
- frontend clean install、13+ tests、typecheck、production build；
- Wails windows/amd64 build；
- 所有手写 production/frontend/automation 文件 <=400 行目标、<=500 hard；Markdown <=350 hard；
- 无 project layer、GUI prefs、旧 Node process MCP、`mcp_servers.cwapi`、ProcessMCPReady、v1/config compatibility path。

## P0 runtime gate

固定 Codex/Node 必须真实证明：

- model-free `command/exec`，无 thread/turn；
- native 与 cmd/bat final executable+argv+cwd；
- safe current tree 可写，cross-tree/mirror/external/temp 拒绝，nested child 同 sandbox；
- short stdout/stderr/exit；long descendant 由 Job Object 收口；
- full structured denial 后同 invocation System 成功；
- per-execution CODEX_HOME 并发隔离；
- Codex/System/Git credential helper secret env canary 隔离。

## Protocol / repository

- request/response/event/Subject 全部 v2；old v1 返回 guidance；
- route/scope、outer shape、threadId、Token type/position 在 claim 前拒绝；
- private GitHub repo 与第二 repo exact 40hex commit；
- same repo+commit 两个 request 使用不同 mutable trees；
- global context 不解析 repository 凭据、不建 Git tree、safe root 不扩大；
- URL normalization、commit object、cwd traversal/reparse、PATH drift、batch wrapper drift 对抗测试。

## Process / authorization

- Codex/System 共享一个 registry；8 active、48 terminal、trim、700ms、8KiB tails；
- public record 字段精确，无 Token/argv/absolute internal path；
- cleanup-once、stop terminal 幂等、4s stop timeout 后 owned cleanup 继续；
- full denial -> Token -> 新 id System；dirty tree 保留；
- 3 Token 并存、第 4 个拒绝、TTL、redelivery 不延时、一次性 consume；
- binding mismatch 不消费；safe/update/issuance/consume race 线性化；
- permanent policy 对 direct target 生效，nested shell/script 不做虚假解析。

## Session / desktop / package

- same-session duplicate/redelivery；restart 清 request/Token/process；
- 启动在 authorization/runtime 前原子重置 permission mode 为 `safe`；`full_access` 不跨 restart，写盘失败不得沿旧模式启动，Slack channel 保持；
- second instance 无 config/state/Core side effect；
- stale reparse 外部 marker 不变；
- Wails public snapshot schema-aware Token redaction，unrelated 64hex 保留；
- GUI 固定 `375 × 690`、不可拉伸且只有一个页面；窗口与标题栏为全不透明宇宙渐变，不使用 Windows backdrop、layered alpha 或额外 native 背景窗口；四个主卡片有应用内深色渐变、柔和虚化与高光边界，最新记录无投影，另一应用获得焦点后外观不变；无 GitHub CLI 状态/刷新、Diagnostics、Settings/About page；
- process monitor 来自 Go registry，active process 才显示 `[STOP]`；不伪造脚本步骤或百分比；
- 最新记录不可滚动且无边缘阴影，只显示 process state/output、execution event、runtime error 或本地 mutation 中时间戳最新的一条；routine runtime info 不进入 GUI，operational error 以数据化字段进入该区域，启动竞态不残留 `CORE_NOT_STARTED`；
- permission mode 与 Slack 配置集成在同一页；
- portable first-run、relocation、manifest/hash、隐私扫描、无用户数据/gh/legacy CWapi MCP。

## Real Slack

configured channel 上至少完成：

- Codex success；
- full denial -> visible short Token -> 新 id System success；
- private repo、second repo、3 Token/第 4 个、long/status refresh/stop；
- same-session duplicate/redelivery；
- probe evidence 中 Token 为 `[REDACTED]`。

真实请求模板见 `automation/v161_slack_e2e_requests.example.json`；expectation 模板见 `automation/v161_slack_e2e_expectations.example.json`。

## Closeout

当前 P0-P7 closeout 已完成。以后明确要求新的正式交付时，再记录该次 source commit、portable/Codex hash、Slack request/process IDs 与 gate 输出，并确认 worktree clean、HEAD 不变。不自动 tag/Release。
