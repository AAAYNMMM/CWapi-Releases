param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,
    [Parameter(Mandatory = $true)]
    [string]$PortableRoot,
    [string]$ZipPath = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$PortableRoot = (Resolve-Path -LiteralPath $PortableRoot).Path
$ExpectedCommit = $ExpectedCommit.ToLowerInvariant()

function Convert-ToSafeRelativePath {
    param([Parameter(Mandatory = $true)][string]$Path)
    $Normalized = $Path.Replace('\', '/')
    while ($Normalized.StartsWith('./', [System.StringComparison]::Ordinal)) {
        $Normalized = $Normalized.Substring(2)
    }
    $Normalized = $Normalized.TrimEnd('/')
    if (-not $Normalized) { return '' }
    if ($Normalized.StartsWith('/') -or $Normalized -match '^[A-Za-z]:' -or $Normalized -match '(^|/)\.\.($|/)') {
        throw "CWAPI_PORTABLE_PRIVACY_UNSAFE_PATH path=$Normalized"
    }
    return $Normalized
}

function Assert-SafePackagePath {
    param([Parameter(Mandatory = $true)][string]$RelativePath)
    $RelativePath = Convert-ToSafeRelativePath -Path $RelativePath
    if (-not $RelativePath) { return }
    if ($RelativePath -match '(?i)(^|/)CWapi-data($|/)' -or
        $RelativePath -match '(?i)(^|/)runtime/mcp/cwapi($|/)' -or
        $RelativePath -match '(?i)(^|/)\.git($|/)' -or
        $RelativePath -match '(?i)(^|/)(user data|browser-profile)($|/)') {
        throw "CWAPI_PORTABLE_PRIVACY_FORBIDDEN_PATH path=$RelativePath"
    }
    $Leaf = [System.IO.Path]::GetFileName($RelativePath)
    if ($Leaf -match '(?i)^(token|credentials)\.json$' -or
        $Leaf -match '(?i)^\.env($|\.)' -or
        $Leaf -match '(?i)\.(db|db-shm|db-wal|sqlite|sqlite3|log)$' -or
        $Leaf -match '(?i)^(cookies|history|login data|web data|local state)$' -or
        $Leaf -match '(?i)^(id_rsa|id_ed25519|known_hosts|authorized_keys|\.npmrc|\.gitconfig)$' -or
        $Leaf -ieq 'gh.exe') {
        throw "CWAPI_PORTABLE_PRIVACY_FORBIDDEN_FILE path=$RelativePath"
    }
}

function Assert-NoContentMatch {
    param(
        [Parameter(Mandatory = $true)][string]$Label,
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [Parameter(Mandatory = $true)][string]$RgPath
    )
    $Matches = @(& $RgPath @Arguments 2>$null)
    $ExitCode = $LASTEXITCODE
    if ($ExitCode -eq 0) {
        throw "CWAPI_PORTABLE_PRIVACY_CONTENT_MATCH label=$Label files=$($Matches.Count)"
    }
    if ($ExitCode -ne 1) {
        throw "CWAPI_PORTABLE_PRIVACY_SCAN_FAILED label=$Label exit=$ExitCode"
    }
    $global:LASTEXITCODE = 0
}

$TopLevel = @(Get-ChildItem -LiteralPath $PortableRoot -Force)
$TopLevelNames = @($TopLevel.Name | Sort-Object)
$ExpectedTopLevel = @('CWapi.exe', 'portable-manifest.json', 'runtime') | Sort-Object
if (($TopLevelNames -join ',') -ne ($ExpectedTopLevel -join ',')) {
    throw "CWAPI_PORTABLE_PRIVACY_TOP_LEVEL_INVALID actual=$($TopLevelNames -join ',')"
}

$AllItems = @(Get-ChildItem -LiteralPath $PortableRoot -Recurse -Force)
foreach ($Item in $AllItems) {
    $Relative = [System.IO.Path]::GetRelativePath($PortableRoot, $Item.FullName)
    Assert-SafePackagePath -RelativePath $Relative
    if (($Item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "CWAPI_PORTABLE_PRIVACY_REPARSE_POINT path=$Relative"
    }
}

$RequiredFiles = @(
    'CWapi.exe',
    'portable-manifest.json',
    'runtime\codex\current\bin\codex.exe',
    'runtime\git\cmd\git.exe',
    'runtime\node\node.exe'
)
foreach ($Relative in $RequiredFiles) {
    if (-not (Test-Path -LiteralPath (Join-Path $PortableRoot $Relative) -PathType Leaf)) {
        throw "CWAPI_PORTABLE_PRIVACY_REQUIRED_FILE_MISSING path=$Relative"
    }
}

$ManifestPath = Join-Path $PortableRoot 'portable-manifest.json'
$Manifest = Get-Content -Raw -LiteralPath $ManifestPath | ConvertFrom-Json
$AllowedManifestFields = @(
    'browser_revision', 'browser_version', 'codex_commit', 'codex_sha256',
    'codex_version', 'git_executable', 'git_version', 'git_version_output',
    'node_sha256', 'node_version', 'playwright_mcp_version', 'schema',
    'source_commit', 'staged_at', 'user_data_included', 'version'
) | Sort-Object
$ActualManifestFields = @($Manifest.PSObject.Properties.Name | Sort-Object)
if (@(Compare-Object $AllowedManifestFields $ActualManifestFields).Count -ne 0) {
    throw 'CWAPI_PORTABLE_PRIVACY_MANIFEST_FIELDS_INVALID'
}
if ($Manifest.schema -ne 'cwapi.portable-manifest.v1' -or
    $Manifest.version -ne '1.6.1' -or
    ([string]$Manifest.source_commit).ToLowerInvariant() -ne $ExpectedCommit -or
    [bool]$Manifest.user_data_included) {
    throw 'CWAPI_PORTABLE_PRIVACY_MANIFEST_INVALID'
}

$Rg = Get-Command rg.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $Rg) { $Rg = Get-Command rg -CommandType Application -ErrorAction Stop | Select-Object -First 1 }
$RgPath = $Rg.Source

$SecretPattern = '(?i)(?:xox[baprs]-[A-Za-z0-9-]{30,}|xapp-[A-Za-z0-9-]{30,}|github_pat_[A-Za-z0-9_]{50,}|gh[pousr]_[A-Za-z0-9]{30,}|sk-proj-[A-Za-z0-9_-]{40,}|sk-[A-Za-z0-9]{40,})'
Assert-NoContentMatch -Label 'credential-shape' -RgPath $RgPath -Arguments @(
    '-a', '-l', '--hidden', '--no-messages', '--pcre2', '-e', $SecretPattern, '--', $PortableRoot
)
$PrivateKeyPattern = '(?is)-----BEGIN (?:RSA |OPENSSH |EC )?PRIVATE KEY-----\s+[A-Za-z0-9+/=\r\n]{64,}\s+-----END (?:RSA |OPENSSH |EC )?PRIVATE KEY-----'
Assert-NoContentMatch -Label 'private-key' -RgPath $RgPath -Arguments @(
    '-a', '-U', '-l', '--hidden', '--no-messages', '--pcre2', '-e', $PrivateKeyPattern, '--', $PortableRoot
)

$IdentityNeedles = @()
$UserProfile = [Environment]::GetFolderPath('UserProfile')
foreach ($Value in @($RepoRoot, $UserProfile)) {
    if ($Value) {
        $IdentityNeedles += $Value
        $IdentityNeedles += $Value.Replace('\', '/')
        $IdentityNeedles += $Value.Replace('\', '\\')
    }
}
$MachineName = [Environment]::MachineName
if ($MachineName -and $MachineName.Length -ge 6) { $IdentityNeedles += $MachineName }
$IdentityNeedles = @($IdentityNeedles | Where-Object { $_ -and $_.Length -ge 6 } | Sort-Object -Unique)
if ($IdentityNeedles.Count -gt 0) {
    $Arguments = @('-a', '-l', '--hidden', '--no-messages', '-F', '-i')
    foreach ($Needle in $IdentityNeedles) { $Arguments += @('-e', $Needle) }
    $Arguments += @('--', $PortableRoot)
    Assert-NoContentMatch -Label 'build-identity' -RgPath $RgPath -Arguments $Arguments
}

$SecretValues = @(Get-ChildItem Env: | Where-Object {
    $_.Name -match '(?i)(TOKEN|SECRET|PASSWORD|PASSWD|API[_-]?KEY|CREDENTIAL)'
} | ForEach-Object { [string]$_.Value } | Where-Object { $_ -and $_.Length -ge 12 } | Sort-Object -Unique)
if ($SecretValues.Count -gt 0) {
    $Arguments = @('-a', '-l', '--hidden', '--no-messages', '-F')
    foreach ($SecretValue in $SecretValues) { $Arguments += @('-e', $SecretValue) }
    $Arguments += @('--', $PortableRoot)
    Assert-NoContentMatch -Label 'active-secret-value' -RgPath $RgPath -Arguments $Arguments
}

$ZipHash = 'not-checked'
if ($ZipPath) {
    $ZipPath = (Resolve-Path -LiteralPath $ZipPath).Path
    $Tar = Get-Command tar.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1
    $ArchiveEntries = @(& $Tar.Source -tf $ZipPath)
    if ($LASTEXITCODE -ne 0) { throw "CWAPI_PORTABLE_PRIVACY_ZIP_LIST_FAILED exit=$LASTEXITCODE" }
    $ArchiveTopLevel = @()
    foreach ($Entry in $ArchiveEntries) {
        $Relative = Convert-ToSafeRelativePath -Path ([string]$Entry)
        if (-not $Relative) { continue }
        Assert-SafePackagePath -RelativePath $Relative
        $ArchiveTopLevel += $Relative.Split('/')[0]
    }
    $ArchiveTopLevel = @($ArchiveTopLevel | Sort-Object -Unique)
    if (($ArchiveTopLevel -join ',') -ne ($ExpectedTopLevel -join ',')) {
        throw "CWAPI_PORTABLE_PRIVACY_ZIP_TOP_LEVEL_INVALID actual=$($ArchiveTopLevel -join ',')"
    }
    $ZipHash = (Get-FileHash -LiteralPath $ZipPath -Algorithm SHA256).Hash.ToLowerInvariant()
}

$Files = @(Get-ChildItem -LiteralPath $PortableRoot -Recurse -File -Force)
Write-Host "CWAPI_PORTABLE_PRIVACY_PASS files=$($Files.Count) zip_sha256=$ZipHash"
