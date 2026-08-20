param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

& (Join-Path $PSScriptRoot 'validate_v160_packaged_gui_start.ps1') -ExpectedCommit $ExpectedCommit
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
