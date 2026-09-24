param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Ghi\bin'),
    [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
$binary = Join-Path $PSScriptRoot 'ghi.exe'
$executables = @('ghi')
# Legacy compiler-only bundles remain installable; partial Mojave bundles fail.
if ((Test-Path -LiteralPath (Join-Path $PSScriptRoot 'mojave.exe')) -or (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'mojave.sha256'))) {
    $executables += 'mojave'
}
foreach ($name in $executables) {
    $source = Join-Path $PSScriptRoot "$name.exe"
    $checksumFile = Join-Path $PSScriptRoot "$name.sha256"
    if (!(Test-Path -LiteralPath $source -PathType Leaf) -or !(Test-Path -LiteralPath $checksumFile -PathType Leaf)) {
        throw 'Extract the complete Ghi release archive before running this installer.'
    }
    $expected = (Get-Content -LiteralPath $checksumFile -Raw).Trim().Split(' ')[0]
    $actual = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash
    if ($expected -notmatch '^[a-fA-F0-9]{64}$' -or $actual -ne $expected) { throw "$name executable checksum mismatch." }
}
$targetDirectory = [IO.Path]::GetFullPath($InstallDir)
& $binary setup
if ($LASTEXITCODE -ne 0) { throw 'Go setup failed. The installed Ghi executable has not been replaced. Retry this installer when setup is available.' }
New-Item -ItemType Directory -Path $targetDirectory -Force | Out-Null
$target = Join-Path $targetDirectory 'ghi.exe'
foreach ($name in $executables) {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot "$name.exe") -Destination (Join-Path $targetDirectory "$name.exe") -Force
}
if (!$NoPath) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @($userPath -split ';' | Where-Object { $_ })
    if ($entries -notcontains $targetDirectory) {
        [Environment]::SetEnvironmentVariable('Path', (($entries + $targetDirectory) -join ';'), 'User')
    }
    if (($env:Path -split ';') -notcontains $targetDirectory) { $env:Path += ";$targetDirectory" }
}
Write-Output "Ghi installed at $target"
Write-Output 'Open a new terminal, then run ghi version, ghi run <project-directory>, or mojave help.'
