param(
    [Parameter(Mandatory = $true)][ValidatePattern('^[0-9a-fA-F]{40}$')][string]$ExpectedCommit,
    [Parameter(Mandatory = $true)][string]$RuntimeSourceRoot,
    [string]$ConfiguredDataRoot = '',
    [string]$SlackChannelID = '',
    [string]$ProjectRepository = '',
    [string]$ProjectPath = '',
    [string]$ProjectRemoteURL = '',
    [string]$ProjectDisplayName = 'CWapi'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

& (Join-Path $PSScriptRoot 'prepare_v160_real_slack_validation.ps1') `
    -ExpectedCommit $ExpectedCommit `
    -RuntimeSourceRoot $RuntimeSourceRoot `
    -ConfiguredDataRoot $ConfiguredDataRoot `
    -SlackChannelID $SlackChannelID `
    -ProjectRepository $ProjectRepository `
    -ProjectPath $ProjectPath `
    -ProjectRemoteURL $ProjectRemoteURL `
    -ProjectDisplayName $ProjectDisplayName
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
