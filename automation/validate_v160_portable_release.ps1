param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,
    [string]$ZipPath = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (-not $ZipPath) { $ZipPath = Join-Path $RepoRoot 'build\stage\CWapi-v1.6.0.zip' }
$ZipPath = (Resolve-Path -LiteralPath $ZipPath).Path

function Wait-File {
    param([Parameter(Mandatory = $true)][string]$Path, [int]$TimeoutSeconds = 45)
    $Deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    while ([DateTime]::UtcNow -lt $Deadline) {
        if (Test-Path -LiteralPath $Path -PathType Leaf) { return }
        Start-Sleep -Milliseconds 200
    }
    throw "CWAPI_PORTABLE_RELOCATION_EVIDENCE_TIMEOUT path=$Path"
}

function Stop-OwnedProcess {
    param([System.Diagnostics.Process]$Process)
    if ($null -eq $Process) { return }
    try { $Process.Refresh() } catch { return }
    if ($Process.HasExited) { return }
    try { Stop-Process -Id $Process.Id -Force -ErrorAction Stop } catch {}
    $Deadline = [DateTime]::UtcNow.AddSeconds(15)
    while ([DateTime]::UtcNow -lt $Deadline) {
        try {
            $Process.Refresh()
            if ($Process.HasExited) { return }
        } catch { return }
        Start-Sleep -Milliseconds 200
    }
    throw "CWAPI_PORTABLE_RELOCATION_PROCESS_STOP_TIMEOUT pid=$($Process.Id)"
}

function Remove-TreeWithRetry {
    param([Parameter(Mandatory = $true)][string]$Path, [Parameter(Mandatory = $true)][string]$AllowedRoot)
    if (-not (Test-Path -LiteralPath $Path)) { return }
    $ResolvedRoot = [System.IO.Path]::GetFullPath($AllowedRoot).TrimEnd('\')
    $ResolvedPath = [System.IO.Path]::GetFullPath($Path).TrimEnd('\')
    if ($ResolvedPath.Equals($ResolvedRoot, [System.StringComparison]::OrdinalIgnoreCase) -or
        -not $ResolvedPath.StartsWith($ResolvedRoot + '\', [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "CWAPI_PORTABLE_RELOCATION_CLEANUP_PATH_INVALID path=$ResolvedPath root=$ResolvedRoot"
    }
    $LastError = $null
    for ($Attempt = 1; $Attempt -le 20; $Attempt++) {
        try {
            Remove-Item -LiteralPath $ResolvedPath -Recurse -Force -ErrorAction Stop
            return
        } catch {
            $LastError = $_
            Start-Sleep -Milliseconds 250
        }
    }
    throw "CWAPI_PORTABLE_RELOCATION_CLEANUP_FAILED path=$ResolvedPath error=$($LastError.Exception.Message)"
}

function Assert-BinaryOmitsBuildIdentity {
    param([Parameter(Mandatory = $true)][string]$Path)
    $Bytes = [System.IO.File]::ReadAllBytes($Path)
    $Text = [System.Text.Encoding]::Latin1.GetString($Bytes)
    $Candidates = @($RepoRoot, $RepoRoot.Replace('\', '/'))
    if ($env:USERPROFILE) {
        $Candidates += $env:USERPROFILE
        $Candidates += $env:USERPROFILE.Replace('\', '/')
    }
    if ($env:USERNAME -and $env:USERNAME.Length -ge 4) { $Candidates += $env:USERNAME }
    foreach ($Candidate in @($Candidates | Where-Object { $_ } | Sort-Object -Unique)) {
        if ($Text.IndexOf($Candidate, [System.StringComparison]::OrdinalIgnoreCase) -ge 0) {
            throw 'CWAPI_PORTABLE_BUILD_IDENTITY_PRESENT'
        }
    }
}

$Entries = @(tar.exe -tf $ZipPath)
if ($LASTEXITCODE -ne 0 -or $Entries.Count -eq 0) { throw 'CWAPI_PORTABLE_ARCHIVE_LIST_FAILED' }
$ForbiddenEntries = @($Entries | Where-Object {
    $_ -match '(?i)(^|/)CWapi-data(/|$)' -or
    $_ -match '(?i)(^|/)(\.env(?:\.[^/]*)?|credentials\.json|token\.json|Cookies|Login Data|Web Data|Local State|Preferences|History)$' -or
    $_ -match '(?i)\.(log|tmp|dmp|trace|db|sqlite|sqlite3|pfx|p12|kdbx)$' -or
    $_ -match '(?i)/(User Data|Default|Profile [0-9]+)(/|$)'
})
if ($ForbiddenEntries.Count -ne 0) {
    $ForbiddenEntries | ForEach-Object { Write-Host $_ }
    throw "CWAPI_PORTABLE_ARCHIVE_PRIVATE_ARTIFACT_PRESENT count=$($ForbiddenEntries.Count)"
}

$ExistingInstances = @(Get-Process -Name 'CWapi' -ErrorAction SilentlyContinue)
if ($ExistingInstances.Count -ne 0) {
    throw "CWAPI_PORTABLE_EXISTING_INSTANCE count=$($ExistingInstances.Count)"
}

$ProbeBase = Join-Path ([System.IO.Path]::GetTempPath()) ('cwapi-public-release-' + [Guid]::NewGuid().ToString('N'))
$InstallRoot = Join-Path $ProbeBase '任意路径 with spaces - CWapi-v1.6.0'
$WorkingRoot = Join-Path $ProbeBase 'unrelated working directory'
New-Item -ItemType Directory -Force -Path $InstallRoot, $WorkingRoot | Out-Null

$Process = $null
$HadProbeConfig = Test-Path Env:CWAPI_GUI_PROBE_CONFIG
$PreviousProbeConfig = if ($HadProbeConfig) { $env:CWAPI_GUI_PROBE_CONFIG } else { $null }
$Result = $null
try {
    Expand-Archive -LiteralPath $ZipPath -DestinationPath $InstallRoot -Force
    $Executable = Join-Path $InstallRoot 'CWapi.exe'
    $ManifestPath = Join-Path $InstallRoot 'portable-manifest.json'
    if (-not (Test-Path -LiteralPath $Executable -PathType Leaf)) { throw 'CWAPI_PORTABLE_RELOCATION_EXE_MISSING' }
    if (-not (Test-Path -LiteralPath $ManifestPath -PathType Leaf)) { throw 'CWAPI_PORTABLE_RELOCATION_MANIFEST_MISSING' }

    $Manifest = Get-Content -Raw -LiteralPath $ManifestPath | ConvertFrom-Json
    if ($Manifest.version -ne '1.6.0' -or $Manifest.source_commit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
        throw 'CWAPI_PORTABLE_RELOCATION_MANIFEST_VERSION_INVALID'
    }
    if (-not [bool]$Manifest.relocatable -or $Manifest.install_root -ne 'executable_directory' -or $Manifest.data_root -ne 'CWapi-data' -or [bool]$Manifest.user_data_included) {
        throw 'CWAPI_PORTABLE_RELOCATION_MANIFEST_POLICY_INVALID'
    }

    $GoPath = (Get-Command go.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $BuildMetadata = @(& $GoPath version -m $Executable)
    if ($LASTEXITCODE -ne 0 -or -not ($BuildMetadata -match '(?m)^\s*build\s+-trimpath=true\s*$')) {
        throw 'CWAPI_PORTABLE_RELOCATION_TRIMPATH_INVALID'
    }
    $global:LASTEXITCODE = 0
    Assert-BinaryOmitsBuildIdentity -Path $Executable

    $GitPath = Join-Path $InstallRoot 'runtime\git\cmd\git.exe'
    $NodePath = Join-Path $InstallRoot 'runtime\node\node.exe'
    $CodexPath = Join-Path $InstallRoot 'runtime\codex\current\bin\codex.exe'
    $GitVersion = (& $GitPath --version).Trim()
    if ($LASTEXITCODE -ne 0 -or $GitVersion -notmatch [regex]::Escape([string]$Manifest.git_version)) { throw 'CWAPI_PORTABLE_RELOCATION_GIT_FAILED' }
    $NodeVersion = (& $NodePath --version).Trim()
    if ($LASTEXITCODE -ne 0 -or $NodeVersion -ne ('v' + [string]$Manifest.node_version)) { throw 'CWAPI_PORTABLE_RELOCATION_NODE_FAILED' }
    $CodexVersion = (& $CodexPath --version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $CodexVersion -ne [string]$Manifest.codex_version_output) { throw 'CWAPI_PORTABLE_RELOCATION_CODEX_FAILED' }
    $global:LASTEXITCODE = 0

    $DataRoot = Join-Path $InstallRoot 'CWapi-data'
    $UnexpectedWorkingData = Join-Path $WorkingRoot 'CWapi-data'
    $FrontendEvidence = Join-Path $DataRoot 'state\frontend-ready.json'
    $GUIEvidence = Join-Path $DataRoot 'state\gui-probe.json'
    $env:CWAPI_GUI_PROBE_CONFIG = (@{ mode = 'first-run'; source_commit = $ExpectedCommit } | ConvertTo-Json -Compress)
    $Process = Start-Process -FilePath $Executable -WorkingDirectory $WorkingRoot -WindowStyle Hidden -PassThru
    Start-Sleep -Milliseconds 750
    $Process.Refresh()
    if ($Process.HasExited) { throw "CWAPI_PORTABLE_RELOCATION_EXITED_EARLY exit=$($Process.ExitCode)" }

    Wait-File -Path $FrontendEvidence -TimeoutSeconds 30
    Wait-File -Path $GUIEvidence -TimeoutSeconds 45
    $Frontend = Get-Content -Raw -LiteralPath $FrontendEvidence | ConvertFrom-Json
    $Probe = Get-Content -Raw -LiteralPath $GUIEvidence | ConvertFrom-Json
    if ($Frontend.source_commit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant() -or
        $Probe.source_commit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant() -or
        -not [bool]$Probe.result.success) {
        throw 'CWAPI_PORTABLE_RELOCATION_GUI_EVIDENCE_INVALID'
    }
    if (Test-Path -LiteralPath $UnexpectedWorkingData) { throw 'CWAPI_PORTABLE_RELOCATION_CWD_DATA_LEAK' }
    if (-not (Test-Path -LiteralPath (Join-Path $DataRoot 'state\cwapi.db') -PathType Leaf)) {
        throw 'CWAPI_PORTABLE_RELOCATION_DATA_ROOT_INVALID'
    }

    $Result = [ordered]@{
        schema = 'cwapi.portable-release-validation.v1'
        version = [string]$Manifest.version
        source_commit = $ExpectedCommit.ToLowerInvariant()
        zip_sha256 = (Get-FileHash -LiteralPath $ZipPath -Algorithm SHA256).Hash.ToLowerInvariant()
        archive_entries = $Entries.Count
        build_trimpath = $true
        build_identity_clean = $true
        user_data_included = $false
        relocated_to_different_drive = -not ([System.IO.Path]::GetPathRoot($RepoRoot).Equals([System.IO.Path]::GetPathRoot($InstallRoot), [System.StringComparison]::OrdinalIgnoreCase))
        install_root_policy = 'executable_directory'
        data_root_policy = 'executable_directory/CWapi-data'
        launch_from_unrelated_cwd = $true
        git_version = $GitVersion
        node_version = $NodeVersion
        codex_version = $CodexVersion
    }
} finally {
    if ($HadProbeConfig) { $env:CWAPI_GUI_PROBE_CONFIG = $PreviousProbeConfig } else { Remove-Item Env:CWAPI_GUI_PROBE_CONFIG -ErrorAction SilentlyContinue }
    Stop-OwnedProcess -Process $Process
    Remove-TreeWithRetry -Path $InstallRoot -AllowedRoot $ProbeBase
    Remove-TreeWithRetry -Path $WorkingRoot -AllowedRoot $ProbeBase
    if (Test-Path -LiteralPath $ProbeBase) {
        $TemporaryRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd('\')
        $ResolvedProbeBase = [System.IO.Path]::GetFullPath($ProbeBase).TrimEnd('\')
        if (-not $ResolvedProbeBase.StartsWith($TemporaryRoot + '\', [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "CWAPI_PORTABLE_RELOCATION_BASE_CLEANUP_PATH_INVALID path=$ResolvedProbeBase root=$TemporaryRoot"
        }
        [System.IO.Directory]::Delete($ResolvedProbeBase, $false)
    }
}

$Result | ConvertTo-Json -Compress | Write-Host
Write-Host "CWAPI_PORTABLE_RELEASE_VALIDATION_PASS commit=$($ExpectedCommit.ToLowerInvariant())"
