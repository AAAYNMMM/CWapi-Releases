param(
    [string]$XwinRoot = '',
    [string]$LlvmRoot = '',
    [string]$CargoTargetRoot = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$TargetTriple = 'x86_64-pc-windows-msvc'
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (-not $XwinRoot) { $XwinRoot = Join-Path $RepoRoot 'cache\xwin-v0.10.0\splat' }
if (-not $LlvmRoot) { $LlvmRoot = Join-Path $RepoRoot 'cache\llvm-mingw-20260616-ucrt-x86_64\toolchain' }
if (-not $CargoTargetRoot) { $CargoTargetRoot = Join-Path $RepoRoot 'cache\codex-target' }
$XwinRoot = [System.IO.Path]::GetFullPath($XwinRoot)
$LlvmRoot = [System.IO.Path]::GetFullPath($LlvmRoot)
$CargoTargetRoot = [System.IO.Path]::GetFullPath($CargoTargetRoot)

function Resolve-CodexBuildTool {
    param([string]$Name, [string[]]$Candidates)
    $Command = Get-Command $Name -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -ne $Command -and $Command.Source) { return $Command.Source }
    foreach ($Candidate in $Candidates) {
        if ($Candidate -and (Test-Path -LiteralPath $Candidate -PathType Leaf)) {
            return (Resolve-Path -LiteralPath $Candidate).Path
        }
    }
    throw "CWAPI_CODEX_BUILD_TOOL_MISSING name=$Name"
}

function Resolve-CodexBuildSingleFile {
    param([string]$Label, [string]$Path, [switch]$Recurse, [string]$Filter = '')
    if ($Recurse) {
        $Matches = @(Get-ChildItem -LiteralPath $Path -Filter $Filter -File -Recurse -ErrorAction SilentlyContinue)
    } else {
        $Matches = @(Get-ChildItem -Path $Path -File -ErrorAction SilentlyContinue)
    }
    if ($Matches.Count -ne 1) {
        throw "CWAPI_CODEX_BUILD_TOOLCHAIN_INVALID label=$Label count=$($Matches.Count)"
    }
    return $Matches[0].FullName
}

function Set-CodexBuildProcessEnv {
    param([string]$Name, [string]$Value)
    [System.Environment]::SetEnvironmentVariable($Name, $Value, [System.EnvironmentVariableTarget]::Process)
}

function Configure-CodexManagedMSVC {
    param([string]$RustcExe)
    if ([System.Environment]::OSVersion.Platform -ne [System.PlatformID]::Win32NT) { return }

    $Msvcrt = Resolve-CodexBuildSingleFile -Label 'msvcrt' -Path (Join-Path $XwinRoot 'VC\Tools\MSVC\*\lib\x64\msvcrt.lib')
    $Ucrt = Resolve-CodexBuildSingleFile -Label 'ucrt' -Path (Join-Path $XwinRoot 'Windows Kits\10\Lib\*\ucrt\x64\ucrt.lib')
    $Kernel32 = Resolve-CodexBuildSingleFile -Label 'kernel32' -Path (Join-Path $XwinRoot 'Windows Kits\10\Lib\*\um\x64\kernel32.lib')
    $ClangCL = Resolve-CodexBuildSingleFile -Label 'clang-cl' -Path $LlvmRoot -Recurse -Filter 'clang-cl.exe'
    $LlvmLib = Resolve-CodexBuildSingleFile -Label 'llvm-lib' -Path $LlvmRoot -Recurse -Filter 'llvm-lib.exe'

    $RustVerbose = @(& $RustcExe -vV)
    $HostLine = $RustVerbose | Where-Object { $_ -like 'host: *' } | Select-Object -First 1
    if ($LASTEXITCODE -ne 0 -or $null -eq $HostLine -or $HostLine.Substring(6).Trim() -ne $TargetTriple) {
        throw 'CWAPI_CODEX_BUILD_RUST_HOST_INVALID'
    }
    $RustSysroot = (& $RustcExe --print sysroot).Trim()
    $RustLld = Join-Path $RustSysroot "lib\rustlib\$TargetTriple\bin\rust-lld.exe"
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $RustLld -PathType Leaf)) {
        throw 'CWAPI_CODEX_BUILD_RUST_LLD_MISSING'
    }

    $MsvcrtFile = Get-Item -LiteralPath $Msvcrt
    $UcrtFile = Get-Item -LiteralPath $Ucrt
    $Kernel32File = Get-Item -LiteralPath $Kernel32
    $CrtVersionRoot = $MsvcrtFile.Directory.Parent.Parent.FullName
    $SdkVersion = $UcrtFile.Directory.Parent.Parent.Name
    $IncludeVersionRoot = Join-Path $XwinRoot "Windows Kits\10\Include\$SdkVersion"

    $Libraries = @($MsvcrtFile.Directory.FullName, $UcrtFile.Directory.FullName, $Kernel32File.Directory.FullName)
    if ($env:LIB) { $Libraries += $env:LIB }
    $env:LIB = $Libraries -join [System.IO.Path]::PathSeparator

    $Includes = @(
        (Join-Path $CrtVersionRoot 'include'),
        (Join-Path $IncludeVersionRoot 'ucrt'),
        (Join-Path $IncludeVersionRoot 'um'),
        (Join-Path $IncludeVersionRoot 'shared'),
        (Join-Path $IncludeVersionRoot 'winrt'),
        (Join-Path $IncludeVersionRoot 'cppwinrt')
    ) | Where-Object { Test-Path -LiteralPath $_ -PathType Container }
    if ($env:INCLUDE) { $Includes += $env:INCLUDE }
    $env:INCLUDE = $Includes -join [System.IO.Path]::PathSeparator

    Set-CodexBuildProcessEnv -Name ('CARGO_TARGET_' + $TargetTriple.ToUpperInvariant().Replace('-', '_') + '_LINKER') -Value $RustLld
    $env:PATH = (Split-Path -Parent $ClangCL) + [System.IO.Path]::PathSeparator + $env:PATH
    foreach ($Key in @('CC','CXX',"CC_$TargetTriple","CXX_$TargetTriple",('CC_' + $TargetTriple.Replace('-','_')),('CXX_' + $TargetTriple.Replace('-','_')))) {
        Set-CodexBuildProcessEnv -Name $Key -Value $ClangCL
    }
    foreach ($Key in @('AR',"AR_$TargetTriple",('AR_' + $TargetTriple.Replace('-','_')))) {
        Set-CodexBuildProcessEnv -Name $Key -Value $LlvmLib
    }
    $Flags = "--target=$TargetTriple /winsysroot $XwinRoot"
    foreach ($Key in @('CFLAGS','CXXFLAGS',"CFLAGS_$TargetTriple","CXXFLAGS_$TargetTriple",('CFLAGS_' + $TargetTriple.Replace('-','_')),('CXXFLAGS_' + $TargetTriple.Replace('-','_')))) {
        Set-CodexBuildProcessEnv -Name $Key -Value $Flags
    }
}

$UserCargoBin = if ($env:USERPROFILE) { Join-Path $env:USERPROFILE '.cargo\bin' } else { '' }
$CargoExe = Resolve-CodexBuildTool -Name 'cargo' -Candidates @((Join-Path $UserCargoBin 'cargo.exe'), 'C:\Rust\.cargo\bin\cargo.exe')
$RustcExe = Resolve-CodexBuildTool -Name 'rustc' -Candidates @((Join-Path $UserCargoBin 'rustc.exe'), 'C:\Rust\.cargo\bin\rustc.exe')
$env:PATH = (Split-Path -Parent $CargoExe) + [System.IO.Path]::PathSeparator + $env:PATH
Configure-CodexManagedMSVC -RustcExe $RustcExe
New-Item -ItemType Directory -Force -Path $CargoTargetRoot | Out-Null
$env:CARGO_TARGET_DIR = (Resolve-Path -LiteralPath $CargoTargetRoot).Path
$env:CWAPI_CODEX_BUILD_CARGO = $CargoExe
Write-Host "CWAPI_CODEX_BUILD_ENV_READY cargo=$CargoExe target=$env:CARGO_TARGET_DIR"
