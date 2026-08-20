# CWapi v1.6.0 验收标准

v1.6.0 产品功能与真实 Web GPT/Slack 工作流已经完成验收。公开发行包必须从发行仓库的 clean exact commit 重新构建并产生新的 same-commit 证据，不能沿用其他仓库或旧 artifact 的 commit/hash。

## Source

- expected commit == actual commit；
- validation 前后 clean；
- `gofmt -d` clean；
- `go test ./...` PASS；
- frontend type/test/build PASS；
- Wails Windows build PASS；
- Wails generated bindings 与当前 Go snapshot types 一致；
- `git diff --check` PASS。

## Stock Codex relay

- packaged executable SHA-256 匹配 `config/codex-runtime.lock.json`；
- `codex app-server --stdio` initialize 成功；
- 允许 `mcpServerStatus/list`；
- 允许 `mcpServer/resource/read`；
- 允许 `mcpServer/tool/call`；
- caller-supplied `threadId` 被拒绝；
- context 使用 `thread/start` + `ephemeral=true`；
- 正常 relay 无 `turn/start`；
- source 中没有 custom `cwapi-dev` / workspace/test/build/automation Tool 平台；
- production CODEX_HOME 只由 CWapi runtime generator 生成，不再存在第二套 capability catalog/template policy。

## Permission integration

- 默认 mode = safe；
- safe -> `cwapi-safe`；
- full_access -> `cwapi-full-access`；
- `thread/start.permissions` 与当前 mode 一致；
- 项目列表与 CWapi data root 进入 safe workspace roots；
- permission/project 变化后 context fingerprint 变化；
- `cwapi-full-access` 不使用 `:danger-full-access`；
- base `rules/default.rules` 存在并可被 stock Codex 加载；
- system-path deny 存在；
- secret/idempotency/owned-process 约束不回归。

## Exact-commit workspace

- tool/resource 请求携带 `project_id + expected_commit` 时，CWapi 只解析已配置项目；
- mirror fetch 后必须证明目标 40-char commit 存在；
- worktree 使用 detached exact commit；
- context CWD 指向该 worktree；
- prepared HEAD 必须等于 `expected_commit`；
- portable runtime 优先使用随包 `runtime/git/cmd/git.exe`，不要求目标机器预装 Git；
- workspace cleanup 由 CWapi 生命周期负责。

## Slack

真实链路：

```text
Web GPT -> Slack -> CWapi -> stock Codex app-server -> MCP server -> result -> Slack
```

至少验证：

- external request -> terminal response；
- `project_id + expected_commit` 原样进入真实请求并建立 exact-commit context；
- duplicate same request 不重复调用；
- request_id conflict 明确拒绝；
- Slack reconnect 后 terminal response 可重投；
- unsupported method 明确拒绝；
- `MCP_CANCEL` 不是 v1.6.0 protocol family，不提供虚假 cancellation contract；
- `cwapi/process_start(command, argv)` 通过真实 Slack 在 exact-commit CWD 执行并返回 marker；
- Playwright `browser_navigate` 通过 stock `mcpServer/tool/call`；
- Playwright `browser_take_screenshot` 结果至少产生一个 Slack File resource；
- 长文本/日志、图片、resource text/blob 的 external-file delivery 不重放 MCP tool；
- 单 artifact 8 MiB、单 response 16 artifact 上限明确生效，超限失败而非静默截断。

真实请求样例：`automation/v160_slack_e2e_requests.example.json`；对应 gate expectations：`automation/v160_slack_e2e_expectations.example.json`。

## Runtime recovery

- app-server crash 后可重建；
- 同一 CWapi 进程内 Slack reconnect 后，已 terminal response 可重投且不重跑 tool；
- CWapi 重启后不显示、不重放上一进程任务或 Slack history；
- ambiguous side-effect tool 不自动 replay；
- Slack File 上传/完成失败不得触发 MCP tool 重放；
- shutdown 不遗留 CWapi-owned Codex process。

## MCP server trust

对实际启用的 local MCP server 必须记录并验证：

- 来源；
- 启动方式；
- 是否运行于 Codex-managed environment/sandbox；
- 是否支持 sandbox-state / permission elicitation；
- 若都不支持，明确把它作为独立受信任程序，而不是假称 thread profile 已限制它。

v1.6.0 当前 packaged Playwright MCP 必须按这个独立 trust boundary 验收。不得把“由 app-server 启动”自动等同于“继承 thread filesystem/execpolicy sandbox”。

packaged `cwapi` command MCP 还必须验证：

- `process_start(command, argv, cwd?)` 经 stock relay 可用；
- 正式 Web GPT 请求统一使用 `C:/...` Windows 路径；实现层仍兼容 `C:\\...`，并验证带空格路径、`.venv/Scripts/python.exe` 与 `node_modules/.bin/*.cmd` shim；
- command 初始 CWD 等于 prepared exact-commit workspace 或其子目录；
- quick command 返回真实 exit code/stdout/stderr；
- long command 可 `process_status` / `process_stop`；
- duplicate request 不重复启动；
- stop/shutdown 只结束 owned process tree；
- app-server 与 command 子进程环境不含 CWapi/Slack/Codex secret；
- 文档明确该 server 以当前 Windows 用户权限运行，不受 thread profile sandbox。

## Portable package

正式 portable 必须包含并验证：

- `CWapi.exe`；
- stock `runtime/codex/current/bin/codex.exe`；
- MinGit `runtime/git/cmd/git.exe`；
- pinned Node；
- pinned Playwright MCP；
- pinned Chromium headless shell；
- `portable-manifest.json` 中 source commit / Codex pin / Git 信息一致；
- 不包含 Slack token、用户 state/log/result、browser profile/session 或验证临时数据；
- `CWapi.exe` build metadata 包含 `-trimpath=true`，二进制不含构建机用户目录或源码绝对路径；
- 从不同盘符、含空格与非 ASCII 字符的路径，以及无关 process working directory 启动成功；
- relocation 后 Codex、Git、Node、MCP、browser 路径仍从 executable 安装根解析。

## Documentation

当前 README / Architecture / Protocol / Codex / Runtime / Security / GUI / Operations / Stage Plan 不得再把以下旧内容写成当前实现：

```text
Private patched Codex Toolhost
cwapi-dev
workspace.open/status/close
git.rev_parse/status
test.run
build.run
automation.run
fs.read/hash/collect
process.status/cancel
resources.read
MCP_CANCEL
codex-capabilities.yaml policy catalog
```

历史说明只留在 Git history/CHANGELOG。
