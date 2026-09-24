param([Parameter(Mandatory)][string]$Installer)
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
    $version = & "$installDirectory\bin\ghi.exe" version
    if ($LASTEXITCODE -ne 0 -or $version -ne 'ghi v0.2.1') { throw 'Installed compiler version failed' }
    $output = & "$installDirectory\bin\ghi.exe" run (Join-Path $repoRoot 'examples/constraints') 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0 -or $output.Trim() -ne '7 Ada Ada') { throw 'Installed compiler example failed' }
    $mojaveVersion = & "$installDirectory\bin\mojave.exe" --version
    if ($LASTEXITCODE -ne 0 -or $mojaveVersion -ne 'mojave v0.1.0') { throw 'Independent Mojave version failed' }
    & "$installDirectory\bin\mojave.exe" help | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Installed Mojave failed' }
    $uninstall = Start-Process -FilePath "$installDirectory\unins000.exe" -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART' -WindowStyle Hidden -PassThru -Wait
    if ($uninstall.ExitCode -ne 0) { throw 'Uninstall failed' }
    if ($environmentKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) -cne $probePath) { throw 'PATH was not preserved byte-for-byte' }
    if ((Test-Path "$installDirectory\bin\ghi.exe") -or (Test-Path $uninstallKey)) { throw 'Uninstall left installed files or registration' }
    Write-Output 'Install, reinstall, Ghi/Mojave execution, uninstall and exact PATH preservation passed.'
} finally {
    if (Test-Path "$installDirectory\unins000.exe") {
        Start-Process -FilePath "$installDirectory\unins000.exe" -ArgumentList '/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART' -WindowStyle Hidden -Wait
    }
    if ($null -eq $originalPath) { $environmentKey.DeleteValue('Path', $false) }
    else { $environmentKey.SetValue('Path', $originalPath, $originalKind) }
    $environmentKey.Dispose()
}
