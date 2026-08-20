param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot
$ExpectedCommit = $ExpectedCommit.ToLowerInvariant()

$ActualCommit = (& git rev-parse HEAD).Trim().ToLowerInvariant()
if ($LASTEXITCODE -ne 0 -or $ActualCommit -ne $ExpectedCommit) {
    throw "CWAPI_NO_LEGACY_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}

$ForbiddenExact = @(
    'internal/protocol/protocol.go',
    'internal/protocol/protocol_test.go',
    'internal/gateway/mcp_only.go',
    'config/cwapi.example.yaml',
    'config/codex-capabilities.yaml',
    'config/codex-home.example.toml',
    'scripts/bootstrap.ps1',
    'scripts/install_scheduled_task.ps1',
    'scripts/assemble_portable_runtime_v160.ps1',
    'scripts/build_complete_portable_v160.ps1',
    'scripts/build_portable_release_v160.ps1',
    'scripts/build_codex_fork_package.ps1',
    'scripts/install_codex_fork_runtime.ps1',
    'scripts/install_codex_fork_source_runtime.ps1',
    'scripts/prepare_codex_fork.ps1',
    'scripts/configure_codex_home.ps1',
    'scripts/check_codex_runtime_lock.py',
    'automation/codex_session_acceptance.py',
    'automation/slack_mcp_e2e_probe_v160.cjs',
    'automation/s15_live_target.ps1',
    'automation/validate_v160_codex_command_routing.ps1',
    'automation/validate_v160_codex_restore.ps1'
)
foreach ($Path in $ForbiddenExact) {
    if (Test-Path -LiteralPath $Path) {
        throw "CWAPI_NO_LEGACY_FILE_PRESENT path=$Path"
    }
}

$ForbiddenPatterns = @(
    'automation/smoke_v160_s1*',
    'automation/validate_v160_s1*.ps1',
    'automation/validate_v160_complete_product*.ps1',
    'scripts/*codex_fork*'
)
foreach ($Pattern in $ForbiddenPatterns) {
    $Matches = @(Get-ChildItem -Path $Pattern -File -ErrorAction SilentlyContinue)
    if ($Matches.Count -ne 0) {
        throw "CWAPI_NO_LEGACY_PATTERN_PRESENT pattern=$Pattern files=$($Matches.FullName -join ';')"
    }
}

if (-not (Test-Path -LiteralPath 'config/cwapi.example.json' -PathType Leaf)) {
    throw 'CWAPI_NO_LEGACY_CURRENT_CONFIG_EXAMPLE_MISSING'
}
$ConfigExample = Get-Content -Raw -LiteralPath 'config/cwapi.example.json'
$LegacyCredentialsFile = 'credentials' + '.json'
$LegacyTokenFile = 'token' + '.json'
foreach ($Forbidden in @('"runner"', '"security"', 'allowed_actions', 'allowed_repositories', 'gmail', $LegacyCredentialsFile, $LegacyTokenFile)) {
    if ($ConfigExample.IndexOf($Forbidden, [StringComparison]::OrdinalIgnoreCase) -ge 0) {
        throw "CWAPI_NO_LEGACY_CONFIG_TOKEN token=$Forbidden"
    }
}

$SlackProtocol = Get-Content -Raw -LiteralPath 'internal/slack/protocol.go'
if (-not $SlackProtocol.Contains('MCPProtocolPrefix = "[CWapi/MCP/1]"')) {
    throw 'CWAPI_NO_LEGACY_MCP_SLACK_PREFIX_MISSING'
}
if ($SlackProtocol.Contains('[CWapi/1]')) {
    throw 'CWAPI_NO_LEGACY_SLACK_PREFIX_PRESENT'
}

$SourceFiles = @(
    Get-ChildItem -LiteralPath 'internal' -Recurse -File -Filter '*.go' |
        Where-Object { $_.Name -notlike '*_test.go' }
)
$SourceFiles += Get-Item -LiteralPath 'app.go'
$SourceFiles += Get-Item -LiteralPath 'main.go'
foreach ($File in $SourceFiles) {
    $Text = Get-Content -Raw -LiteralPath $File.FullName
    foreach ($Forbidden in @('[CWapi/1]', 'cwapi.task.v1', 'cwapi.ack.v1', 'cwapi.progress.v1', 'cwapi.result.v1', 'TaskSchema', 'FamilyTask', 'BuildTask(', 'ParseSubject(', 'MCP_CANCEL', 'cancel_requested')) {
        if ($Text.Contains($Forbidden)) {
            throw "CWAPI_NO_LEGACY_PROTOCOL_TOKEN path=$($File.FullName) token=$Forbidden"
        }
    }
}

$SelfPath = (Resolve-Path -LiteralPath $PSCommandPath).Path
$RunnableRoots = @('scripts', 'automation')
foreach ($Root in $RunnableRoots) {
    foreach ($File in @(Get-ChildItem -LiteralPath $Root -Recurse -File -ErrorAction SilentlyContinue)) {
        if ($File.FullName -eq $SelfPath) { continue }
        $Text = Get-Content -Raw -LiteralPath $File.FullName -ErrorAction SilentlyContinue
        if ($null -eq $Text) { continue }
        foreach ($Forbidden in @('auth-gmail', 'runner-start', 'Gmail draft', $LegacyCredentialsFile, 'config\cwapi.yaml', 'cwapi.codex_toolhost')) {
            if ($Text.IndexOf($Forbidden, [StringComparison]::OrdinalIgnoreCase) -ge 0) {
                throw "CWAPI_NO_LEGACY_RUNNABLE_TOKEN path=$($File.FullName) token=$Forbidden"
            }
        }
    }
}

Write-Host "CWAPI_NO_LEGACY_COMPAT_PASS commit=$ActualCommit"
