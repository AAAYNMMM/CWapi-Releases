param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

function Invoke-Checked {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][scriptblock]$Command
    )
    Write-Host "CWAPI_PACKAGED_GUI_STEP_START $Name"
    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "CWAPI_PACKAGED_GUI_STEP_FAILED: $Name exit=$LASTEXITCODE"
    }
    Write-Host "CWAPI_PACKAGED_GUI_STEP_PASS $Name"
}

function Assert-UnderRoot {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Label
    )
    $ResolvedRoot = [System.IO.Path]::GetFullPath($Root).TrimEnd('\')
    $ResolvedPath = [System.IO.Path]::GetFullPath($Path).TrimEnd('\')
    $Comparison = [System.StringComparison]::OrdinalIgnoreCase
    if ($ResolvedPath.Equals($ResolvedRoot, $Comparison)) { return }
    if ($ResolvedPath.StartsWith($ResolvedRoot + '\', $Comparison)) { return }
    throw "CWAPI_PACKAGED_GUI_PATH_OUTSIDE_ROOT label=$Label path=$ResolvedPath root=$ResolvedRoot"
}

function Wait-File {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [ValidateRange(1, 120)][int]$TimeoutSeconds = 45
    )
    $Deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    while ([DateTime]::UtcNow -lt $Deadline) {
        if (Test-Path -LiteralPath $Path -PathType Leaf) { return }
        Start-Sleep -Milliseconds 200
    }
    throw "CWAPI_PACKAGED_GUI_EVIDENCE_TIMEOUT path=$Path"
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
    throw "CWAPI_PACKAGED_GUI_PROCESS_STOP_TIMEOUT pid=$($Process.Id)"
}

function Remove-TreeWithRetry {
    param([Parameter(Mandatory = $true)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return }
    $LastError = $null
    for ($Attempt = 1; $Attempt -le 20; $Attempt++) {
        try {
            Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction Stop
            return
        } catch {
            $LastError = $_
            Start-Sleep -Milliseconds 250
        }
    }
    throw "CWAPI_PACKAGED_GUI_CLEANUP_FAILED path=$Path error=$($LastError.Exception.Message)"
}

$ActualCommit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_PACKAGED_GUI_HEAD_FAILED' }
if ($ActualCommit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
    throw "CWAPI_PACKAGED_GUI_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}

$StatusBefore = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_PACKAGED_GUI_STATUS_FAILED' }
if ($StatusBefore.Count -ne 0) { throw 'CWAPI_PACKAGED_GUI_WORKTREE_DIRTY_BEFORE' }

$NpmOutput = @(& (Join-Path $PSScriptRoot 'ensure_v160_validation_node.ps1'))
if ($LASTEXITCODE -ne 0 -or $NpmOutput.Count -eq 0) { throw 'CWAPI_PACKAGED_GUI_PINNED_NODE_FAILED' }
$NpmPath = ([string]($NpmOutput | Select-Object -Last 1)).Trim()
$NodePath = Join-Path (Split-Path -Parent $NpmPath) 'node.exe'
if (-not (Test-Path -LiteralPath $NodePath -PathType Leaf)) { throw 'CWAPI_PACKAGED_GUI_PINNED_NODE_UNAVAILABLE' }
if (-not (Test-Path -LiteralPath $NpmPath -PathType Leaf)) { throw 'CWAPI_PACKAGED_GUI_PINNED_NPM_UNAVAILABLE' }

$WailsOutput = @(& (Join-Path $PSScriptRoot 'ensure_v160_validation_wails.ps1'))
if ($LASTEXITCODE -ne 0 -or $WailsOutput.Count -eq 0) { throw 'CWAPI_PACKAGED_GUI_PINNED_WAILS_FAILED' }
$WailsPath = ([string]($WailsOutput | Select-Object -Last 1)).Trim()
if (-not (Test-Path -LiteralPath $WailsPath -PathType Leaf)) { throw 'CWAPI_PACKAGED_GUI_PINNED_WAILS_UNAVAILABLE' }
$WailsPath = (Resolve-Path -LiteralPath $WailsPath).Path

$OriginalPath = $env:Path
$NodeDir = Split-Path -Parent $NodePath
$GoPath = (Get-Command go.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$GitPath = Join-Path $RepoRoot 'runtime\git\cmd\git.exe'
if (-not (Test-Path -LiteralPath $GitPath -PathType Leaf)) {
    $GitPath = (Get-Command git.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
}
$WindowsRoot = [Environment]::GetEnvironmentVariable('SystemRoot', 'Process')
if (-not $WindowsRoot) { throw 'CWAPI_PACKAGED_GUI_SYSTEM_ROOT_MISSING' }
$env:Path = (@(
    $NodeDir,
    (Split-Path -Parent $GoPath),
    (Split-Path -Parent $GitPath),
    (Join-Path $WindowsRoot 'System32'),
    $WindowsRoot,
    (Join-Path $WindowsRoot 'System32\Wbem')
) -join ';')
Write-Host "CWAPI_PACKAGED_GUI_NODE $NodePath"
Write-Host "CWAPI_PACKAGED_GUI_NPM $NpmPath"
Write-Host "CWAPI_PACKAGED_GUI_WAILS $WailsPath"

Push-Location 'frontend'
try {
    if (-not (Test-Path -LiteralPath 'node_modules\.bin\tsc.cmd' -PathType Leaf)) {
        Invoke-Checked 'frontend-npm-install' { & $NpmPath install --package-lock=false --prefer-offline --no-audit --no-fund }
    }
} finally {
    Pop-Location
}

$LdFlags = "-X github.com/AAAYNMMM/CWapi/internal/buildinfo.SourceCommit=$ExpectedCommit"
Invoke-Checked 'wails-windows-build' { & $WailsPath build -clean -trimpath -platform windows/amd64 -ldflags $LdFlags }

$ExePath = Join-Path $RepoRoot 'build\bin\CWapi.exe'
if (-not (Test-Path -LiteralPath $ExePath -PathType Leaf)) { throw 'CWAPI_PACKAGED_GUI_EXE_MISSING' }
$ExePath = (Resolve-Path -LiteralPath $ExePath).Path
$ExeDir = Split-Path -Parent $ExePath
Assert-UnderRoot -Path $ExeDir -Root $RepoRoot -Label 'exe-dir'
$ExeHash = (Get-FileHash -LiteralPath $ExePath -Algorithm SHA256).Hash.ToLowerInvariant()
Write-Host "CWAPI_PACKAGED_GUI_EXE $ExePath"
Write-Host "CWAPI_PACKAGED_GUI_EXE_SHA256=$ExeHash"

$ProbeRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("cwapi-v160-packaged-gui-" + [System.Guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $ProbeRoot -Force
$DataRoot = Join-Path $ExeDir 'CWapi-data'
Assert-UnderRoot -Path $DataRoot -Root $ExeDir -Label 'data-root'
$BackupDataRoot = Join-Path $ProbeRoot 'CWapi-data.backup'
$HadDataRoot = Test-Path -LiteralPath $DataRoot
if ($HadDataRoot) { Move-Item -LiteralPath $DataRoot -Destination $BackupDataRoot }

$FrontendEvidence = Join-Path $DataRoot 'state\frontend-ready.json'
$GUIEvidence = Join-Path $DataRoot 'state\gui-probe.json'
$HadProbeConfig = Test-Path Env:CWAPI_GUI_PROBE_CONFIG
$OldProbeConfig = $null
if ($HadProbeConfig) { $OldProbeConfig = $env:CWAPI_GUI_PROBE_CONFIG }
$env:CWAPI_GUI_PROBE_CONFIG = (@{ mode = 'first-run'; source_commit = $ExpectedCommit } | ConvertTo-Json -Compress)
Write-Host 'CWAPI_PACKAGED_GUI_EMBEDDED_PROBE mode=first-run'

$Process = $null
$CleanupFailure = $null
try {
    $Process = Start-Process -FilePath $ExePath -WorkingDirectory $ExeDir -WindowStyle Hidden -PassThru
    Start-Sleep -Milliseconds 750
    $Process.Refresh()
    if ($Process.HasExited) { throw "CWAPI_PACKAGED_GUI_EXITED_EARLY exit=$($Process.ExitCode)" }
    Write-Host "CWAPI_PACKAGED_GUI_PROCESS_STARTED pid=$($Process.Id)"

    Wait-File -Path $FrontendEvidence -TimeoutSeconds 30
    $Frontend = Get-Content -Raw -LiteralPath $FrontendEvidence | ConvertFrom-Json
    if ($Frontend.schema -ne 'cwapi.frontend-ready.v1') { throw 'CWAPI_PACKAGED_GUI_FRONTEND_SCHEMA_INVALID' }
    if ($Frontend.marker -ne 'react-mounted-v1') { throw 'CWAPI_PACKAGED_GUI_FRONTEND_MARKER_INVALID' }
    if ($Frontend.source_commit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
        throw "CWAPI_PACKAGED_GUI_FRONTEND_COMMIT_MISMATCH expected=$ExpectedCommit actual=$($Frontend.source_commit)"
    }
    Write-Host "CWAPI_PACKAGED_GUI_FRONTEND_READY_PASS marker=$($Frontend.marker)"

    Wait-File -Path $GUIEvidence -TimeoutSeconds 45
    $Probe = Get-Content -Raw -LiteralPath $GUIEvidence | ConvertFrom-Json
    if ($Probe.schema -ne 'cwapi.gui-probe.v1') { throw 'CWAPI_PACKAGED_GUI_PROBE_SCHEMA_INVALID' }
    if ($Probe.mode -ne 'first-run' -or $Probe.result.mode -ne 'first-run') { throw 'CWAPI_PACKAGED_GUI_PROBE_MODE_INVALID' }
    if ($Probe.source_commit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
        throw "CWAPI_PACKAGED_GUI_PROBE_COMMIT_MISMATCH expected=$ExpectedCommit actual=$($Probe.source_commit)"
    }
    if (-not [bool]$Probe.result.success) {
        throw "CWAPI_PACKAGED_GUI_REACT_PROBE_FAILED error=$($Probe.result.error)"
    }
    foreach ($Required in @('first-run-heading', 'first-run-controls', 'controlled-inputs')) {
        if (-not @($Probe.result.checks).Contains($Required)) {
            throw "CWAPI_PACKAGED_GUI_PROBE_CHECK_MISSING check=$Required"
        }
    }
    $Process.Refresh()
    if ($Process.HasExited) { throw "CWAPI_PACKAGED_GUI_EXITED_AFTER_PROBE exit=$($Process.ExitCode)" }
    Write-Host "CWAPI_PACKAGED_GUI_REACT_PROBE_PASS checks=$(@($Probe.result.checks).Count)"
} finally {
    $env:Path = $OriginalPath
    if ($HadProbeConfig) { $env:CWAPI_GUI_PROBE_CONFIG = $OldProbeConfig } else { Remove-Item Env:CWAPI_GUI_PROBE_CONFIG -ErrorAction SilentlyContinue }
    try { Stop-OwnedProcess -Process $Process } catch { $CleanupFailure = $_ }
    try { Remove-TreeWithRetry -Path $DataRoot } catch { if ($null -eq $CleanupFailure) { $CleanupFailure = $_ } }
    if (Test-Path -LiteralPath $BackupDataRoot) {
        try { Move-Item -LiteralPath $BackupDataRoot -Destination $DataRoot -ErrorAction Stop } catch { if ($null -eq $CleanupFailure) { $CleanupFailure = $_ } }
    }
    try { Remove-TreeWithRetry -Path $ProbeRoot } catch { if ($null -eq $CleanupFailure) { $CleanupFailure = $_ } }
}
if ($null -ne $CleanupFailure) { throw $CleanupFailure }

Invoke-Checked 'git-diff-check' { git diff --check }
$StatusAfter = @(& git status --porcelain)
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_PACKAGED_GUI_STATUS_AFTER_FAILED' }
if ($StatusAfter.Count -ne 0) {
    $StatusAfter | ForEach-Object { Write-Host $_ }
    throw 'CWAPI_PACKAGED_GUI_WORKTREE_DIRTY_AFTER'
}

Write-Host "CWAPI_PACKAGED_GUI_START_PASS commit=$ActualCommit"
