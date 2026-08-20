param(
    [Parameter(Mandatory = $true)][ValidatePattern('^[0-9a-fA-F]{40}$')][string]$ExpectedCommit,
    [Parameter(Mandatory = $true)][string]$ExpectationsPath,
    [ValidateRange(30, 1800)][int]$TimeoutSeconds = 300
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ExecutablePath = Join-Path $RepoRoot 'build\validation\CWapi-v1.6.0\CWapi.exe'

& (Join-Path $PSScriptRoot 'validate_v160_real_slack_mcp.ps1') `
    -ExpectedCommit $ExpectedCommit `
    -ExecutablePath $ExecutablePath `
    -ExpectationsPath $ExpectationsPath `
    -TimeoutSeconds $TimeoutSeconds
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
