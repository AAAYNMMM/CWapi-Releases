param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,
    [Parameter(Mandatory = $true)]
    [string]$RuntimeSourceRoot,
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Z0-9]{9,32}$')]
    [string]$SlackChannelID,
    [ValidateSet('safe', 'full_access')]
    [string]$PermissionMode = 'full_access'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

function Copy-DirectoryTree {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    & robocopy $Source $Destination /E /COPY:DAT /DCOPY:DAT /R:2 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Host
    $CopyExit = $LASTEXITCODE
    if ($CopyExit -gt 7) { throw "CWAPI_REAL_SLACK_PREP_COPY_FAILED exit=$CopyExit" }
    $global:LASTEXITCODE = 0
}

$StageArgs = @{
    ExpectedCommit = $ExpectedCommit
    RuntimeSourceRoot = $RuntimeSourceRoot
}
& (Join-Path $PSScriptRoot 'stage_v161_portable.ps1') @StageArgs
if ($LASTEXITCODE -ne 0) { throw "CWAPI_REAL_SLACK_PREP_PORTABLE_FAILED exit=$LASTEXITCODE" }

$PortableRoot = Join-Path $RepoRoot 'build\stage\CWapi-v1.6.1'
$ValidationRoot = Join-Path $RepoRoot 'build\validation\CWapi-v1.6.1'
if (-not (Test-Path -LiteralPath $PortableRoot -PathType Container)) {
    throw 'CWAPI_REAL_SLACK_PREP_PORTABLE_ROOT_MISSING'
}
Remove-Item -LiteralPath $ValidationRoot -Recurse -Force -ErrorAction SilentlyContinue
Copy-DirectoryTree -Source $PortableRoot -Destination $ValidationRoot

$ConfigRoot = Join-Path $ValidationRoot 'CWapi-data\config'
New-Item -ItemType Directory -Force -Path $ConfigRoot | Out-Null
$Config = [ordered]@{
    schema = 'cwapi.config.v2'
    version = '1.6.1'
    permission_mode = $PermissionMode
    slack = [ordered]@{ channel_id = $SlackChannelID }
}
$ConfigJSON = $Config | ConvertTo-Json -Depth 3
[System.IO.File]::WriteAllText(
    (Join-Path $ConfigRoot 'cwapi.json'),
    $ConfigJSON + [Environment]::NewLine,
    (New-Object System.Text.UTF8Encoding($false))
)

foreach ($Name in @('state', 'logs', 'results', 'workspaces', 'temp')) {
    $Path = Join-Path $ValidationRoot "CWapi-data\$Name"
    if (Test-Path -LiteralPath $Path) {
        throw "CWAPI_REAL_SLACK_PREP_USER_DATA_LEAK path=$Path"
    }
}
foreach ($Relative in @(
    'CWapi.exe',
    'runtime\codex\current\bin\codex.exe',
    'runtime\git\cmd\git.exe',
    'runtime\node\node.exe',
    'runtime\mcp\playwright\node_modules\@playwright\mcp\cli.js'
)) {
    if (-not (Test-Path -LiteralPath (Join-Path $ValidationRoot $Relative) -PathType Leaf)) {
        throw "CWAPI_REAL_SLACK_PREP_RUNTIME_MISSING path=$Relative"
    }
}
if (Test-Path -LiteralPath (Join-Path $ValidationRoot 'runtime\mcp\cwapi')) {
    throw 'CWAPI_REAL_SLACK_PREP_LEGACY_PROCESS_MCP_PRESENT'
}

$Result = [ordered]@{
    schema = 'cwapi.real-slack-validation-package.v2'
    source_commit = $ExpectedCommit.ToLowerInvariant()
    permission_mode = $PermissionMode
    executable_path = (Join-Path $ValidationRoot 'CWapi.exe')
    data_root = (Join-Path $ValidationRoot 'CWapi-data')
    credentials_source = 'Windows Credential Manager current user'
}
$Result | ConvertTo-Json -Compress | Write-Host
Write-Host "CWAPI_REAL_SLACK_PREP_PASS executable=$($Result.executable_path)"
