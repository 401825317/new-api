param(
    [Parameter(Mandatory=$true)][string]$UpstreamRef,
    [Parameter(Mandatory=$true)][string]$TargetDirectory
)
$ErrorActionPreference = 'Stop'
$sourceRoot = Split-Path $PSScriptRoot -Parent
if (Test-Path -LiteralPath $TargetDirectory) { throw 'TargetDirectory must not exist.' }
function Invoke-GitChecked {
    param([string[]]$GitArgs)
    & git @GitArgs
    if ($LASTEXITCODE -ne 0) { throw "git failed: $($GitArgs[0])" }
}
$dirty = & git -C $sourceRoot status --porcelain
if ($LASTEXITCODE -ne 0 -or $dirty) { throw 'Commit the recovery branch before verifying an upgrade.' }
$patchFile = New-TemporaryFile
try {
    Invoke-GitChecked @('-C', $sourceRoot, 'diff', '--binary', "--output=$($patchFile.FullName)", 'v0.13.1-patch.1', 'HEAD')
    Invoke-GitChecked @('-C', $sourceRoot, 'worktree', 'add', '--detach', $TargetDirectory, $UpstreamRef)
    Invoke-GitChecked @('-C', $TargetDirectory, 'apply', '--check', $patchFile.FullName)
    Invoke-GitChecked @('-C', $TargetDirectory, 'apply', $patchFile.FullName)
    Push-Location $TargetDirectory
    try {
        & go test ./common ./model ./middleware ./controller ./relay/channel/openai ./service ./logger -run 'TestResponsesRecovery|TestReleaseDrain' -count=3 -timeout 120s
        if ($LASTEXITCODE -ne 0) { throw 'Recovery regression tests failed; do not deploy.' }
        & go test ./model ./middleware ./relay/channel -count=1 -timeout 120s
        if ($LASTEXITCODE -ne 0) { throw 'Routing regression tests failed; do not deploy.' }
        & git diff --check
        if ($LASTEXITCODE -ne 0) { throw 'Patch whitespace check failed.' }
    } finally { Pop-Location }
    Write-Output "Upgrade verification passed: $UpstreamRef at $TargetDirectory"
} finally {
    Remove-Item -LiteralPath $patchFile.FullName
}
