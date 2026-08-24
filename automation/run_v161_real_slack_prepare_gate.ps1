param(
    [Parameter(Mandatory = $true)][ValidatePattern('^[0-9a-fA-F]{40}$')][string]$ExpectedCommit,
    [Parameter(Mandatory = $true)][string]$RuntimeSourceRoot,
    [Parameter(Mandatory = $true)][ValidatePattern('^[A-Z0-9]{9,32}$')][string]$SlackChannelID,
    [ValidateSet('safe', 'full_access')][string]$PermissionMode = 'full_access'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Arguments = @{
    ExpectedCommit = $ExpectedCommit
    RuntimeSourceRoot = $RuntimeSourceRoot
    SlackChannelID = $SlackChannelID
    PermissionMode = $PermissionMode
}
& (Join-Path $PSScriptRoot 'prepare_v161_real_slack_validation.ps1') @Arguments
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
