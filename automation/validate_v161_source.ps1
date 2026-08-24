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

function Assert-Clean {
    param([string]$Label)
    $Status = @(& git status --porcelain)
    if ($LASTEXITCODE -ne 0) { throw "CWAPI_SOURCE_STATUS_FAILED label=$Label" }
    if ($Status.Count -ne 0) {
        $Status | Select-Object -First 80 | ForEach-Object { Write-Host $_ }
        throw "CWAPI_SOURCE_WORKTREE_DIRTY label=$Label"
    }
}

function Invoke-Checked {
    param([string]$Label, [scriptblock]$Command)
    Write-Host "CWAPI_SOURCE_STEP_START $Label"
    & $Command
    if ($LASTEXITCODE -ne 0) { throw "CWAPI_SOURCE_STEP_FAILED label=$Label exit=$LASTEXITCODE" }
    Write-Host "CWAPI_SOURCE_STEP_PASS $Label"
}

$ActualCommit = (& git rev-parse HEAD).Trim().ToLowerInvariant()
if ($LASTEXITCODE -ne 0 -or $ActualCommit -ne $ExpectedCommit) {
    throw "CWAPI_SOURCE_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}
Assert-Clean 'before'

$ForbiddenPaths = @(
    'internal/projects',
    'internal/app/projects.go',
    'internal/gateway/mcp_projects.go',
    'internal/gateway/mcp_process_context.go',
    'mcp/cwapi',
    'runtime/mcp/cwapi'
)
foreach ($Path in $ForbiddenPaths) {
    $Tracked = @(& git ls-files -- $Path)
    if ($LASTEXITCODE -ne 0) { throw "CWAPI_SOURCE_RETIRED_PATH_CHECK_FAILED path=$Path" }
    if ($Tracked.Count -ne 0) { throw "CWAPI_SOURCE_RETIRED_PATH_PRESENT path=$Path" }
}

$Config = Get-Content -Raw -LiteralPath 'config/cwapi.example.json' | ConvertFrom-Json
if ($Config.schema -ne 'cwapi.config.v2' -or $Config.version -ne '1.6.1') {
    throw 'CWAPI_SOURCE_CONFIG_VERSION_INVALID'
}
$ConfigFields = @($Config.PSObject.Properties.Name | Sort-Object)
$SlackFields = @($Config.slack.PSObject.Properties.Name | Sort-Object)
if (($ConfigFields -join ',') -ne 'permission_mode,schema,slack,version' -or ($SlackFields -join ',') -ne 'channel_id') {
    throw 'CWAPI_SOURCE_CONFIG_SHAPE_INVALID'
}

$Protocol = Get-Content -Raw -LiteralPath 'internal/protocol/mcp.go'
foreach ($Required in @('[CWapi/MCP/2]', 'cwapi-mcp/2', 'cwapi.mcp.request.v2', 'cwapi.mcp.response.v2')) {
    if (-not $Protocol.Contains($Required)) { throw "CWAPI_SOURCE_PROTOCOL_TOKEN_MISSING token=$Required" }
}
$SlackProtocol = Get-Content -Raw -LiteralPath 'internal/slack/protocol.go'
if (-not $SlackProtocol.Contains('MCPProtocolPrefix = "[CWapi/MCP/2]"')) {
    throw 'CWAPI_SOURCE_SLACK_PROTOCOL_INVALID'
}

$Production = @(
    Get-ChildItem -LiteralPath 'internal' -Recurse -File -Filter '*.go' |
        Where-Object { $_.Name -notlike '*_test.go' }
    Get-Item -LiteralPath 'app.go'
    Get-Item -LiteralPath 'main.go'
)
foreach ($File in $Production) {
    $Text = Get-Content -Raw -LiteralPath $File.FullName
    foreach ($Forbidden in @('ProcessMCPReady', 'mcp_servers.cwapi', 'projects/list', 'ProjectPaths', 'cwapi.mcp.event.v1')) {
        if ($Text.Contains($Forbidden)) {
            throw "CWAPI_SOURCE_RETIRED_TOKEN path=$($File.FullName) token=$Forbidden"
        }
    }
}

$GoFiles = @($Production) + @(
    Get-ChildItem -LiteralPath 'internal' -Recurse -File -Filter '*_test.go'
    Get-ChildItem -LiteralPath '.' -File -Filter '*_test.go'
)
$Unformatted = @(& gofmt -l @($GoFiles.FullName) 2>&1)
if ($LASTEXITCODE -ne 0 -or $Unformatted.Count -ne 0) {
    $Unformatted | Select-Object -First 80 | ForEach-Object { Write-Host $_ }
    throw 'CWAPI_SOURCE_GOFMT_REQUIRED'
}

Invoke-Checked 'go-test' { go test ./... }

$NpmOutput = @(& (Join-Path $PSScriptRoot 'ensure_v161_validation_node.ps1'))
if ($LASTEXITCODE -ne 0 -or $NpmOutput.Count -eq 0) { throw 'CWAPI_SOURCE_PINNED_NODE_FAILED' }
$NpmPath = ([string]($NpmOutput | Select-Object -Last 1)).Trim()
$OriginalPath = $env:Path
$OriginalPathExt = $env:PATHEXT
$WindowsRoot = [Environment]::GetEnvironmentVariable('SystemRoot', 'Process')
if (-not $WindowsRoot) { throw 'CWAPI_SOURCE_SYSTEM_ROOT_MISSING' }
$env:Path = @(
    (Split-Path -Parent $NpmPath),
    (Join-Path $WindowsRoot 'System32'),
    $WindowsRoot
) -join [System.IO.Path]::PathSeparator
$env:PATHEXT = '.COM;.EXE;.BAT;.CMD'
Push-Location 'frontend'
try {
    Invoke-Checked 'frontend-install' { & $NpmPath ci --prefer-offline --no-audit --no-fund }
    Invoke-Checked 'frontend-test' { & $NpmPath test }
    Invoke-Checked 'frontend-build' { & $NpmPath run build }
} finally {
    Pop-Location
    $env:Path = $OriginalPath
    $env:PATHEXT = $OriginalPathExt
}

Invoke-Checked 'git-diff-check' { git diff --check }
Assert-Clean 'after'
Write-Host "CWAPI_V161_SOURCE_PASS commit=$ActualCommit"
