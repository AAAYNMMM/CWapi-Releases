param(
    [Parameter(Mandatory = $true)][ValidatePattern('^[0-9a-fA-F]{40}$')][string]$ExpectedCommit,
    [Parameter(Mandatory = $true)][string]$ExpectationsPath,
    [ValidateRange(30, 150)][int]$TimeoutSeconds = 120
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$Arguments = @{
    ExpectedCommit = $ExpectedCommit
    ExecutablePath = (Join-Path $RepoRoot 'build\validation\CWapi-v1.6.1\CWapi.exe')
    ExpectationsPath = $ExpectationsPath
    TimeoutSeconds = $TimeoutSeconds
}
& (Join-Path $PSScriptRoot 'validate_v161_real_slack_mcp.ps1') @Arguments
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
