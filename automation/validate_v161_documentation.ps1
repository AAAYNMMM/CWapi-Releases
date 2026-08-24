param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

$ActualCommit = (& git rev-parse HEAD).Trim().ToLowerInvariant()
if ($LASTEXITCODE -ne 0 -or $ActualCommit -ne $ExpectedCommit.ToLowerInvariant()) {
    throw "CWAPI_DOCS_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}
$Status = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0 -or $Status.Count -ne 0) { throw 'CWAPI_DOCS_WORKTREE_DIRTY' }

$Files = @(
    Get-Item -LiteralPath 'README.md'
    Get-Item -LiteralPath 'AGENTS.md'
    Get-ChildItem -LiteralPath 'docs' -Recurse -File -Filter '*.md'
)
foreach ($File in $Files) {
    $Lines = @(Get-Content -LiteralPath $File.FullName)
    if ($Lines.Count -gt 350) {
        throw "CWAPI_DOCS_FILE_TOO_LONG path=$($File.FullName) lines=$($Lines.Count)"
    }
    if ($File.Length -gt 20KB) {
        throw "CWAPI_DOCS_FILE_TOO_LARGE path=$($File.FullName) bytes=$($File.Length)"
    }
    if ($Lines.Count -gt 250) {
        Write-Host "CWAPI_DOCS_FILE_REVIEW path=$($File.FullName) lines=$($Lines.Count)"
    }
}

$Facts = (Get-Content -Raw -LiteralPath 'README.md') + (Get-Content -Raw -LiteralPath 'docs/PROTOCOL.md')
foreach ($Required in @('v1.6.1', '[CWapi/MCP/2]', 'cwapi-mcp/2', 'cwapi.config.v2')) {
    if (-not $Facts.Contains($Required)) { throw "CWAPI_DOCS_FACT_MISSING token=$Required" }
}
$Runnable = @(
    Get-ChildItem -LiteralPath 'automation' -Recurse -File
    Get-ChildItem -LiteralPath 'scripts' -Recurse -File
)
foreach ($File in $Runnable) {
    if ($File.Name -match 'v160|v1_6_0') { throw "CWAPI_DOCS_RETIRED_RUNNABLE path=$($File.FullName)" }
}
Write-Host "CWAPI_V161_DOCUMENTATION_PASS commit=$ActualCommit files=$($Files.Count)"
