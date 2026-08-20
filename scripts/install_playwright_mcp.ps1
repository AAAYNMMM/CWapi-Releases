param(
    [string]$ProjectRoot = (Split-Path -Parent $PSScriptRoot)
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$project = (Resolve-Path -LiteralPath $ProjectRoot).Path
$portableLockPath = Join-Path $project "config\portable-runtime.lock.json"
$packageSource = Join-Path $project "config\playwright-mcp-package.json"
$packageLockSource = Join-Path $project "config\playwright-mcp-package-lock.json"
foreach ($required in @($portableLockPath, $packageSource, $packageLockSource)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "Playwright runtime source is missing: $required"
    }
}

$portableLock = Get-Content -Raw -LiteralPath $portableLockPath | ConvertFrom-Json
if ($portableLock.schema -ne 'cwapi.portable-runtime-lock.v2') {
    throw "Portable runtime lock schema is invalid: $($portableLock.schema)"
}
$expectedVersion = ([string]$portableLock.components.playwright_mcp.version).Trim()
$expectedPackage = ([string]$portableLock.components.playwright_mcp.package).Trim()
if ($expectedPackage -ne '@playwright/mcp' -or -not $expectedVersion) {
    throw 'Portable runtime Playwright MCP lock is invalid.'
}

$packageDefinition = Get-Content -Raw -LiteralPath $packageSource | ConvertFrom-Json
$packageLockDefinition = Get-Content -Raw -LiteralPath $packageLockSource | ConvertFrom-Json -AsHashtable
if ([string]$packageDefinition.dependencies.'@playwright/mcp' -ne $expectedVersion) {
    throw "Playwright package.json does not match portable lock. expected=$expectedVersion actual=$($packageDefinition.dependencies.'@playwright/mcp')"
}
$lockedMCPVersion = [string]$packageLockDefinition['packages']['node_modules/@playwright/mcp']['version']
if ($lockedMCPVersion -ne $expectedVersion) {
    throw "Playwright package-lock does not match portable lock. expected=$expectedVersion actual=$lockedMCPVersion"
}

$node = Join-Path $project 'runtime\node\node.exe'
$npm = Join-Path $project 'runtime\node\npm.cmd'
if (-not (Test-Path -LiteralPath $node -PathType Leaf)) {
    throw "Packaged Node runtime is required before Playwright MCP installation: $node"
}
if (-not (Test-Path -LiteralPath $npm -PathType Leaf)) {
    throw "Packaged npm runtime is required before Playwright MCP installation: $npm"
}
$expectedNodeSha = ([string]$portableLock.components.node.executable_sha256).Trim().ToLowerInvariant()
if ($expectedNodeSha -notmatch '^[0-9a-f]{64}$') {
    throw 'Portable runtime Node executable SHA-256 is invalid.'
}
$actualNodeSha = (Get-FileHash -LiteralPath $node -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualNodeSha -ne $expectedNodeSha) {
    throw "Packaged Node SHA-256 mismatch. expected=$expectedNodeSha actual=$actualNodeSha"
}

$installRoot = Join-Path $project 'runtime\mcp\playwright'
New-Item -ItemType Directory -Force -Path $installRoot | Out-Null
Copy-Item -LiteralPath $packageSource -Destination (Join-Path $installRoot 'package.json') -Force
Copy-Item -LiteralPath $packageLockSource -Destination (Join-Path $installRoot 'package-lock.json') -Force

$oldSkipBrowser = $env:PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD
try {
    $env:PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = '1'
    & $npm ci --prefix $installRoot --ignore-scripts --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to install pinned @playwright/mcp@$expectedVersion from package-lock."
    }
} finally {
    if ($null -eq $oldSkipBrowser) {
        Remove-Item Env:PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD -ErrorAction SilentlyContinue
    } else {
        $env:PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = $oldSkipBrowser
    }
}

$mcpPackagePath = Join-Path $installRoot 'node_modules\@playwright\mcp\package.json'
if (-not (Test-Path -LiteralPath $mcpPackagePath -PathType Leaf)) {
    throw "Playwright MCP package.json was not installed: $mcpPackagePath"
}
$mcpPackage = Get-Content -LiteralPath $mcpPackagePath -Raw | ConvertFrom-Json
if ([string]$mcpPackage.version -ne $expectedVersion) {
    throw "Playwright MCP version mismatch. expected=$expectedVersion actual=$($mcpPackage.version)"
}
$cliPath = Join-Path (Split-Path $mcpPackagePath -Parent) 'cli.js'
if (-not (Test-Path -LiteralPath $cliPath -PathType Leaf)) {
    throw "Playwright MCP CLI entry not found: $cliPath"
}

$runtimeManifest = [ordered]@{
    schema = 'cwapi.playwright-mcp-runtime.v1'
    package = '@playwright/mcp'
    version = $expectedVersion
    node_executable = 'runtime/node/node.exe'
    cli_path = 'runtime/mcp/playwright/node_modules/@playwright/mcp/cli.js'
    browser_runtime = 'runtime/browser; pinned separately by config/portable-runtime.lock.json'
    installed_at = [DateTimeOffset]::UtcNow.ToString('o')
}
$runtimeManifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $installRoot 'runtime.json') -Encoding UTF8

Write-Host "Installed pinned Playwright MCP from portable lock: $expectedVersion"
Write-Host "Node: $node"
Write-Host "CLI: $cliPath"
Write-Host 'Browser download was intentionally skipped; CWapi packages the separately pinned browser runtime.'
