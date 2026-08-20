param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedCommit
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $RepoRoot

$ActualCommit = (& git rev-parse HEAD).Trim().ToLowerInvariant()
if ($LASTEXITCODE -ne 0 -or $ActualCommit -ne $ExpectedCommit.ToLowerInvariant()) {
    throw "CWAPI_DOCS_COMMIT_MISMATCH expected=$ExpectedCommit actual=$ActualCommit"
}

$Files = @()
$Files += Get-Item -LiteralPath 'README.md'
if (Test-Path -LiteralPath 'AGENTS.md' -PathType Leaf) {
    $Files += Get-Item -LiteralPath 'AGENTS.md'
}
$Files += @(Get-ChildItem -LiteralPath 'docs' -File -Filter '*.md')

$TargetLines = 250
$HardLines = 350
$TargetSectionLines = 80
$SectionReviewLines = 120
$HardBytes = 20KB
$RetiredTokens = @(
    'MCP_BOUNDARY_CONTRACTS.md',
    'MCP_RELAY_ARCHITECTURE.md',
    'CHANNELS_AND_DELIVERY.md',
    'RUNTIME_DEPENDENCY_AUDIT.md',
    'V1_6_0_STAGE2_S2_3_VALIDATED.md',
    'V1_6_0_STAGE2_S2_4_B_VALIDATED.md',
    'V1_6_0_STAGE2_S2_4_PROGRESS.md',
    'Stage 1 = COMPLETE BASELINE',
    'Stage 2 = ACTIVE',
    'Stage 3 = BLOCKED'
)

foreach ($File in $Files) {
    $Lines = @(Get-Content -LiteralPath $File.FullName)
    $Text = $Lines -join "`n"
    $LineCount = $Lines.Count
    $ByteCount = $File.Length
    $RelativePath = (Resolve-Path -LiteralPath $File.FullName -Relative)

    if ($LineCount -gt $HardLines) {
        throw "CWAPI_DOCS_FILE_TOO_LONG path=$RelativePath lines=$LineCount hard=$HardLines"
    }
    if ($ByteCount -gt $HardBytes) {
        throw "CWAPI_DOCS_FILE_TOO_LARGE path=$RelativePath bytes=$ByteCount hard=$HardBytes"
    }
    if ($LineCount -gt $TargetLines) {
        Write-Host "CWAPI_DOCS_FILE_REVIEW path=$RelativePath lines=$LineCount target=$TargetLines"
    }

    foreach ($Token in $RetiredTokens) {
        if ($Text.IndexOf($Token, [StringComparison]::OrdinalIgnoreCase) -ge 0) {
            throw "CWAPI_DOCS_RETIRED_REFERENCE path=$RelativePath token=$Token"
        }
    }

    $SectionStart = 0
    $SectionTitle = '<preamble>'
    for ($Index = 0; $Index -lt $Lines.Count; $Index++) {
        if ($Lines[$Index] -match '^#{1,2}\s+(.+)$') {
            $SectionLength = $Index - $SectionStart
            if ($SectionLength -gt $SectionReviewLines) {
                Write-Host "CWAPI_DOCS_SECTION_REVIEW path=$RelativePath section=$SectionTitle lines=$SectionLength limit=$SectionReviewLines"
            } elseif ($SectionLength -gt $TargetSectionLines) {
                Write-Host "CWAPI_DOCS_SECTION_TARGET path=$RelativePath section=$SectionTitle lines=$SectionLength target=$TargetSectionLines"
            }
            $SectionStart = $Index
            $SectionTitle = $Matches[1]
        }
    }

    $FinalSectionLength = $Lines.Count - $SectionStart
    if ($FinalSectionLength -gt $SectionReviewLines) {
        Write-Host "CWAPI_DOCS_SECTION_REVIEW path=$RelativePath section=$SectionTitle lines=$FinalSectionLength limit=$SectionReviewLines"
    } elseif ($FinalSectionLength -gt $TargetSectionLines) {
        Write-Host "CWAPI_DOCS_SECTION_TARGET path=$RelativePath section=$SectionTitle lines=$FinalSectionLength target=$TargetSectionLines"
    }
}

Write-Host "CWAPI_DOCUMENTATION_MODULARITY_PASS commit=$ActualCommit files=$($Files.Count) hard_lines=$HardLines hard_bytes=$HardBytes"
