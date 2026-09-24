param(
    [string]$Version = '0.2.1',
    [Parameter(Mandatory)][string]$BundleRoot,
    [string]$ISCC = 'ISCC.exe'
)
$ErrorActionPreference = 'Stop'
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw 'Use a numeric release version, e.g. 0.2.1.' }
$repoRoot = Split-Path $PSScriptRoot -Parent
$bundlePath = (Resolve-Path -LiteralPath $BundleRoot).Path
foreach ($arch in @('amd64', 'arm64')) {
    foreach ($name in @('ghi', 'mojave')) {
        $directory = Join-Path $bundlePath "windows-$arch"
        $expected = ((Get-Content -LiteralPath (Join-Path $directory "$name.sha256") -Raw).Trim() -split '\s+')[0]
        $actual = (Get-FileHash -LiteralPath (Join-Path $directory "$name.exe") -Algorithm SHA256).Hash
        if ($expected -notmatch '^[a-fA-F0-9]{64}$' -or $expected -ne $actual) { throw "$arch $name checksum mismatch" }
    }
}
$output = Join-Path $repoRoot 'dist'
New-Item -ItemType Directory -Force -Path $output | Out-Null
& $ISCC "/DReleaseVersion=$Version" "/DBundleRoot=$bundlePath" "/DRepoRoot=$repoRoot" "/DOutputRoot=$output" (Join-Path $repoRoot 'install/windows/ghi.iss')
if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed.' }
$installer = Join-Path $output "ghi_v${Version}_windows_setup.exe"
$digest = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash.ToLowerInvariant()
[IO.File]::WriteAllText("$installer.sha256", "$digest  $([IO.Path]::GetFileName($installer))`n", [Text.UTF8Encoding]::new($false))
Write-Output $installer
