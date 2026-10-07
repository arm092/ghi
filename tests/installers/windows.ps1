param(
    [Parameter(Mandatory)][string]$Installer,
    [string]$Version = '1.0.0',
    [string]$MojaveVersion = '0.1.0'
)
$ErrorActionPreference = 'Stop'
$installerPath = (Resolve-Path -LiteralPath $Installer).Path
$repoRoot = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$installDirectory = Join-Path $repoRoot '.work/windows installer test'
$uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\am.ghi.compiler_is1'
if (Test-Path $uninstallKey) { throw 'Run this test in an account without an existing registered Ghi installation.' }
$environmentKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$originalPath = $environmentKey.GetValue('Path', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
$originalKind = if ($null -ne $originalPath) { $environmentKey.GetValueKind('Path') } else { [Microsoft.Win32.RegistryValueKind]::ExpandString }
# Empty PATH entries and a trailing separator must survive install/uninstall.
$probePath = "$originalPath;;"
$environmentKey.SetValue('Path', $probePath, $originalKind)
try {
    foreach ($iteration in 1..2) {
        $run = Start-Process -FilePath $installerPath -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART',"/DIR=`"$installDirectory`"" -WindowStyle Hidden -PassThru -Wait
        if ($run.ExitCode -ne 0) { throw "Install/reinstall failed: $($run.ExitCode)" }
        if (!(Test-Path $uninstallKey)) { throw 'No Add/Remove Programs registration' }
        $userPath = $environmentKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        if (@(($userPath -split ';') | Where-Object { $_ -eq "$installDirectory\bin" }).Count -ne 1) { throw 'Missing or duplicated PATH entry' }
    }
    $actualVersion = & "$installDirectory\bin\ghi.exe" version
    if ($LASTEXITCODE -ne 0 -or $actualVersion -ne "ghi v$Version") { throw 'Installed compiler version failed' }
    $output = & "$installDirectory\bin\ghi.exe" run (Join-Path $repoRoot 'examples/constraints') 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0 -or $output.Trim() -ne '7 Ada Ada') { throw 'Installed compiler example failed' }
    $enumOutput = & "$installDirectory\bin\ghi.exe" run (Join-Path $repoRoot 'examples/enums') 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0 -or ($enumOutput.Trim() -replace "`r", '') -ne "pending 200 true`nnorth") { throw 'Installed compiler enum example failed' }
    $actualMojaveVersion = & "$installDirectory\bin\mojave.exe" --version
    if ($LASTEXITCODE -ne 0 -or $actualMojaveVersion -ne "mojave v$MojaveVersion") { throw 'Independent Mojave version failed' }
    & "$installDirectory\bin\mojave.exe" help | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Installed Mojave failed' }
    $uninstall = Start-Process -FilePath "$installDirectory\unins000.exe" -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART' -WindowStyle Hidden -PassThru -Wait
    if ($uninstall.ExitCode -ne 0) { throw 'Uninstall failed' }
    if ($environmentKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) -cne $probePath) { throw 'PATH was not preserved byte-for-byte' }
    if ((Test-Path "$installDirectory\bin\ghi.exe") -or (Test-Path $uninstallKey)) { throw 'Uninstall left installed files or registration' }
    Write-Output 'Install, reinstall, Ghi/Mojave execution, uninstall and exact PATH preservation passed.'
} finally {
    try {
        if (Test-Path "$installDirectory\unins000.exe") {
            Start-Process -FilePath "$installDirectory\unins000.exe" -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART' -WindowStyle Hidden -Wait
        }
    } finally {
        try {
            if ($null -eq $originalPath) { $environmentKey.DeleteValue('Path', $false) }
            else { $environmentKey.SetValue('Path', $originalPath, $originalKind) }
        } finally { $environmentKey.Dispose() }
    }
}
