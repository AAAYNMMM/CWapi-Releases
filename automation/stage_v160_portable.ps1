param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit,
    [Parameter(Mandatory = $true)]
    [string]$RuntimeSourceRoot,
    [string]$CodexExecutablePath = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

function Assert-UnderRepo {
    param([Parameter(Mandatory = $true)][string]$Path)
    $Root = [System.IO.Path]::GetFullPath($RepoRoot).TrimEnd('\')
    $Resolved = [System.IO.Path]::GetFullPath($Path).TrimEnd('\')
    if (-not ($Resolved.Equals($Root, [System.StringComparison]::OrdinalIgnoreCase) -or $Resolved.StartsWith($Root + '\', [System.StringComparison]::OrdinalIgnoreCase))) {
        throw "CWAPI_PORTABLE_PATH_OUTSIDE_REPO path=$Resolved"
    }
}

function Assert-SourceClean {
    param([Parameter(Mandatory = $true)][string]$Label)
    $Status = @(& git status --porcelain)
    if ($LASTEXITCODE -ne 0) { throw "CWAPI_PORTABLE_STATUS_FAILED label=$Label" }
    $Unexpected = @($Status | Where-Object { $_ -notmatch '^\?\? build/' -and $_ -notmatch '^.. build/' })
    if ($Unexpected.Count -ne 0) {
        $Unexpected | ForEach-Object { Write-Host $_ }
        throw "CWAPI_PORTABLE_SOURCE_DIRTY label=$Label"
    }
}

function Copy-DirectoryTree {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$Label
    )
    if (-not (Test-Path -LiteralPath $Source -PathType Container)) {
        throw "CWAPI_PORTABLE_COPY_SOURCE_MISSING label=$Label path=$Source"
    }
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    & robocopy $Source $Destination /E /COPY:DAT /DCOPY:DAT /R:2 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Host
    $CopyExit = $LASTEXITCODE
    if ($CopyExit -gt 7) {
        throw "CWAPI_PORTABLE_COPY_FAILED label=$Label exit=$CopyExit source=$Source destination=$Destination"
    }
    $global:LASTEXITCODE = 0
    Write-Host "CWAPI_PORTABLE_COPY_PASS label=$Label robocopy_exit=$CopyExit"
}

function New-PortableZip {
    param(
        [Parameter(Mandatory = $true)][string]$SourceRoot,
        [Parameter(Mandatory = $true)][string]$Destination
    )
    $Tar = Get-Command tar.exe -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $Tar -or -not $Tar.Source) { throw 'CWAPI_PORTABLE_TAR_UNAVAILABLE' }
    Remove-Item -LiteralPath $Destination -Force -ErrorAction SilentlyContinue
    & $Tar.Source -a -c -f $Destination -C $SourceRoot .
    $TarExit = $LASTEXITCODE
    if ($TarExit -ne 0) { throw "CWAPI_PORTABLE_ZIP_FAILED exit=$TarExit" }
    $global:LASTEXITCODE = 0
}

function Remove-PortableTransientFiles {
    param([Parameter(Mandatory = $true)][string]$Root)
    $Transient = @(Get-ChildItem -LiteralPath $Root -Recurse -Force -File | Where-Object {
        $_.Name -match '(?i)\.(log|tmp|dmp|trace)$'
    })
    foreach ($File in $Transient) {
        Remove-Item -LiteralPath $File.FullName -Force
    }
    Write-Host "CWAPI_PORTABLE_TRANSIENT_REMOVED count=$($Transient.Count)"
}

function Assert-PortablePayloadClean {
    param([Parameter(Mandatory = $true)][string]$Root)
    if (Test-Path -LiteralPath (Join-Path $Root 'CWapi-data')) {
        throw 'CWAPI_PORTABLE_USER_DATA_PRESENT'
    }
    $ForbiddenFiles = @(Get-ChildItem -LiteralPath $Root -Recurse -Force -File | Where-Object {
        $_.Name -match '(?i)^(\.env(?:\..*)?|credentials\.json|token\.json|Cookies|Login Data|Web Data|Local State|Preferences|History)$' -or
        $_.Name -match '(?i)\.(log|tmp|dmp|trace|db|sqlite|sqlite3|pfx|p12|kdbx)$'
    })
    if ($ForbiddenFiles.Count -ne 0) {
        $ForbiddenFiles | ForEach-Object { Write-Host $_.FullName }
        throw "CWAPI_PORTABLE_PRIVATE_ARTIFACT_PRESENT count=$($ForbiddenFiles.Count)"
    }
    $BrowserProfiles = @(Get-ChildItem -LiteralPath $Root -Recurse -Force -Directory | Where-Object {
        $_.Name -match '(?i)^(User Data|Default|Profile [0-9]+)$'
    })
    if ($BrowserProfiles.Count -ne 0) {
        $BrowserProfiles | ForEach-Object { Write-Host $_.FullName }
        throw "CWAPI_PORTABLE_BROWSER_PROFILE_PRESENT count=$($BrowserProfiles.Count)"
    }
}

function Read-RuntimeLock {
    param([Parameter(Mandatory = $true)][string]$Path, [Parameter(Mandatory = $true)][string]$Schema)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "CWAPI_PORTABLE_LOCK_MISSING path=$Path" }
    $Lock = Get-Content -Raw -LiteralPath $Path | ConvertFrom-Json
    if ($Lock.schema -ne $Schema) { throw "CWAPI_PORTABLE_LOCK_SCHEMA_INVALID expected=$Schema actual=$($Lock.schema)" }
    return $Lock
}

function Assert-FileSha256 {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Expected,
        [Parameter(Mandatory = $true)][string]$Label
    )
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "CWAPI_PORTABLE_FILE_MISSING label=$Label path=$Path" }
    $Actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected.ToLowerInvariant()) {
        throw "CWAPI_PORTABLE_HASH_MISMATCH label=$Label expected=$Expected actual=$Actual"
    }
    return $Actual
}

$ActualCommit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_PORTABLE_HEAD_FAILED' }
if ($ActualCommit.ToLowerInvariant() -ne $ExpectedCommit.ToLowerInvariant()) {
    throw "CWAPI_PORTABLE_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}
Assert-SourceClean -Label 'before'

$CodexLock = Read-RuntimeLock -Path (Join-Path $RepoRoot 'config\codex-runtime.lock.json') -Schema 'cwapi.codex-runtime-lock.v1'
$PortableLock = Read-RuntimeLock -Path (Join-Path $RepoRoot 'config\portable-runtime.lock.json') -Schema 'cwapi.portable-runtime-lock.v2'
$PinnedCodexCommit = ([string]$CodexLock.source_commit).ToLowerInvariant()
$PinnedCodexVersion = [string]$CodexLock.version
$PinnedCodexSha256 = ([string]$CodexLock.executable_sha256).ToLowerInvariant()
if ($PinnedCodexCommit -notmatch '^[0-9a-f]{40}$' -or $PinnedCodexSha256 -notmatch '^[0-9a-f]{64}$') {
    throw 'CWAPI_PORTABLE_CODEX_LOCK_INVALID'
}
if (([string]$PortableLock.components.codex.source_commit).ToLowerInvariant() -ne $PinnedCodexCommit -or [string]$PortableLock.components.codex.version -ne $PinnedCodexVersion -or ([string]$PortableLock.components.codex.executable_sha256).ToLowerInvariant() -ne $PinnedCodexSha256) {
    throw 'CWAPI_PORTABLE_CODEX_LOCK_DIVERGED'
}

$RuntimeSourceRoot = (Resolve-Path -LiteralPath $RuntimeSourceRoot).Path
$RuntimeRootCandidate = Join-Path $RuntimeSourceRoot 'runtime'
if (-not (Test-Path -LiteralPath $RuntimeRootCandidate -PathType Container)) {
    $RuntimeRootCandidate = $RuntimeSourceRoot
}

if (-not $CodexExecutablePath) {
    $CodexExecutablePath = Join-Path $RuntimeRootCandidate 'codex\current\bin\codex.exe'
}
$CodexExecutablePath = (Resolve-Path -LiteralPath $CodexExecutablePath).Path
$CodexHash = Assert-FileSha256 -Path $CodexExecutablePath -Expected $PinnedCodexSha256 -Label 'codex-source'
Write-Host "CWAPI_PORTABLE_CODEX_VERIFIED commit=$PinnedCodexCommit sha256=$CodexHash"

$NpmPath = (& (Join-Path $PSScriptRoot 'ensure_v160_validation_node.ps1') | Select-Object -Last 1).Trim()
$NodeRoot = Split-Path -Parent $NpmPath
$WailsPath = (& (Join-Path $PSScriptRoot 'ensure_v160_validation_wails.ps1') | Select-Object -Last 1).Trim()
if (-not (Test-Path -LiteralPath $NpmPath -PathType Leaf)) { throw 'CWAPI_PORTABLE_NPM_MISSING' }
if (-not (Test-Path -LiteralPath $WailsPath -PathType Leaf)) { throw 'CWAPI_PORTABLE_WAILS_MISSING' }

$OldPath = $env:Path
try {
    $GoPath = (Get-Command go.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $GitPath = Join-Path $RuntimeRootCandidate 'git\cmd\git.exe'
    if (-not (Test-Path -LiteralPath $GitPath -PathType Leaf)) {
        $GitPath = (Get-Command git.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    }
    $WindowsRoot = [Environment]::GetEnvironmentVariable('SystemRoot', 'Process')
    if (-not $WindowsRoot) { throw 'CWAPI_PORTABLE_SYSTEM_ROOT_MISSING' }
    $env:Path = (@(
        $NodeRoot,
        (Split-Path -Parent $GoPath),
        (Split-Path -Parent $GitPath),
        (Join-Path $WindowsRoot 'System32'),
        $WindowsRoot,
        (Join-Path $WindowsRoot 'System32\Wbem')
    ) -join ';')
    $LdFlags = "-X github.com/AAAYNMMM/CWapi/internal/buildinfo.SourceCommit=$ExpectedCommit"
    & $WailsPath build -clean -trimpath -platform windows/amd64 -ldflags $LdFlags
    if ($LASTEXITCODE -ne 0) { throw "CWAPI_PORTABLE_WAILS_BUILD_FAILED exit=$LASTEXITCODE" }
} finally {
    $env:Path = $OldPath
}

$BuiltExe = Join-Path $RepoRoot 'build\bin\CWapi.exe'
if (-not (Test-Path -LiteralPath $BuiltExe -PathType Leaf)) { throw 'CWAPI_PORTABLE_EXE_MISSING' }
$BuildMetadata = @(& $GoPath version -m $BuiltExe)
if ($LASTEXITCODE -ne 0) { throw 'CWAPI_PORTABLE_BUILD_METADATA_FAILED' }
if (-not ($BuildMetadata -match '(?m)^\s*build\s+-trimpath=true\s*$')) {
    throw 'CWAPI_PORTABLE_BUILD_TRIMPATH_MISSING'
}
$global:LASTEXITCODE = 0

$StageParent = Join-Path $RepoRoot 'build\stage'
$StageRoot = Join-Path $StageParent 'CWapi-v1.6.0'
$ZipPath = Join-Path $StageParent 'CWapi-v1.6.0.zip'
Assert-UnderRepo -Path $StageParent
Assert-UnderRepo -Path $StageRoot
Assert-UnderRepo -Path $ZipPath

Remove-Item -LiteralPath $StageRoot -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $ZipPath -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $StageRoot | Out-Null
Copy-Item -LiteralPath $BuiltExe -Destination (Join-Path $StageRoot 'CWapi.exe')

$StageRuntime = Join-Path $StageRoot 'runtime'
New-Item -ItemType Directory -Force -Path $StageRuntime | Out-Null
$CodexCurrentSource = Join-Path $RuntimeRootCandidate 'codex\current'
Copy-DirectoryTree -Source $CodexCurrentSource -Destination (Join-Path $StageRuntime 'codex\current') -Label 'codex-current'
foreach ($RequiredRuntime in @('git', 'node', 'mcp', 'browser')) {
    $Source = Join-Path $RuntimeRootCandidate $RequiredRuntime
    if (-not (Test-Path -LiteralPath $Source -PathType Container)) {
        throw "CWAPI_PORTABLE_RUNTIME_COMPONENT_MISSING component=$RequiredRuntime path=$Source"
    }
    Copy-DirectoryTree -Source $Source -Destination (Join-Path $StageRuntime $RequiredRuntime) -Label $RequiredRuntime
}
$CWapiMCPSource = Join-Path $RepoRoot 'mcp\cwapi'
$CWapiMCPDestination = Join-Path $StageRuntime 'mcp\cwapi'
Assert-UnderRepo -Path $CWapiMCPDestination
if (-not (Test-Path -LiteralPath (Join-Path $CWapiMCPSource 'process-server.cjs') -PathType Leaf) -or -not (Test-Path -LiteralPath (Join-Path $CWapiMCPSource 'process-invocation.cjs') -PathType Leaf) -or -not (Test-Path -LiteralPath (Join-Path $CWapiMCPSource 'process-output.cjs') -PathType Leaf)) {
    throw 'CWAPI_PORTABLE_PROCESS_MCP_SOURCE_MISSING'
}
if (Test-Path -LiteralPath $CWapiMCPDestination) {
    Remove-Item -LiteralPath $CWapiMCPDestination -Recurse -Force
}
Copy-DirectoryTree -Source $CWapiMCPSource -Destination $CWapiMCPDestination -Label 'cwapi-process-mcp'
Remove-PortableTransientFiles -Root $StageRoot

$StagedCodex = Join-Path $StageRuntime 'codex\current\bin\codex.exe'
Copy-Item -LiteralPath $CodexExecutablePath -Destination $StagedCodex -Force
$StagedCodexHash = Assert-FileSha256 -Path $StagedCodex -Expected $PinnedCodexSha256 -Label 'codex-staged'
$CodexVersionOutput = (& $StagedCodex --version | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or -not $CodexVersionOutput) { throw 'CWAPI_PORTABLE_STAGED_CODEX_VERSION_INVALID' }
$global:LASTEXITCODE = 0

$GitRelative = ([string]$PortableLock.components.git.executable_relative).Replace('/', '\')
$ExpectedGitVersion = ([string]$PortableLock.components.git.version).Trim()
if (-not $GitRelative -or -not $ExpectedGitVersion) { throw 'CWAPI_PORTABLE_GIT_LOCK_INVALID' }
$StagedGit = Join-Path (Join-Path $StageRuntime 'git') $GitRelative
if (-not (Test-Path -LiteralPath $StagedGit -PathType Leaf)) { throw "CWAPI_PORTABLE_STAGED_GIT_MISSING path=$StagedGit" }
$GitVersionOutput = (& $StagedGit --version).Trim()
if ($LASTEXITCODE -ne 0 -or $GitVersionOutput -notmatch [Regex]::Escape($ExpectedGitVersion)) {
    throw "CWAPI_PORTABLE_STAGED_GIT_INVALID expected=$ExpectedGitVersion actual=$GitVersionOutput"
}
$global:LASTEXITCODE = 0

$StagedNode = Join-Path $StageRuntime 'node\node.exe'
$ExpectedNodeSha = ([string]$PortableLock.components.node.executable_sha256).ToLowerInvariant()
if ($ExpectedNodeSha -notmatch '^[0-9a-f]{64}$') { throw 'CWAPI_PORTABLE_NODE_LOCK_INVALID' }
$StagedNodeHash = Assert-FileSha256 -Path $StagedNode -Expected $ExpectedNodeSha -Label 'node-staged'
$NodeVersionOutput = (& $StagedNode --version).Trim()
$ExpectedNodeVersion = 'v' + ([string]$PortableLock.components.node.version).Trim()
if ($LASTEXITCODE -ne 0 -or $NodeVersionOutput -ne $ExpectedNodeVersion) {
    throw "CWAPI_PORTABLE_STAGED_NODE_VERSION_INVALID expected=$ExpectedNodeVersion actual=$NodeVersionOutput"
}
$global:LASTEXITCODE = 0

$MCPPackagePath = Join-Path $StageRuntime 'mcp\playwright\node_modules\@playwright\mcp\package.json'
$MCPCLIPath = Join-Path $StageRuntime 'mcp\playwright\node_modules\@playwright\mcp\cli.js'
$ProcessMCPPath = Join-Path $StageRuntime 'mcp\cwapi\process-server.cjs'
$ProcessMCPInvocationPath = Join-Path $StageRuntime 'mcp\cwapi\process-invocation.cjs'
$ProcessMCPOutputPath = Join-Path $StageRuntime 'mcp\cwapi\process-output.cjs'
if (-not (Test-Path -LiteralPath $MCPPackagePath -PathType Leaf) -or -not (Test-Path -LiteralPath $MCPCLIPath -PathType Leaf) -or -not (Test-Path -LiteralPath $ProcessMCPPath -PathType Leaf) -or -not (Test-Path -LiteralPath $ProcessMCPInvocationPath -PathType Leaf) -or -not (Test-Path -LiteralPath $ProcessMCPOutputPath -PathType Leaf)) {
    throw 'CWAPI_PORTABLE_STAGED_MCP_MISSING'
}
$MCPPackage = Get-Content -Raw -LiteralPath $MCPPackagePath | ConvertFrom-Json
$ExpectedMCPVersion = ([string]$PortableLock.components.playwright_mcp.version).Trim()
if ([string]$MCPPackage.version -ne $ExpectedMCPVersion) {
    throw "CWAPI_PORTABLE_STAGED_PLAYWRIGHT_MCP_VERSION_INVALID expected=$ExpectedMCPVersion actual=$($MCPPackage.version)"
}

$BrowserRevision = ([string]$PortableLock.components.playwright_mcp.browser_revision).Trim()
$BrowserExecutable = Join-Path $StageRuntime "browser\chromium_headless_shell-$BrowserRevision\chrome-headless-shell-win64\chrome-headless-shell.exe"
if (-not (Test-Path -LiteralPath $BrowserExecutable -PathType Leaf)) {
    throw "CWAPI_PORTABLE_STAGED_BROWSER_MISSING revision=$BrowserRevision path=$BrowserExecutable"
}

$Manifest = [ordered]@{
    schema = 'cwapi.portable-manifest.v1'
    version = '1.6.0'
    source_commit = $ExpectedCommit.ToLowerInvariant()
    codex_version = $PinnedCodexVersion
    codex_version_output = $CodexVersionOutput
    codex_commit = $PinnedCodexCommit
    codex_sha256 = $StagedCodexHash
    git_version = $ExpectedGitVersion
    git_executable = "runtime/git/$([string]$PortableLock.components.git.executable_relative)"
    git_version_output = $GitVersionOutput
    node_version = [string]$PortableLock.components.node.version
    node_sha256 = $StagedNodeHash
    playwright_mcp_version = $ExpectedMCPVersion
    cwapi_process_mcp_version = '1.6.0'
    browser_revision = $BrowserRevision
    browser_version = [string]$PortableLock.components.playwright_mcp.browser_version
    relocatable = $true
    install_root = 'executable_directory'
    data_root = 'CWapi-data'
    user_data_included = $false
    staged_at = [DateTime]::UtcNow.ToString('o')
}
$ManifestJSON = $Manifest | ConvertTo-Json -Depth 4
[System.IO.File]::WriteAllText((Join-Path $StageRoot 'portable-manifest.json'), $ManifestJSON + [Environment]::NewLine, (New-Object System.Text.UTF8Encoding($false)))

Assert-PortablePayloadClean -Root $StageRoot
New-PortableZip -SourceRoot $StageRoot -Destination $ZipPath
if (-not (Test-Path -LiteralPath $ZipPath -PathType Leaf)) { throw 'CWAPI_PORTABLE_ZIP_MISSING' }
Assert-SourceClean -Label 'after'
Write-Host "CWAPI_PORTABLE_STAGE_PASS root=$StageRoot zip=$ZipPath codex_commit=$PinnedCodexCommit codex_sha256=$StagedCodexHash git=$GitVersionOutput node=$NodeVersionOutput mcp=$ExpectedMCPVersion browser_revision=$BrowserRevision"
