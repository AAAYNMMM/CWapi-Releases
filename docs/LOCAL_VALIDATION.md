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

## 结果判断

对明确发起的某次新交付 gate，通过必须同时满足：command exit 0、gate 明确 PASS marker、source status clean、HEAD 等于 expected commit。只看到 GUI 或只看到 Slack online 不算该次 gate 完成；该判断不改变已经记录的 v1.6.1 `DONE` 状态。
