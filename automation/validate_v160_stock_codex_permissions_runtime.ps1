param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,

    [Parameter(Mandatory = $true)]
    [string]$CodexExecutable
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

$ActualCommit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $ActualCommit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
    throw "CWAPI_STOCK_PERMISSION_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}

$StatusBefore = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_STOCK_PERMISSION_STATUS_BEFORE_FAILED' }
if ($StatusBefore.Count -ne 0) { throw 'CWAPI_STOCK_PERMISSION_WORKTREE_DIRTY_BEFORE' }

if (-not [System.IO.Path]::IsPathRooted($CodexExecutable)) {
    throw 'CWAPI_STOCK_PERMISSION_CODEX_PATH_NOT_ABSOLUTE'
}
$ResolvedCodex = (Resolve-Path -LiteralPath $CodexExecutable).Path
$CodexInfo = Get-Item -LiteralPath $ResolvedCodex
if (-not $CodexInfo.PSIsContainer -and $CodexInfo.Length -gt 0) {
    $CodexHash = (Get-FileHash -LiteralPath $ResolvedCodex -Algorithm SHA256).Hash.ToLowerInvariant()
} else {
    throw 'CWAPI_STOCK_PERMISSION_CODEX_NOT_FILE'
}
$ExpectedCodexHash = '51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5'
if ($CodexHash -ne $ExpectedCodexHash) {
    throw "CWAPI_STOCK_PERMISSION_CODEX_HASH_MISMATCH expected=$ExpectedCodexHash actual=$CodexHash"
}

$PreviousRunGate = [Environment]::GetEnvironmentVariable('CWAPI_RUN_STOCK_CODEX_PERMISSION_RUNTIME', 'Process')
$PreviousCodexExe = [Environment]::GetEnvironmentVariable('CWAPI_TEST_CODEX_EXE', 'Process')
try {
    $env:CWAPI_RUN_STOCK_CODEX_PERMISSION_RUNTIME = '1'
    $env:CWAPI_TEST_CODEX_EXE = $ResolvedCodex

    & go test ./internal/codex -run '^TestStockCodexPermissionProfilesRuntime$' -count=1 -timeout 120s -v
    if ($LASTEXITCODE -ne 0) {
        throw "CWAPI_STOCK_PERMISSION_RUNTIME_TEST_FAILED exit=$LASTEXITCODE"
    }
}
finally {
    if ($null -eq $PreviousRunGate) {
        Remove-Item Env:CWAPI_RUN_STOCK_CODEX_PERMISSION_RUNTIME -ErrorAction SilentlyContinue
    } else {
        $env:CWAPI_RUN_STOCK_CODEX_PERMISSION_RUNTIME = $PreviousRunGate
    }
    if ($null -eq $PreviousCodexExe) {
        Remove-Item Env:CWAPI_TEST_CODEX_EXE -ErrorAction SilentlyContinue
    } else {
        $env:CWAPI_TEST_CODEX_EXE = $PreviousCodexExe
    }
}

$FormatOutput = @(& gofmt -l 'internal/codex/permissions_runtime_integration_test.go' 2>&1)
if ($LASTEXITCODE -ne 0) {
    $FormatOutput | Select-Object -First 40 | ForEach-Object { Write-Host $_ }
    throw "CWAPI_STOCK_PERMISSION_GOFMT_EXEC_FAILED exit=$LASTEXITCODE"
}
if ($FormatOutput.Count -ne 0) {
    $FormatOutput | Select-Object -First 40 | ForEach-Object { Write-Host $_ }
    throw 'CWAPI_STOCK_PERMISSION_GOFMT_REQUIRED'
}

& git diff --check
if ($LASTEXITCODE -ne 0) { throw "CWAPI_STOCK_PERMISSION_DIFF_CHECK_FAILED exit=$LASTEXITCODE" }

$StatusAfter = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_STOCK_PERMISSION_STATUS_AFTER_FAILED' }
if ($StatusAfter.Count -ne 0) {
    $StatusAfter | ForEach-Object { Write-Host $_ }
    throw 'CWAPI_STOCK_PERMISSION_WORKTREE_DIRTY_AFTER'
}

Write-Host "CWAPI_STOCK_CODEX_PERMISSION_RUNTIME_PASS commit=$ActualCommit codex_sha256=$CodexHash"
