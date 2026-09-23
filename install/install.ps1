param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Ghi\bin'),
    [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
$binary = Join-Path $PSScriptRoot 'ghi.exe'
$checksumFile = Join-Path $PSScriptRoot 'ghi.sha256'
if (!(Test-Path -LiteralPath $binary) -or !(Test-Path -LiteralPath $checksumFile)) {
    throw 'Extract the complete Ghi release archive before running this installer.'
}
$expected = (Get-Content -LiteralPath $checksumFile -Raw).Trim().Split(' ')[0]
$actual = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash
if ($expected -notmatch '^[a-fA-F0-9]{64}$' -or $actual -ne $expected) {
    throw 'Ghi executable checksum mismatch.'
}
$targetDirectory = [IO.Path]::GetFullPath($InstallDir)
New-Item -ItemType Directory -Path $targetDirectory -Force | Out-Null
$target = Join-Path $targetDirectory 'ghi.exe'
Copy-Item -LiteralPath $binary -Destination $target -Force
& $target setup
if ($LASTEXITCODE -ne 0) { throw 'Ghi was copied, but Go setup failed. Run ghi setup to retry.' }
if (!$NoPath) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @($userPath -split ';' | Where-Object { $_ })
    if ($entries -notcontains $targetDirectory) {
        [Environment]::SetEnvironmentVariable('Path', (($entries + $targetDirectory) -join ';'), 'User')
    }
    if (($env:Path -split ';') -notcontains $targetDirectory) { $env:Path += ";$targetDirectory" }
}
Write-Output "Ghi installed at $target"
Write-Output 'Open a new terminal, then run ghi version or ghi run <project-directory>.'
