param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,
    [Parameter(Mandatory = $true)]
    [string]$Path
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot
$ActualCommit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $ActualCommit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
    throw 'CWAPI_REPOSITORY_HASH_COMMIT_MISMATCH'
}
$Target = (Resolve-Path -LiteralPath $Path).Path
$RootPrefix = $RepoRoot.TrimEnd('\') + '\'
if (-not $Target.StartsWith($RootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'CWAPI_REPOSITORY_HASH_PATH_OUTSIDE_REPO'
}
$Relative = $Target.Substring($RootPrefix.Length)
$Hash = (Get-FileHash -LiteralPath $Target -Algorithm SHA256).Hash.ToLowerInvariant()
Write-Host "CWAPI_REPOSITORY_FILE_HASH path=$Relative sha256=$Hash"
