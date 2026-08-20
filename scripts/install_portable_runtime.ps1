param(
    [string]$ProjectRoot = (Split-Path -Parent $PSScriptRoot),
    [string]$DownloadCache = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path
$PortableLockPath = Join-Path $ProjectRoot 'config\portable-runtime.lock.json'
if (-not (Test-Path -LiteralPath $PortableLockPath -PathType Leaf)) {
    throw "CWAPI_PORTABLE_RUNTIME_LOCK_MISSING path=$PortableLockPath"
}
$Lock = Get-Content -Raw -LiteralPath $PortableLockPath | ConvertFrom-Json
if ($Lock.schema -ne 'cwapi.portable-runtime-lock.v2') {
    throw "CWAPI_PORTABLE_RUNTIME_LOCK_INVALID schema=$($Lock.schema)"
}

if (-not $DownloadCache) {
    $DownloadCache = Join-Path $ProjectRoot 'build\runtime-cache'
}
New-Item -ItemType Directory -Force -Path $DownloadCache | Out-Null
$RuntimeRoot = Join-Path $ProjectRoot 'runtime'
New-Item -ItemType Directory -Force -Path $RuntimeRoot | Out-Null

function Get-VerifiedArchive {
    param(
        [Parameter(Mandatory = $true)][string]$URL,
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Sha256
    )
    if ($Sha256 -notmatch '^[0-9a-fA-F]{64}$') { throw "CWAPI_PORTABLE_RUNTIME_ARCHIVE_SHA_INVALID name=$Name" }
    $Path = Join-Path $DownloadCache $Name
    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        $ExistingHash = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($ExistingHash -eq $Sha256.ToLowerInvariant()) { return $Path }
        Remove-Item -LiteralPath $Path -Force
    }
    $Temporary = $Path + '.download'
    Remove-Item -LiteralPath $Temporary -Force -ErrorAction SilentlyContinue
    Invoke-WebRequest -UseBasicParsing -Uri $URL -OutFile $Temporary
    $ActualHash = (Get-FileHash -LiteralPath $Temporary -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($ActualHash -ne $Sha256.ToLowerInvariant()) {
        Remove-Item -LiteralPath $Temporary -Force -ErrorAction SilentlyContinue
        throw "CWAPI_PORTABLE_RUNTIME_ARCHIVE_HASH_MISMATCH name=$Name expected=$Sha256 actual=$ActualHash"
    }
    Move-Item -LiteralPath $Temporary -Destination $Path -Force
    return $Path
}

function Reset-Directory {
    param([Parameter(Mandatory = $true)][string]$Path)
    $Root = [System.IO.Path]::GetFullPath($ProjectRoot).TrimEnd('\')
    $Resolved = [System.IO.Path]::GetFullPath($Path).TrimEnd('\')
    if ($Resolved.Equals($Root, [System.StringComparison]::OrdinalIgnoreCase) -or -not $Resolved.StartsWith($Root + '\', [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "CWAPI_PORTABLE_RESET_PATH_INVALID path=$Resolved"
    }
    Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
}

function Expand-Fresh {
    param([Parameter(Mandatory = $true)][string]$Archive, [Parameter(Mandatory = $true)][string]$Label)
    $Temp = Join-Path ([System.IO.Path]::GetTempPath()) ("cwapi-runtime-$Label-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $Temp | Out-Null
    Expand-Archive -LiteralPath $Archive -DestinationPath $Temp -Force
    return $Temp
}

$Codex = $Lock.components.codex
$CodexArchive = Get-VerifiedArchive -URL ([string]$Codex.url) -Name ([string]$Codex.archive_name) -Sha256 ([string]$Codex.sha256)
Write-Host 'CWAPI_PORTABLE_RUNTIME_CODEX_START'
& (Join-Path $PSScriptRoot 'install_codex_runtime.ps1') `
    -ArchivePath $CodexArchive `
    -Version ([string]$Codex.version) `
    -ExpectedSha256 ([string]$Codex.sha256) `
    -ProjectRoot $ProjectRoot
if ($LASTEXITCODE -ne 0) { throw "CWAPI_PORTABLE_RUNTIME_CODEX_FAILED exit=$LASTEXITCODE" }
$global:LASTEXITCODE = 0
$CodexExecutable = Join-Path $RuntimeRoot 'codex\current\bin\codex.exe'
if (-not (Test-Path -LiteralPath $CodexExecutable -PathType Leaf)) { throw "CWAPI_PORTABLE_RUNTIME_CODEX_MISSING path=$CodexExecutable" }
$CodexHash = (Get-FileHash -LiteralPath $CodexExecutable -Algorithm SHA256).Hash.ToLowerInvariant()
if ($CodexHash -ne ([string]$Codex.executable_sha256).ToLowerInvariant()) {
    throw "CWAPI_PORTABLE_RUNTIME_CODEX_HASH_MISMATCH expected=$($Codex.executable_sha256) actual=$CodexHash"
}
Write-Host "CWAPI_PORTABLE_RUNTIME_CODEX_PASS version=$($Codex.version) sha256=$CodexHash"

$Git = $Lock.components.git
$GitArchive = Get-VerifiedArchive -URL ([string]$Git.url) -Name ([string]$Git.archive_name) -Sha256 ([string]$Git.sha256)
$GitRoot = Join-Path $RuntimeRoot 'git'
$GitTemp = Expand-Fresh -Archive $GitArchive -Label 'git'
try {
    Reset-Directory -Path $GitRoot
    Get-ChildItem -LiteralPath $GitTemp -Force | ForEach-Object { Move-Item -LiteralPath $_.FullName -Destination $GitRoot }
} finally {
    Remove-Item -LiteralPath $GitTemp -Recurse -Force -ErrorAction SilentlyContinue
}
$GitExecutable = Join-Path $GitRoot (([string]$Git.executable_relative).Replace('/', '\'))
if (-not (Test-Path -LiteralPath $GitExecutable -PathType Leaf)) { throw "CWAPI_PORTABLE_RUNTIME_GIT_MISSING path=$GitExecutable" }
$GitVersionOutput = (& $GitExecutable --version).Trim()
if ($LASTEXITCODE -ne 0 -or $GitVersionOutput -notmatch [Regex]::Escape(([string]$Git.version))) {
    throw "CWAPI_PORTABLE_RUNTIME_GIT_VERSION_INVALID expected=$($Git.version) actual=$GitVersionOutput"
}
$global:LASTEXITCODE = 0
$GitManifest = [ordered]@{ schema='cwapi.git-runtime.v1'; version=[string]$Git.version; archive_sha256=([string]$Git.sha256).ToLowerInvariant(); executable_relative=[string]$Git.executable_relative }
$GitManifest | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $GitRoot 'runtime.json') -Encoding UTF8
Write-Host "CWAPI_PORTABLE_RUNTIME_GIT_PASS version=$($Git.version)"

$Node = $Lock.components.node
$NodeArchive = Get-VerifiedArchive -URL ([string]$Node.url) -Name ([string]$Node.archive_name) -Sha256 ([string]$Node.sha256)
$NodeRoot = Join-Path $RuntimeRoot 'node'
$NodeTemp = Expand-Fresh -Archive $NodeArchive -Label 'node'
try {
    $ExtractedNodeRoot = Join-Path $NodeTemp "node-v$($Node.version)-win-x64"
    if (-not (Test-Path -LiteralPath (Join-Path $ExtractedNodeRoot 'node.exe') -PathType Leaf)) {
        throw "CWAPI_PORTABLE_RUNTIME_NODE_LAYOUT_INVALID path=$ExtractedNodeRoot"
    }
    Reset-Directory -Path $NodeRoot
    Get-ChildItem -LiteralPath $ExtractedNodeRoot -Force | ForEach-Object { Move-Item -LiteralPath $_.FullName -Destination $NodeRoot }
} finally {
    Remove-Item -LiteralPath $NodeTemp -Recurse -Force -ErrorAction SilentlyContinue
}
$NodeExecutable = Join-Path $NodeRoot 'node.exe'
$NodeHash = (Get-FileHash -LiteralPath $NodeExecutable -Algorithm SHA256).Hash.ToLowerInvariant()
if ($NodeHash -ne ([string]$Node.executable_sha256).ToLowerInvariant()) {
    throw "CWAPI_PORTABLE_RUNTIME_NODE_HASH_MISMATCH expected=$($Node.executable_sha256) actual=$NodeHash"
}
$NodeVersionOutput = (& $NodeExecutable --version).Trim()
if ($LASTEXITCODE -ne 0 -or $NodeVersionOutput -ne "v$($Node.version)") {
    throw "CWAPI_PORTABLE_RUNTIME_NODE_VERSION_INVALID expected=v$($Node.version) actual=$NodeVersionOutput"
}
$global:LASTEXITCODE = 0
Write-Host "CWAPI_PORTABLE_RUNTIME_NODE_PASS version=$($Node.version) sha256=$NodeHash"

Write-Host 'CWAPI_PORTABLE_RUNTIME_MCP_START'
& (Join-Path $PSScriptRoot 'install_playwright_mcp.ps1') -ProjectRoot $ProjectRoot
if ($LASTEXITCODE -ne 0) { throw "CWAPI_PORTABLE_RUNTIME_MCP_FAILED exit=$LASTEXITCODE" }
$global:LASTEXITCODE = 0
$CWapiMCPSource = Join-Path $ProjectRoot 'mcp\cwapi'
$CWapiMCPRoot = Join-Path $RuntimeRoot 'mcp\cwapi'
Reset-Directory -Path $CWapiMCPRoot
foreach ($ProcessMCPFile in @('process-server.cjs', 'process-invocation.cjs', 'process-output.cjs')) {
    $ProcessMCPSource = Join-Path $CWapiMCPSource $ProcessMCPFile
    if (-not (Test-Path -LiteralPath $ProcessMCPSource -PathType Leaf)) { throw "CWAPI_PROCESS_MCP_SOURCE_MISSING file=$ProcessMCPFile" }
    Copy-Item -LiteralPath $ProcessMCPSource -Destination $CWapiMCPRoot
}

$Browser = $Lock.components.playwright_mcp
$BrowserArchive = Get-VerifiedArchive -URL ([string]$Browser.browser_url) -Name ([string]$Browser.browser_archive_name) -Sha256 ([string]$Browser.browser_sha256)
$BrowserRoot = Join-Path $RuntimeRoot 'browser'
Reset-Directory -Path $BrowserRoot
$BrowserRevisionRoot = Join-Path $BrowserRoot "chromium_headless_shell-$($Browser.browser_revision)"
New-Item -ItemType Directory -Force -Path $BrowserRevisionRoot | Out-Null
$BrowserTemp = Expand-Fresh -Archive $BrowserArchive -Label 'browser'
try {
    $ExtractedBrowserRoot = Join-Path $BrowserTemp 'chrome-headless-shell-win64'
    $ExtractedBrowserExe = Join-Path $ExtractedBrowserRoot 'chrome-headless-shell.exe'
    if (-not (Test-Path -LiteralPath $ExtractedBrowserExe -PathType Leaf)) {
        throw "CWAPI_PORTABLE_RUNTIME_BROWSER_LAYOUT_INVALID path=$ExtractedBrowserExe"
    }
    Move-Item -LiteralPath $ExtractedBrowserRoot -Destination $BrowserRevisionRoot
} finally {
    Remove-Item -LiteralPath $BrowserTemp -Recurse -Force -ErrorAction SilentlyContinue
}
$BrowserExecutable = Join-Path $BrowserRevisionRoot 'chrome-headless-shell-win64\chrome-headless-shell.exe'
if (-not (Test-Path -LiteralPath $BrowserExecutable -PathType Leaf)) { throw "CWAPI_PORTABLE_RUNTIME_BROWSER_MISSING path=$BrowserExecutable" }
$BrowserManifest = [ordered]@{
    schema = 'cwapi.browser-runtime.v1'
    revision = [string]$Browser.browser_revision
    version = [string]$Browser.browser_version
    archive_sha256 = ([string]$Browser.browser_sha256).ToLowerInvariant()
    executable_relative = "chromium_headless_shell-$($Browser.browser_revision)/chrome-headless-shell-win64/chrome-headless-shell.exe"
}
$BrowserManifest | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $BrowserRoot 'runtime.json') -Encoding UTF8
Write-Host "CWAPI_PORTABLE_RUNTIME_BROWSER_PASS revision=$($Browser.browser_revision) version=$($Browser.browser_version)"

$MCPPackage = Join-Path $RuntimeRoot 'mcp\playwright\node_modules\@playwright\mcp\package.json'
$ProcessMCP = Join-Path $RuntimeRoot 'mcp\cwapi\process-server.cjs'
$ProcessMCPInvocation = Join-Path $RuntimeRoot 'mcp\cwapi\process-invocation.cjs'
$ProcessMCPOutput = Join-Path $RuntimeRoot 'mcp\cwapi\process-output.cjs'
foreach ($Required in @($CodexExecutable, $GitExecutable, $NodeExecutable, $MCPPackage, $ProcessMCP, $ProcessMCPInvocation, $ProcessMCPOutput, $BrowserExecutable)) {
    if (-not (Test-Path -LiteralPath $Required -PathType Leaf)) { throw "CWAPI_PORTABLE_RUNTIME_COMPONENT_FINAL_MISSING path=$Required" }
}
$InstalledMCP = Get-Content -Raw -LiteralPath $MCPPackage | ConvertFrom-Json
if ([string]$InstalledMCP.version -ne [string]$Browser.version) {
    throw "CWAPI_PORTABLE_RUNTIME_MCP_VERSION_FINAL_INVALID expected=$($Browser.version) actual=$($InstalledMCP.version)"
}

Write-Host "CWAPI_PORTABLE_RUNTIME_INSTALL_PASS root=$RuntimeRoot codex=$($Codex.version) git=$($Git.version) node=$($Node.version) mcp=$($Browser.version) browser_revision=$($Browser.browser_revision)"
