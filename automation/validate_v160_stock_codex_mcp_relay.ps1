param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

$ActualCommit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $ActualCommit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
    throw "CWAPI_STOCK_MCP_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}
$StatusBefore = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_STOCK_MCP_STATUS_BEFORE_FAILED' }
if ($StatusBefore.Count -ne 0) { throw 'CWAPI_STOCK_MCP_WORKTREE_DIRTY_BEFORE' }

$RetiredPaths = @(
    'internal/mcptools',
    'internal/gateway/mcp_workspace.go',
    'internal/gateway/mcp_git.go',
    'internal/gateway/mcp_process.go',
    'internal/gateway/mcp_automation.go',
    'internal/gateway/mcp_files.go',
    'internal/gateway/mcp_resources.go',
    'internal/state/mcp_workspaces.go',
    'internal/codex/mcp_host_runtime_integration_test.go',
    'internal/codex/mcp_host_source_repository_integration_test.go'
)
foreach ($Path in $RetiredPaths) {
    if (Test-Path -LiteralPath $Path) {
        throw "CWAPI_STOCK_MCP_RETIRED_PATH_PRESENT path=$Path"
    }
}

$Client = Get-Content -Raw -LiteralPath 'internal/codex/client.go'
foreach ($Required in @('mcpServerStatus/list', 'mcpServer/resource/read', 'mcpServer/tool/call', 'thread/start')) {
    if (-not $Client.Contains($Required)) {
        throw "CWAPI_STOCK_MCP_CLIENT_ROUTE_MISSING token=$Required"
    }
}
foreach ($Forbidden in @('"turn/start"', '"command/exec"', '"fs/readFile"', '"fs/writeFile"')) {
    if ($Client.Contains($Forbidden)) {
        throw "CWAPI_STOCK_MCP_CLIENT_FORBIDDEN_METHOD token=$Forbidden"
    }
}

$HostSource = Get-Content -Raw -LiteralPath 'internal/codex/mcp_host.go'
foreach ($Required in @('"thread/start"', 'CallMCP')) {
    if (-not $HostSource.Contains($Required)) {
        throw "CWAPI_STOCK_MCP_HOST_CONTEXT_MISSING token=$Required"
    }
}
if ($HostSource -notmatch '"ephemeral"\s*:\s*true') {
    throw 'CWAPI_STOCK_MCP_HOST_CONTEXT_MISSING token=ephemeral:true'
}
if ($HostSource.Contains('"turn/start"')) { throw 'CWAPI_STOCK_MCP_HOST_MODEL_TURN_PRESENT' }

$ProductionSource = (@(
    Get-ChildItem -LiteralPath 'internal' -Recurse -File -Filter '*.go' | Where-Object { $_.Name -notlike '*_test.go' } | ForEach-Object { Get-Content -Raw -LiteralPath $_.FullName }
) -join "`n")
foreach ($Forbidden in @('cwapi-dev', 'workspace.open', 'workspace.status', 'workspace.close', 'git.rev_parse', 'git.status', 'test.run', 'build.run', 'automation.run', 'resources.read')) {
    if ($ProductionSource.Contains($Forbidden)) {
        throw "CWAPI_STOCK_MCP_CUSTOM_TOOL_PRESENT token=$Forbidden"
    }
}

$GoFiles = @(Get-ChildItem -LiteralPath 'internal' -Recurse -File -Filter '*.go') + @(Get-Item -LiteralPath 'app.go')
$Unformatted = @(& gofmt -l @($GoFiles.FullName) 2>&1)
if ($LASTEXITCODE -ne 0) {
    $Unformatted | Select-Object -First 80 | ForEach-Object { Write-Host $_ }
    throw "CWAPI_STOCK_MCP_GOFMT_EXEC_FAILED exit=$LASTEXITCODE"
}
if ($Unformatted.Count -ne 0) {
    $Unformatted | Select-Object -First 200 | ForEach-Object { Write-Host $_ }
    throw 'CWAPI_STOCK_MCP_GOFMT_REQUIRED'
}

& go test ./internal/codex ./internal/gateway ./internal/app ./internal/protocol ./internal/state
if ($LASTEXITCODE -ne 0) { throw "CWAPI_STOCK_MCP_FOCUSED_TEST_FAILED exit=$LASTEXITCODE" }
& go test ./...
if ($LASTEXITCODE -ne 0) { throw "CWAPI_STOCK_MCP_FULL_TEST_FAILED exit=$LASTEXITCODE" }
& git diff --check
if ($LASTEXITCODE -ne 0) { throw "CWAPI_STOCK_MCP_DIFF_CHECK_FAILED exit=$LASTEXITCODE" }

$StatusAfter = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_STOCK_MCP_STATUS_AFTER_FAILED' }
if ($StatusAfter.Count -ne 0) {
    $StatusAfter | ForEach-Object { Write-Host $_ }
    throw 'CWAPI_STOCK_MCP_WORKTREE_DIRTY_AFTER'
}

Write-Host "CWAPI_STOCK_CODEX_MCP_RELAY_PASS commit=$ActualCommit"
