param(
    [Parameter(Mandatory = $true)]
    [string]$ArchivePath,

    [Parameter(Mandatory = $true)]
    [string]$Version,

    [string]$ExpectedSha256 = "",

    [string]$ProjectRoot = (Split-Path -Parent $PSScriptRoot)
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Remove-CWapiPathSafely {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [switch]$BestEffort,

        [ValidateRange(1, 30)]
        [int]$MaxAttempts = 8
    )

    if (-not (Test-Path -LiteralPath $Path)) {
        return $true
    }

    $lastError = $null
    for ($attempt = 1; $attempt -le $MaxAttempts; $attempt++) {
        try {
            $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
            $isReparsePoint = ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
            if ($isReparsePoint) {
                Remove-Item -LiteralPath $Path -Force -ErrorAction Stop
            }
            else {
                Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction Stop
            }
            return $true
        }
        catch {
            $lastError = $_
            if ($attempt -lt $MaxAttempts) {
                [GC]::Collect()
                [GC]::WaitForPendingFinalizers()
                Start-Sleep -Milliseconds ([Math]::Min(2000, 250 * $attempt))
            }
        }
    }

    if ($BestEffort) {
        $parent = Split-Path -Parent $Path
        $leaf = Split-Path -Leaf $Path
        $pendingName = ".cleanup-pending-$leaf-$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())-$([Guid]::NewGuid().ToString('N'))"
        $pendingPath = Join-Path $parent $pendingName
        try {
            Move-Item -LiteralPath $Path -Destination $pendingPath -Force -ErrorAction Stop
            Write-Warning "Runtime installation succeeded, but a temporary directory was still locked. It was moved to: $pendingPath"
        }
        catch {
            Write-Warning "Runtime installation succeeded, but a temporary directory could not be removed yet: $Path. It can be deleted later after the scanner releases the file. Last error: $($lastError.Exception.Message)"
        }
        return $false
    }

    throw $lastError
}

function Remove-StaleInstallDirectories {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RuntimeRoot
    )

    if (-not (Test-Path -LiteralPath $RuntimeRoot -PathType Container)) {
        return
    }

    foreach ($pattern in @(".staging-*", ".current-*", ".cleanup-pending-*")) {
        Get-ChildItem -LiteralPath $RuntimeRoot -Directory -Force -Filter $pattern -ErrorAction SilentlyContinue |
            ForEach-Object {
                [void](Remove-CWapiPathSafely -Path $_.FullName -BestEffort)
            }
    }
}

$archive = (Resolve-Path -LiteralPath $ArchivePath).Path
$project = (Resolve-Path -LiteralPath $ProjectRoot).Path
$runtimeRoot = Join-Path $project "runtime\codex"
$releasesRoot = Join-Path $runtimeRoot "releases"
$releaseRoot = Join-Path $releasesRoot $Version
$currentRoot = Join-Path $runtimeRoot "current"
$stagingRoot = Join-Path $runtimeRoot (".staging-" + [Guid]::NewGuid().ToString("N"))
$currentStaging = Join-Path $runtimeRoot (".current-" + [Guid]::NewGuid().ToString("N"))

$isZip = $archive.EndsWith(".zip", [StringComparison]::OrdinalIgnoreCase)
$isTarGz = $archive.EndsWith(".tar.gz", [StringComparison]::OrdinalIgnoreCase) -or $archive.EndsWith(".tgz", [StringComparison]::OrdinalIgnoreCase)
if (-not $isZip -and -not $isTarGz) {
    throw "Codex runtime archive must be ZIP, TAR.GZ, or TGZ: $archive"
}

if ($ExpectedSha256) {
    $actualHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    $expectedHash = $ExpectedSha256.Trim().ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        throw "Codex archive SHA-256 mismatch. Expected $expectedHash, actual $actualHash"
    }
}

New-Item -ItemType Directory -Force -Path $releasesRoot | Out-Null
Remove-StaleInstallDirectories -RuntimeRoot $runtimeRoot
New-Item -ItemType Directory -Force -Path $stagingRoot | Out-Null

try {
    if ($isZip) {
        Expand-Archive -LiteralPath $archive -DestinationPath $stagingRoot -Force
    }
    else {
        $tar = Get-Command tar -ErrorAction Stop
        & $tar.Source -xzf $archive -C $stagingRoot
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to extract Codex TAR.GZ archive."
        }
    }

    $candidates = Get-ChildItem -LiteralPath $stagingRoot -Filter "codex.exe" -File -Recurse
    $codex = $candidates |
        Sort-Object @{ Expression = { if ($_.Directory.Name -eq "bin") { 0 } else { 1 } } }, FullName |
        Select-Object -First 1
    if (-not $codex) {
        throw "The archive does not contain codex.exe"
    }
    if ($codex.Directory.Name -ne "bin") {
        throw "The Codex package must contain bin\codex.exe and its companion directories"
    }

    $bundleRoot = $codex.Directory.Parent.FullName
    foreach ($required in @("bin\codex.exe", "codex-resources", "codex-path")) {
        if (-not (Test-Path -LiteralPath (Join-Path $bundleRoot $required))) {
            throw "Codex package is incomplete. Missing: $required"
        }
    }

    [void](Remove-CWapiPathSafely -Path $releaseRoot)
    New-Item -ItemType Directory -Force -Path $releaseRoot | Out-Null
    Copy-Item -Path (Join-Path $bundleRoot "*") -Destination $releaseRoot -Recurse -Force

    New-Item -ItemType Directory -Force -Path $currentStaging | Out-Null
    Copy-Item -Path (Join-Path $releaseRoot "*") -Destination $currentStaging -Recurse -Force
    [void](Remove-CWapiPathSafely -Path $currentRoot)
    Move-Item -LiteralPath $currentStaging -Destination $currentRoot

    $manifest = [ordered]@{
        schema = "cwapi.codex-runtime.v1"
        version = $Version
        archive_path = $archive
        archive_sha256 = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
        installed_at = [DateTimeOffset]::UtcNow.ToString("o")
        executable_path = (Join-Path $currentRoot "bin\codex.exe")
        source = "pinned stock Codex package"
    }
    $manifest |
        ConvertTo-Json -Depth 4 |
        Set-Content -LiteralPath (Join-Path $runtimeRoot "runtime.json") -Encoding UTF8

    Write-Host "Installed CWapi stock Codex runtime: $Version"
    Write-Host "Executable: $(Join-Path $currentRoot 'bin\codex.exe')"
    Write-Host "No Codex process was started."
}
finally {
    [void](Remove-CWapiPathSafely -Path $stagingRoot -BestEffort)
    [void](Remove-CWapiPathSafely -Path $currentStaging -BestEffort)
}
