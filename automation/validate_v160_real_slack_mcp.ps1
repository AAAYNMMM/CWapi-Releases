param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,
    [Parameter(Mandatory = $true)]
    [string]$ExpectationsPath,
    [ValidateRange(30, 1800)]
    [int]$TimeoutSeconds = 300
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

function Assert-CleanWorktree {
    param([Parameter(Mandatory = $true)][string]$Label)
    $Status = @(& git status --porcelain)
    if ($LASTEXITCODE -ne 0) { throw "CWAPI_REAL_SLACK_STATUS_FAILED label=$Label" }
    $Unexpected = @($Status | Where-Object { $_ -notmatch '^\?\? build/' -and $_ -notmatch '^.. build/' })
    if ($Unexpected.Count -ne 0) {
        $Unexpected | ForEach-Object { Write-Host $_ }
        throw "CWAPI_REAL_SLACK_WORKTREE_DIRTY label=$Label"
    }
}

function Wait-File {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][int]$TimeoutSeconds
    )
    $Deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    while ([DateTime]::UtcNow -lt $Deadline) {
        if (Test-Path -LiteralPath $Path -PathType Leaf) { return }
        Start-Sleep -Milliseconds 250
    }
    throw "CWAPI_REAL_SLACK_EVIDENCE_TIMEOUT path=$Path"
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
    throw "CWAPI_REAL_SLACK_PROCESS_STOP_TIMEOUT pid=$($Process.Id)"
}

$ActualCommit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_REAL_SLACK_HEAD_FAILED' }
if ($ActualCommit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
    throw "CWAPI_REAL_SLACK_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}
Assert-CleanWorktree -Label 'before'

$ExecutablePath = (Resolve-Path -LiteralPath $ExecutablePath).Path
$ExpectationsPath = (Resolve-Path -LiteralPath $ExpectationsPath).Path
if (-not (Test-Path -LiteralPath $ExecutablePath -PathType Leaf)) { throw 'CWAPI_REAL_SLACK_EXE_MISSING' }
if (-not (Test-Path -LiteralPath $ExpectationsPath -PathType Leaf)) { throw 'CWAPI_REAL_SLACK_EXPECTATIONS_MISSING' }

$ExpectedDataRoot = Join-Path (Split-Path -Parent $ExecutablePath) 'CWapi-data'
if (-not (Test-Path -LiteralPath (Join-Path $ExpectedDataRoot 'config\cwapi.json') -PathType Leaf)) {
    throw "CWAPI_REAL_SLACK_CONFIG_MISSING data_root=$ExpectedDataRoot"
}
$Expectations = Get-Content -Raw -LiteralPath $ExpectationsPath | ConvertFrom-Json
if ($Expectations.schema -ne 'cwapi.slack-mcp-e2e.expectations.v1' -or @($Expectations.requests).Count -lt 1) {
    throw 'CWAPI_REAL_SLACK_EXPECTATIONS_INVALID'
}

$StateRoot = Join-Path $ExpectedDataRoot 'state'
$FrontendEvidence = Join-Path $StateRoot 'frontend-ready.json'
$ProbeEvidence = Join-Path $StateRoot 'gui-probe.json'
Remove-Item -LiteralPath $FrontendEvidence -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $ProbeEvidence -Force -ErrorAction SilentlyContinue

$ProbeConfig = [ordered]@{
    mode = 'real-slack'
    source_commit = $ExpectedCommit.ToLowerInvariant()
    timeout_seconds = $TimeoutSeconds
    expectations = $Expectations
}
$ProbeJSON = $ProbeConfig | ConvertTo-Json -Depth 12 -Compress
if ([Text.Encoding]::UTF8.GetByteCount($ProbeJSON) -gt 65536) { throw 'CWAPI_REAL_SLACK_PROBE_CONFIG_TOO_LARGE' }

$HadProbeConfig = Test-Path Env:CWAPI_GUI_PROBE_CONFIG
$OldProbeConfig = $null
if ($HadProbeConfig) { $OldProbeConfig = $env:CWAPI_GUI_PROBE_CONFIG }
$env:CWAPI_GUI_PROBE_CONFIG = $ProbeJSON
Write-Host "CWAPI_REAL_SLACK_EMBEDDED_PROBE requests=$(@($Expectations.requests).Count) timeout=$TimeoutSeconds"

$Process = $null
try {
    $Process = Start-Process -FilePath $ExecutablePath -WorkingDirectory (Split-Path -Parent $ExecutablePath) -WindowStyle Hidden -PassThru
    Start-Sleep -Milliseconds 750
    $Process.Refresh()
    if ($Process.HasExited) { throw "CWAPI_REAL_SLACK_GUI_EXITED_EARLY exit=$($Process.ExitCode)" }
    Write-Host "CWAPI_REAL_SLACK_GUI_STARTED pid=$($Process.Id)"

    Wait-File -Path $FrontendEvidence -TimeoutSeconds 30
    $Frontend = Get-Content -Raw -LiteralPath $FrontendEvidence | ConvertFrom-Json
    if ($Frontend.schema -ne 'cwapi.frontend-ready.v1' -or $Frontend.marker -ne 'react-mounted-v1') {
        throw 'CWAPI_REAL_SLACK_FRONTEND_READY_INVALID'
    }
    if ($Frontend.source_commit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
        throw "CWAPI_REAL_SLACK_FRONTEND_COMMIT_MISMATCH expected=$ExpectedCommit actual=$($Frontend.source_commit)"
    }
    Write-Host 'CWAPI_REAL_SLACK_FRONTEND_READY_PASS'

    Wait-File -Path $ProbeEvidence -TimeoutSeconds ($TimeoutSeconds + 30)
    $Probe = Get-Content -Raw -LiteralPath $ProbeEvidence | ConvertFrom-Json
    if ($Probe.schema -ne 'cwapi.gui-probe.v1' -or $Probe.mode -ne 'real-slack' -or $Probe.result.mode -ne 'real-slack') {
        throw 'CWAPI_REAL_SLACK_PROBE_EVIDENCE_INVALID'
    }
    if ($Probe.source_commit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
        throw "CWAPI_REAL_SLACK_PROBE_COMMIT_MISMATCH expected=$ExpectedCommit actual=$($Probe.source_commit)"
    }
    if (-not [bool]$Probe.result.success) {
        throw "CWAPI_REAL_SLACK_MCP_PROBE_FAILED error=$($Probe.result.error)"
    }
    $Evidence = $Probe.result.evidence
    if ($Evidence.schema -ne 'cwapi.slack-mcp-e2e.result.v1') { throw 'CWAPI_REAL_SLACK_RESULT_SCHEMA_INVALID' }
    if (-not [bool]$Evidence.slack_socket_ready -or -not [bool]$Evidence.mcp_runtime_ready -or -not [bool]$Evidence.codex_ready) {
        throw 'CWAPI_REAL_SLACK_READINESS_RESULT_INVALID'
    }
    if (@($Evidence.requests).Count -ne @($Expectations.requests).Count) {
        throw "CWAPI_REAL_SLACK_REQUEST_COUNT_MISMATCH expected=$(@($Expectations.requests).Count) actual=$(@($Evidence.requests).Count)"
    }
    $Process.Refresh()
    if ($Process.HasExited) { throw "CWAPI_REAL_SLACK_GUI_EXITED_AFTER_PROBE exit=$($Process.ExitCode)" }
    $Evidence | ConvertTo-Json -Depth 12 -Compress | ForEach-Object { Write-Host "CWAPI_REAL_SLACK_MCP_E2E_PASS $_" }
} finally {
    if ($HadProbeConfig) { $env:CWAPI_GUI_PROBE_CONFIG = $OldProbeConfig } else { Remove-Item Env:CWAPI_GUI_PROBE_CONFIG -ErrorAction SilentlyContinue }
    Stop-OwnedProcess -Process $Process
}

Assert-CleanWorktree -Label 'after'
Write-Host "CWAPI_REAL_SLACK_MCP_GATE_PASS commit=$ActualCommit executable=$ExecutablePath"
