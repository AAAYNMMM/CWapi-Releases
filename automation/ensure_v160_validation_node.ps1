param(
    [string]$CacheRoot = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$LockPath = Join-Path $RepoRoot 'config\portable-runtime.lock.json'
if (-not (Test-Path -LiteralPath $LockPath -PathType Leaf)) { throw 'CWAPI_NODE_PORTABLE_LOCK_MISSING' }
$Lock = Get-Content -Raw -LiteralPath $LockPath | ConvertFrom-Json
if ($Lock.schema -ne 'cwapi.portable-runtime-lock.v2') { throw "CWAPI_NODE_PORTABLE_LOCK_INVALID schema=$($Lock.schema)" }

$Version = ([string]$Lock.components.node.version).Trim()
$ArchiveName = ([string]$Lock.components.node.archive_name).Trim()
$ArchiveSha256 = ([string]$Lock.components.node.sha256).Trim().ToLowerInvariant()
$NodeHashExpected = ([string]$Lock.components.node.executable_sha256).Trim().ToLowerInvariant()
$DownloadUrl = ([string]$Lock.components.node.url).Trim()
if (-not $Version -or -not $ArchiveName -or $ArchiveSha256 -notmatch '^[0-9a-f]{64}$' -or $NodeHashExpected -notmatch '^[0-9a-f]{64}$' -or -not $DownloadUrl) {
    throw 'CWAPI_NODE_PORTABLE_LOCK_FIELDS_INVALID'
}
if (-not $CacheRoot) { $CacheRoot = Join-Path $RepoRoot "cache\portable-node-v$Version" }
$CacheRoot = [System.IO.Path]::GetFullPath($CacheRoot)
$RuntimeRoot = Join-Path $CacheRoot "node-v$Version-win-x64"
$NodePath = Join-Path $RuntimeRoot 'node.exe'
$NpmPath = Join-Path $RuntimeRoot 'npm.cmd'

if ((Test-Path -LiteralPath $NodePath -PathType Leaf) -and (Test-Path -LiteralPath $NpmPath -PathType Leaf)) {
    $CachedNodeHash = (Get-FileHash -LiteralPath $NodePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($CachedNodeHash -eq $NodeHashExpected) {
        Write-Output $NpmPath
        exit 0
    }
    Remove-Item -LiteralPath $RuntimeRoot -Recurse -Force -ErrorAction SilentlyContinue
}

New-Item -ItemType Directory -Force -Path $CacheRoot | Out-Null
$ArchivePath = Join-Path $CacheRoot $ArchiveName
$TemporaryArchive = $ArchivePath + '.download'

if (-not (Test-Path -LiteralPath $ArchivePath -PathType Leaf)) {
    Remove-Item -LiteralPath $TemporaryArchive -Force -ErrorAction SilentlyContinue
    Invoke-WebRequest -UseBasicParsing -Uri $DownloadUrl -OutFile $TemporaryArchive
    $DownloadedHash = (Get-FileHash -LiteralPath $TemporaryArchive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($DownloadedHash -ne $ArchiveSha256) {
        Remove-Item -LiteralPath $TemporaryArchive -Force -ErrorAction SilentlyContinue
        throw "CWAPI_NODE_ARCHIVE_HASH_MISMATCH expected=$ArchiveSha256 actual=$DownloadedHash"
    }
    Move-Item -LiteralPath $TemporaryArchive -Destination $ArchivePath -Force
} else {
    $CachedHash = (Get-FileHash -LiteralPath $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($CachedHash -ne $ArchiveSha256) {
        Remove-Item -LiteralPath $ArchivePath -Force
        throw "CWAPI_NODE_CACHE_HASH_MISMATCH expected=$ArchiveSha256 actual=$CachedHash"
    }
}

$ExtractRoot = Join-Path $CacheRoot 'extracting'
Remove-Item -LiteralPath $ExtractRoot -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $ExtractRoot | Out-Null
try {
    Expand-Archive -LiteralPath $ArchivePath -DestinationPath $ExtractRoot -Force
    $ExtractedRuntime = Join-Path $ExtractRoot "node-v$Version-win-x64"
    if (-not (Test-Path -LiteralPath (Join-Path $ExtractedRuntime 'node.exe') -PathType Leaf)) {
        throw 'CWAPI_NODE_ARCHIVE_LAYOUT_INVALID'
    }
    Remove-Item -LiteralPath $RuntimeRoot -Recurse -Force -ErrorAction SilentlyContinue
    Move-Item -LiteralPath $ExtractedRuntime -Destination $RuntimeRoot
} finally {
    Remove-Item -LiteralPath $ExtractRoot -Recurse -Force -ErrorAction SilentlyContinue
}

if (-not (Test-Path -LiteralPath $NodePath -PathType Leaf) -or -not (Test-Path -LiteralPath $NpmPath -PathType Leaf)) {
    throw 'CWAPI_NODE_RUNTIME_MISSING_AFTER_EXTRACT'
}
$NodeHashActual = (Get-FileHash -LiteralPath $NodePath -Algorithm SHA256).Hash.ToLowerInvariant()
if ($NodeHashActual -ne $NodeHashExpected) {
    throw "CWAPI_NODE_EXECUTABLE_HASH_MISMATCH expected=$NodeHashExpected actual=$NodeHashActual"
}
Write-Output $NpmPath
