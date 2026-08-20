param(
    [string]$LockPath = "",
    [string]$ProjectRoot = (Split-Path -Parent $PSScriptRoot),
    [ValidateRange(1, 16)]
    [int]$Connections = 8,
    [switch]$Clobber
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

function Get-RequiredString([object]$Value, [string]$Name) {
    $text = [string]$Value
    if ([string]::IsNullOrWhiteSpace($text)) {
        throw "$Name must be a non-empty string."
    }
    return $text.Trim()
}

function Get-ReleaseAsset([object]$Metadata, [string]$Name) {
    $asset = $Metadata.assets |
        Where-Object { [string]$_.name -eq $Name } |
        Select-Object -First 1
    if ($null -eq $asset) {
        throw "Release does not contain required asset: $Name"
    }
    return $asset
}

function Get-AssetDigest([object]$Asset) {
    $digest = [string]$Asset.digest
    if ($digest -match '^sha256:([0-9a-fA-F]{64})$') {
        return $matches[1].ToLowerInvariant()
    }
    throw "GitHub Release asset does not expose a SHA-256 digest: $([string]$Asset.name)"
}

function Get-GitHubReleaseMetadata(
    [string]$GhPath,
    [string]$Repository,
    [string]$Tag
) {
    $endpoint = "repos/$Repository/releases/tags/$Tag"
    $json = & $GhPath api $endpoint `
        --header "Accept: application/vnd.github+json" `
        --header "X-GitHub-Api-Version: 2022-11-28"
    if ($LASTEXITCODE -ne 0) {
        throw "Could not read authenticated GitHub Release metadata: $Repository@$Tag"
    }
    try {
        return ($json | ConvertFrom-Json)
    }
    catch {
        throw "GitHub CLI returned invalid Release metadata for $Repository@$Tag. $($_.Exception.Message)"
    }
}

function Test-FileSha256(
    [string]$Path,
    [string]$ExpectedSha256
) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return $false
    }
    $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    return $actual -eq $ExpectedSha256.ToLowerInvariant()
}

function Invoke-CurlDownload(
    [string]$CurlPath,
    [string]$Uri,
    [string]$PartialPath,
    [string]$Description
) {
    $resume = Test-Path -LiteralPath $PartialPath -PathType Leaf
    $arguments = @(
        "--fail",
        "--location",
        "--retry", "15",
        "--retry-all-errors",
        "--retry-delay", "2",
        "--connect-timeout", "30",
        "--speed-time", "60",
        "--speed-limit", "1024",
        "--output", $PartialPath
    )
    if ($resume) {
        $arguments += @("--continue-at", "-")
        Write-Host "Resuming exact Release asset with curl.exe: $Description"
    }
    else {
        Write-Host "Downloading exact Release asset with curl.exe: $Description"
    }
    $arguments += $Uri

    & $CurlPath @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "curl.exe failed to download $Description. Partial data remains at $PartialPath"
    }
}

function Invoke-Aria2Download(
    [string]$Aria2Path,
    [string]$Uri,
    [string]$PartialPath,
    [string]$Description,
    [int]$ConnectionCount
) {
    $directory = Split-Path -Parent $PartialPath
    $fileName = Split-Path -Leaf $PartialPath
    Write-Host "Downloading exact Release asset with aria2c ($ConnectionCount connections): $Description"

    & $Aria2Path `
        --continue=true `
        --allow-overwrite=true `
        --auto-file-renaming=false `
        --max-tries=0 `
        --retry-wait=2 `
        --connect-timeout=30 `
        --timeout=60 `
        --split=$ConnectionCount `
        --max-connection-per-server=$ConnectionCount `
        --min-split-size=1M `
        --file-allocation=none `
        --dir=$directory `
        --out=$fileName `
        $Uri

    if ($LASTEXITCODE -ne 0) {
        throw "aria2c failed to download $Description. Partial data remains at $PartialPath"
    }
}

function Invoke-OptimizedDownload(
    [string]$Uri,
    [string]$Destination,
    [string]$Description,
    [string]$ExpectedSha256,
    [int]$ConnectionCount
) {
    $destinationDirectory = Split-Path -Parent $Destination
    New-Item -ItemType Directory -Force -Path $destinationDirectory | Out-Null

    if (Test-FileSha256 -Path $Destination -ExpectedSha256 $ExpectedSha256) {
        Write-Host "Reusing verified cached asset: $Description"
        return
    }
    if (Test-Path -LiteralPath $Destination -PathType Leaf) {
        Remove-Item -LiteralPath $Destination -Force
    }

    $partial = "$Destination.part"
    $aria2 = Get-Command aria2c.exe -ErrorAction SilentlyContinue
    if ($null -eq $aria2) {
        $aria2 = Get-Command aria2c -ErrorAction SilentlyContinue
    }
    $curl = Get-Command curl.exe -ErrorAction SilentlyContinue

    if ($null -ne $aria2) {
        try {
            Invoke-Aria2Download `
                -Aria2Path $aria2.Source `
                -Uri $Uri `
                -PartialPath $partial `
                -Description $Description `
                -ConnectionCount $ConnectionCount
        }
        catch {
            if ($null -eq $curl) {
                throw
            }
            Write-Warning "aria2c failed; falling back to resumable curl.exe. $($_.Exception.Message)"
            Invoke-CurlDownload `
                -CurlPath $curl.Source `
                -Uri $Uri `
                -PartialPath $partial `
                -Description $Description
        }
    }
    elseif ($null -ne $curl) {
        Invoke-CurlDownload `
            -CurlPath $curl.Source `
            -Uri $Uri `
            -PartialPath $partial `
            -Description $Description
    }
    else {
        throw "Neither aria2c nor curl.exe is available. Install aria2 for multi-connection downloads or use a current Windows build with curl.exe."
    }

    if (-not (Test-Path -LiteralPath $partial -PathType Leaf)) {
        throw "Download completed without creating a file: $Description"
    }
    if ((Get-Item -LiteralPath $partial).Length -le 0) {
        throw "Downloaded file is empty: $Description"
    }

    $actualSha = (Get-FileHash -LiteralPath $partial -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualSha -ne $ExpectedSha256.ToLowerInvariant()) {
        throw "Downloaded $Description SHA-256 mismatch. Expected $ExpectedSha256, actual $actualSha. Partial data remains at $partial"
    }

    Move-Item -LiteralPath $partial -Destination $Destination -Force
    Remove-Item -LiteralPath "$partial.aria2" -Force -ErrorAction SilentlyContinue
}

$project = (Resolve-Path -LiteralPath $ProjectRoot).Path
if (-not $LockPath) {
    $LockPath = Join-Path $project "config\codex-runtime.lock.json"
}
$lockPathResolved = (Resolve-Path -LiteralPath $LockPath).Path
$lock = Get-Content -LiteralPath $lockPathResolved -Raw | ConvertFrom-Json
if ($lock.schema -ne "cwapi.codex-runtime-lock.v1" -or $lock.state -ne "source_ready") {
    throw "Codex runtime lock is not ready."
}

$repository = Get-RequiredString $lock.repository "lock.repository"
$sourceRef = Get-RequiredString $lock.source_ref "lock.source_ref"
$sourceCommit = (Get-RequiredString $lock.source_commit "lock.source_commit").ToLowerInvariant()
$target = Get-RequiredString $lock.target "lock.target"
$version = Get-RequiredString $lock.version "lock.version"
$releaseRepository = Get-RequiredString $lock.release_repository "lock.release_repository"
$releaseTag = Get-RequiredString $lock.release_tag "lock.release_tag"
$releaseAssetName = Get-RequiredString $lock.release_asset "lock.release_asset"
$releaseManifestName = Get-RequiredString $lock.release_manifest_asset "lock.release_manifest_asset"
$upstreamReleaseRepository = Get-RequiredString $lock.upstream_release_repository "lock.upstream_release_repository"

if ($repository -eq "openai/codex" -or $releaseRepository -eq "openai/codex") {
    throw "The target Release must belong to the user's Codex fork."
}
if ($sourceCommit -notmatch '^[0-9a-f]{40}$') {
    throw "Codex source lock contains an invalid commit."
}
if ($releaseAssetName -ne "codex-package-$target.tar.gz") {
    throw "CWapi publisher only permits the locked target package: codex-package-$target.tar.gz"
}

$gh = Get-Command gh -ErrorAction Stop
& $gh.Source auth status *> $null
if ($LASTEXITCODE -ne 0) {
    throw "GitHub CLI is not authenticated. Run gh auth login first."
}

$upstreamMetadata = Get-GitHubReleaseMetadata `
    -GhPath $gh.Source `
    -Repository $upstreamReleaseRepository `
    -Tag $releaseTag
$packageAsset = Get-ReleaseAsset $upstreamMetadata $releaseAssetName
$expectedArchiveSha = Get-AssetDigest $packageAsset
$assetSize = [int64]$packageAsset.size
$assetUrl = [string]$packageAsset.browser_download_url

$downloadRoot = Join-Path $project ("runtime\codex\downloads\upstream\" + $releaseTag)
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("cwapi-codex-publish-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $downloadRoot, $tempRoot | Out-Null

try {
    $archivePath = Join-Path $downloadRoot $releaseAssetName
    $manifestPath = Join-Path $tempRoot $releaseManifestName

    Write-Host "Selected one upstream asset only:"
    Write-Host "  Repository: $upstreamReleaseRepository"
    Write-Host "  Tag:        $releaseTag"
    Write-Host "  Asset:      $releaseAssetName"
    Write-Host "  Size:       $([Math]::Round($assetSize / 1MB, 2)) MiB"

    Invoke-OptimizedDownload `
        -Uri $assetUrl `
        -Destination $archivePath `
        -Description $releaseAssetName `
        -ExpectedSha256 $expectedArchiveSha `
        -ConnectionCount $Connections

    $releaseManifest = [ordered]@{
        schema = "cwapi.codex-prebuilt-runtime.v1"
        source_repository = $repository
        source_ref = $sourceRef
        source_commit = $sourceCommit
        target = $target
        version = $version
        archive_name = $releaseAssetName
        archive_size = (Get-Item -LiteralPath $archivePath).Length
        archive_sha256 = $expectedArchiveSha
        mirrored_from_repository = $upstreamReleaseRepository
        mirrored_from_tag = $releaseTag
        published_at = [DateTimeOffset]::UtcNow.ToString("o")
    }
    $releaseManifest |
        ConvertTo-Json -Depth 6 |
        Set-Content -LiteralPath $manifestPath -Encoding UTF8

    & $gh.Source release view $releaseTag --repo $releaseRepository *> $null
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Creating GitHub Release $releaseRepository@$releaseTag ..."
        & $gh.Source release create $releaseTag `
            --repo $releaseRepository `
            --verify-tag `
            --title "CWapi Codex $version" `
            --notes "Pinned Windows x64 runtime for CWapi. Source commit: $sourceCommit"
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to create the Codex fork Release."
        }
    }

    $uploadArgs = @(
        "release", "upload", $releaseTag,
        $archivePath,
        $manifestPath,
        "--repo", $releaseRepository
    )
    if ($Clobber) {
        $uploadArgs += "--clobber"
    }
    & $gh.Source @uploadArgs
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to upload the locked Windows package to the user's fork Release."
    }

    Write-Host "Published the single CWapi-required Codex asset."
    Write-Host "Release: $releaseRepository@$releaseTag"
    Write-Host "Asset: $releaseAssetName"
    Write-Host "SHA-256: $expectedArchiveSha"
    Write-Host "Download cache: $downloadRoot"
}
finally {
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}
