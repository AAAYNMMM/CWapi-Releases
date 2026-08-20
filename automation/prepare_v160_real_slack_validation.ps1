param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,
    [Parameter(Mandatory = $true)]
    [string]$RuntimeSourceRoot,
    [string]$ConfiguredDataRoot = '',
    [string]$SlackChannelID = '',
    [string]$ProjectRepository = '',
    [string]$ProjectPath = '',
    [string]$ProjectRemoteURL = '',
    [string]$ProjectDisplayName = 'CWapi'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

function Resolve-ConfiguredConfig {
    if (-not $ConfiguredDataRoot) { return $null }
    if (-not (Test-Path -LiteralPath $ConfiguredDataRoot -PathType Container)) { return $null }
    $Root = (Resolve-Path -LiteralPath $ConfiguredDataRoot).Path
    $Path = Join-Path $Root 'config\cwapi.json'
    if (Test-Path -LiteralPath $Path -PathType Leaf) { return $Path }
    return $null
}

function Copy-DirectoryTree {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$Label
    )
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    & robocopy $Source $Destination /E /COPY:DAT /DCOPY:DAT /R:2 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Host
    $CopyExit = $LASTEXITCODE
    if ($CopyExit -gt 7) {
        throw "CWAPI_REAL_SLACK_PREP_COPY_FAILED label=$Label exit=$CopyExit"
    }
    $global:LASTEXITCODE = 0
    Write-Host "CWAPI_REAL_SLACK_PREP_COPY_PASS label=$Label robocopy_exit=$CopyExit"
}

function Write-ValidationConfig {
    param([Parameter(Mandatory = $true)][string]$Destination, [object]$SourceConfig = $null)

    if ($null -ne $SourceConfig) {
        $Config = $SourceConfig
        $Config | Add-Member -NotePropertyName permission_mode -NotePropertyValue 'safe' -Force
    } else {
        if ($SlackChannelID -notmatch '^[A-Z0-9]{9,32}$') { throw 'CWAPI_REAL_SLACK_PREP_CHANNEL_INVALID' }
        if ($ProjectRepository -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') { throw 'CWAPI_REAL_SLACK_PREP_REPOSITORY_INVALID' }
        if (-not $ProjectPath -or -not (Test-Path -LiteralPath $ProjectPath -PathType Container)) { throw 'CWAPI_REAL_SLACK_PREP_PROJECT_PATH_INVALID' }
        $ResolvedProjectPath = (Resolve-Path -LiteralPath $ProjectPath).Path
        if (-not $ProjectRemoteURL -or $ProjectRemoteURL.Length -gt 2048) { throw 'CWAPI_REAL_SLACK_PREP_REMOTE_INVALID' }
        if (-not $ProjectDisplayName -or $ProjectDisplayName.Length -gt 120) { throw 'CWAPI_REAL_SLACK_PREP_DISPLAY_NAME_INVALID' }

        $Config = [ordered]@{
            schema = 'cwapi.config.v1'
            version = '1.6.0'
            permission_mode = 'safe'
            slack = [ordered]@{ channel_id = $SlackChannelID }
            projects = @(
                [ordered]@{
                    id = 'prj-160000000000000000000001'
                    display_name = $ProjectDisplayName
                    repository = $ProjectRepository
                    local_path = $ResolvedProjectPath
                    remote_url = $ProjectRemoteURL
                }
            )
        }
    }

    if ($Config.schema -ne 'cwapi.config.v1') { throw 'CWAPI_REAL_SLACK_PREP_CONFIG_SCHEMA_INVALID' }
    if ($Config.permission_mode -ne 'safe') { throw 'CWAPI_REAL_SLACK_PREP_PERMISSION_MODE_INVALID' }
    $JSON = $Config | ConvertTo-Json -Depth 8
    [System.IO.File]::WriteAllText($Destination, $JSON + [Environment]::NewLine, (New-Object System.Text.UTF8Encoding($false)))
    Write-Host 'CWAPI_REAL_SLACK_PREP_PERMISSION_SAFE'
}

& (Join-Path $PSScriptRoot 'stage_v160_portable.ps1') `
    -ExpectedCommit $ExpectedCommit `
    -RuntimeSourceRoot $RuntimeSourceRoot
if ($LASTEXITCODE -ne 0) {
    throw "CWAPI_REAL_SLACK_PREP_PORTABLE_FAILED exit=$LASTEXITCODE"
}

$PortableRoot = Join-Path $RepoRoot 'build\stage\CWapi-v1.6.0'
$ValidationRoot = Join-Path $RepoRoot 'build\validation\CWapi-v1.6.0'
if (-not (Test-Path -LiteralPath $PortableRoot -PathType Container)) {
    throw 'CWAPI_REAL_SLACK_PREP_PORTABLE_ROOT_MISSING'
}

Remove-Item -LiteralPath $ValidationRoot -Recurse -Force -ErrorAction SilentlyContinue
Copy-DirectoryTree -Source $PortableRoot -Destination $ValidationRoot -Label 'portable-to-validation'

$ValidationConfigRoot = Join-Path $ValidationRoot 'CWapi-data\config'
New-Item -ItemType Directory -Force -Path $ValidationConfigRoot | Out-Null
$ValidationConfig = Join-Path $ValidationConfigRoot 'cwapi.json'
$ConfiguredConfig = Resolve-ConfiguredConfig
if ($ConfiguredConfig) {
    $SourceConfig = Get-Content -Raw -LiteralPath $ConfiguredConfig | ConvertFrom-Json
    Write-ValidationConfig -Destination $ValidationConfig -SourceConfig $SourceConfig
    Write-Host "CWAPI_REAL_SLACK_PREP_CONFIG_COPIED path=$ConfiguredConfig"
} else {
    Write-ValidationConfig -Destination $ValidationConfig
}

$Forbidden = @(
    'state',
    'logs',
    'results',
    'mcp-workspaces',
    'mcp-resources',
    'temp'
)
foreach ($Name in $Forbidden) {
    $Path = Join-Path $ValidationRoot "CWapi-data\$Name"
    if (Test-Path -LiteralPath $Path) {
        throw "CWAPI_REAL_SLACK_PREP_USER_DATA_LEAK path=$Path"
    }
}

$ExecutablePath = Join-Path $ValidationRoot 'CWapi.exe'
$CodexPath = Join-Path $ValidationRoot 'runtime\codex\current\bin\codex.exe'
$GitPath = Join-Path $ValidationRoot 'runtime\git\cmd\git.exe'
if (-not (Test-Path -LiteralPath $ExecutablePath -PathType Leaf)) { throw 'CWAPI_REAL_SLACK_PREP_EXE_MISSING' }
if (-not (Test-Path -LiteralPath $CodexPath -PathType Leaf)) { throw 'CWAPI_REAL_SLACK_PREP_CODEX_MISSING' }
if (-not (Test-Path -LiteralPath $GitPath -PathType Leaf)) { throw 'CWAPI_REAL_SLACK_PREP_GIT_MISSING' }

$Result = [ordered]@{
    schema = 'cwapi.real-slack-validation-package.v1'
    source_commit = $ExpectedCommit.ToLowerInvariant()
    permission_mode = 'safe'
    executable_path = $ExecutablePath
    git_path = $GitPath
    data_root = (Join-Path $ValidationRoot 'CWapi-data')
    config_path = $ValidationConfig
    credentials_source = 'Windows Credential Manager current user'
}
$Result | ConvertTo-Json -Compress | Write-Host
Write-Host "CWAPI_REAL_SLACK_PREP_PASS executable=$ExecutablePath"
