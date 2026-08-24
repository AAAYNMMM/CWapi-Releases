param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,
    [Parameter(Mandatory = $true)][string]$CodexExecutable,
    [Parameter(Mandatory = $true)][string]$NodeExecutable
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

$ActualCommit = (& git rev-parse HEAD).Trim().ToLowerInvariant()
if ($LASTEXITCODE -ne 0 -or $ActualCommit -ne $ExpectedCommit.ToLowerInvariant()) {
    throw "CWAPI_CODEX_RUNTIME_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}
$StatusBefore = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0 -or $StatusBefore.Count -ne 0) { throw 'CWAPI_CODEX_RUNTIME_WORKTREE_DIRTY_BEFORE' }

$CodexExecutable = (Resolve-Path -LiteralPath $CodexExecutable).Path
$NodeExecutable = (Resolve-Path -LiteralPath $NodeExecutable).Path
$ExpectedCodexHash = '51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5'
$CodexHash = (Get-FileHash -LiteralPath $CodexExecutable -Algorithm SHA256).Hash.ToLowerInvariant()
if ($CodexHash -ne $ExpectedCodexHash) {
    throw "CWAPI_CODEX_RUNTIME_HASH_MISMATCH expected=$ExpectedCodexHash actual=$CodexHash"
}
if (-not (Test-Path -LiteralPath $NodeExecutable -PathType Leaf)) { throw 'CWAPI_CODEX_RUNTIME_NODE_MISSING' }

$Saved = @{}
foreach ($Name in @('CWAPI_RUN_V161_CAPABILITY_RUNTIME', 'CWAPI_RUN_STOCK_CODEX_PERMISSION_RUNTIME', 'CWAPI_TEST_CODEX_EXE', 'CWAPI_TEST_NODE_EXE')) {
    $Saved[$Name] = [Environment]::GetEnvironmentVariable($Name, 'Process')
}
try {
    $env:CWAPI_RUN_V161_CAPABILITY_RUNTIME = '1'
    $env:CWAPI_RUN_STOCK_CODEX_PERMISSION_RUNTIME = '1'
    $env:CWAPI_TEST_CODEX_EXE = $CodexExecutable
    $env:CWAPI_TEST_NODE_EXE = $NodeExecutable
    & go test ./internal/codex -run '^(TestV161CodexProcessCapabilities|TestStockCodexPermissionProfilesRuntime)$' -count=1 -timeout 170s -v
    if ($LASTEXITCODE -ne 0) { throw "CWAPI_CODEX_RUNTIME_TEST_FAILED exit=$LASTEXITCODE" }
} finally {
    foreach ($Name in $Saved.Keys) {
        [Environment]::SetEnvironmentVariable($Name, $Saved[$Name], 'Process')
    }
}

& git diff --check
if ($LASTEXITCODE -ne 0) { throw "CWAPI_CODEX_RUNTIME_DIFF_CHECK_FAILED exit=$LASTEXITCODE" }
$StatusAfter = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0 -or $StatusAfter.Count -ne 0) { throw 'CWAPI_CODEX_RUNTIME_WORKTREE_DIRTY_AFTER' }
Write-Host "CWAPI_V161_CODEX_RUNTIME_PASS commit=$ActualCommit codex_sha256=$CodexHash"
