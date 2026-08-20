param(
    [string]$CacheRoot = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Version = 'v2.13.0'
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (-not $CacheRoot) { $CacheRoot = Join-Path $RepoRoot "cache\wails-$Version" }
$CacheRoot = [System.IO.Path]::GetFullPath($CacheRoot)
$WailsPath = Join-Path $CacheRoot 'wails.exe'

if (Test-Path -LiteralPath $WailsPath -PathType Leaf) {
    Write-Output $WailsPath
    exit 0
}

New-Item -ItemType Directory -Force -Path $CacheRoot | Out-Null
$PreviousGoBin = $env:GOBIN
try {
    $env:GOBIN = $CacheRoot
    & go install "github.com/wailsapp/wails/v2/cmd/wails@$Version"
    if ($LASTEXITCODE -ne 0) {
        throw "CWAPI_VALIDATION_WAILS_INSTALL_FAILED exit=$LASTEXITCODE"
    }
} finally {
    $env:GOBIN = $PreviousGoBin
}

if (-not (Test-Path -LiteralPath $WailsPath -PathType Leaf)) {
    throw 'CWAPI_VALIDATION_WAILS_MISSING_AFTER_INSTALL'
}

Write-Output $WailsPath
