# CWapi v1.6.1 本地验证

当前 v1.6.1 任务状态为 `DONE`。本文件只供维护者做回归，不是最终用户的安装或环境配置步骤。只有明确要求生成新的正式 portable/Release，或实际修改影响打包与运行链路时，才重新执行相应完整 gate；文档差异或单独的源码检查不会自动重开已完成任务。

## 快速回归

```powershell
gofmt -l (Get-ChildItem -Recurse -Filter *.go)
go vet ./...
go test ./...
git diff --check

cd frontend
npm ci
npm test
npm run build
```

## 开发者源码回归（clean commit）

```powershell
$commit = (git rev-parse HEAD).Trim()
.\automation\validate_v161_source.ps1 -ExpectedCommit $commit
.\automation\validate_v161_documentation.ps1 -ExpectedCommit $commit
```

## 固定 Codex gate

```powershell
.\automation\validate_v161_codex_runtime.ps1 `
  -ExpectedCommit $commit `
  -CodexExecutable <portable-root>\runtime\codex\current\bin\codex.exe `
  -NodeExecutable <portable-root>\runtime\node\node.exe
```

单次外部 gate 总等待不超过 3 分钟。超时先定位具体 runtime/网络步骤，不通过无限等待掩盖问题。

## Windows 运行时探测

真实 Slack / CWapi process 回归时，必须以 **CWapi 进程实际看到的环境** 为准，不要用维护者当前终端的 PATH 代替，也不要把某次机器上的安装路径固化成通用验证步骤。

出现 `process invocation could not be resolved` 或某个依赖缺失时，按以下顺序处理：

1. **CWapi 管理环境优先。** 先检查当前 portable/runtime、受控 tools、CWapi 自己缓存并管理的运行时，存在时使用对应绝对路径；
2. **再探测本机环境。** 使用当前 CWapi 可见 PATH、系统 launcher、`Get-Command` / `where` 或必要的受限目录探测找到用户已经安装的工具，并记录真实版本与可执行文件路径；
3. **两边都没有就停止路径猜测。** 明确报告缺少的依赖，不要继续尝试大量可能路径；
4. 用户可切换 CWapi 到 `FULL` 权限模式，由 Web GPT 通过 CWapi 安装依赖；或者用户选择手动安装；
5. Web GPT 自动安装只在用户明确允许/切换 `FULL` 后执行。安装后重新探测实际路径和版本；如果当前 CWapi 的 frozen PATH 尚未更新，则直接使用新安装的绝对路径，或在需要时重启 CWapi 后再验证；
6. repository 内脚本优先使用仓库相对 command，例如 `tools/env-probe.cmd`，不要只依赖切换 cwd 后的 basename 解析；
7. MCP JSON 中 Windows path 优先使用 `/`，避免未转义 `\` 导致 claim 前 JSON 解析失败。

例如 Python 的验证不应固定写成 `C:/WINDOWS/py.exe`。正确做法是先确认 CWapi 自管目录里是否已有可用 Python；没有再探测本机的 `python` / `python3` / `py` launcher 或实际安装目录；仍不存在才进入 `FULL` 自动安装或用户手动安装分支。

这类探测只用于确认执行环境，不应把用户机器绝对路径写入正式 portable manifest、Release 产物或长期凭据记录。

## Playwright / Slack 文件回归

需要验证浏览器截图真的能经 Slack 到达调用方时，不能只检查“本地截图文件已生成”。应验证完整链路：

```text
browser_take_screenshot (不传 filename)
  -> MCP type=image content
  -> CWapi externalizeMCPResult
  -> Slack external file upload
  -> response.resources 出现 Slack file reference
  -> 原 request thread 中存在真实 Slack File
```

指定 `filename` 后只得到本地 path 文本，不算 Slack 文件传输通过。CWapi 不根据普通文本 path/URI 自动读取本地文件。

stock MCP 使用 request-scoped ephemeral context。涉及导航、输入、点击、断言、截图的 E2E 优先在一次 Playwright 调用中完成；如果拆成多个 request，每个后续 request 必须自行重新建立页面状态。

## Windows package

```powershell
.\automation\stage_v161_portable.ps1 `
  -ExpectedCommit $commit `
  -RuntimeSourceRoot <portable-root>
.\automation\validate_v161_packaged_gui_start.ps1 -ExpectedCommit $commit
.\automation\validate_v161_portable_privacy.ps1 `
  -ExpectedCommit $commit `
  -PortableRoot .\build\stage\CWapi-v1.6.1 `
  -ZipPath .\build\stage\CWapi-v1.6.1.zip
```

## Real Slack

```powershell
.\automation\prepare_v161_real_slack_validation.ps1 `
  -ExpectedCommit $commit `
  -RuntimeSourceRoot <portable-root> `
  -SlackChannelID <channel> `
  -PermissionMode full_access

.\automation\validate_v161_real_slack_mcp.ps1 `
  -ExpectedCommit $commit `
  -ExecutablePath .\build\validation\CWapi-v1.6.1\CWapi.exe `
  -ExpectationsPath <expectations.json>
```

启动前确认没有同 single-instance ID 的旧 CWapi 进程。测试只停止本轮明确启动的 PID，并在 finally 中恢复/清理 gate 数据。

## 等待边界

- 单次连续等待/轮询外部编译、Runner、Slack response 或长进程累计最多 3 分钟；
- 3 分钟仍无 terminal result 时立即结束本轮等待并报告当前 request/process 状态；
- 不允许通过重复短轮询把累计等待扩展到 3 分钟以上；
- 已有 stable process_id 时用新的 global `process_status` request_id 刷新，不重复 `process_start`；
- 确认依赖不存在后不进入等待循环，应直接进入 `FULL` 安装或用户手动安装决策。

## 结果判断

对明确发起的某次新交付 gate，通过必须同时满足：command exit 0、gate 明确 PASS marker、source status clean、HEAD 等于 expected commit。只看到 GUI 或只看到 Slack online 不算该次 gate 完成；该判断不改变已经记录的 v1.6.1 `DONE` 状态。
