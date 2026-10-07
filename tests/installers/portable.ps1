param(
    [Parameter(Mandatory)][string]$Bundle,
    [Parameter(Mandatory)][string]$WorkDir,
    [string]$Version = '1.0.0',
    [string]$MojaveVersion = '0.1.0'
)
$ErrorActionPreference = 'Stop'
$bundlePath = (Resolve-Path -LiteralPath $Bundle).Path
$work = [IO.Path]::GetFullPath($WorkDir)
if (Test-Path -LiteralPath $work) { throw 'WorkDir must not exist; use a fresh directory.' }
New-Item -ItemType Directory -Path $work | Out-Null
$keys = @('PATH', 'LOCALAPPDATA', 'APPDATA', 'GHI_GO', 'GOROOT', 'GOTOOLCHAIN', 'GOWORK', 'GOPATH', 'GOCACHE', 'GOENV')
$saved = @{}
foreach ($key in $keys) { $saved[$key] = [Environment]::GetEnvironmentVariable($key, 'Process') }
function Invoke-Checked([string]$File, [string[]]$Arguments) {
    $output = & $File @Arguments 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { throw "$File failed ($LASTEXITCODE): $output" }
    return $output.Trim()
}
try {
    $env:PATH = "$env:SystemRoot\System32"
    $env:LOCALAPPDATA = Join-Path $work 'local'
    $env:APPDATA = Join-Path $work 'roaming'
    $env:GOPATH = Join-Path $work 'gopath'
    $env:GOCACHE = Join-Path $work 'go-build'
    $env:GOENV = 'off'
    foreach ($key in @('GHI_GO', 'GOROOT', 'GOTOOLCHAIN', 'GOWORK')) { [Environment]::SetEnvironmentVariable($key, $null, 'Process') }
    if (Get-Command go -ErrorAction SilentlyContinue) { throw 'Go is unexpectedly on PATH.' }
    $copy = Join-Path $work 'bundle'
    Copy-Item -LiteralPath $bundlePath -Destination $copy -Recurse
    $installed = Join-Path $work 'installed bin'
    & (Join-Path $copy 'install.ps1') -InstallDir $installed -NoPath
    $ghi = Join-Path $installed 'ghi.exe'
    $mojave = Join-Path $installed 'mojave.exe'
    if ((Invoke-Checked $ghi @('-v')) -ne "ghi v$Version") { throw 'Wrong Ghi version.' }
    if ((Invoke-Checked $mojave @('-v')) -ne "mojave v$MojaveVersion") { throw 'Wrong Mojave version.' }
    $setup = Invoke-Checked $ghi @('setup')
    if (!$setup.Contains((Join-Path $env:LOCALAPPDATA 'ghi\toolchains'))) { throw "Toolchain was not provisioned in the fresh cache: $setup" }
    $before = (Get-FileHash -LiteralPath $ghi).Hash
    & (Join-Path $copy 'install.ps1') -InstallDir $installed -NoPath
    if ((Get-FileHash -LiteralPath $ghi).Hash -ne $before) { throw 'Reinstall changed the compiler.' }
    $guard = Join-Path $work 'protected destination'
    New-Item -ItemType Directory -Path $guard | Out-Null
    $sentinels = @{}
    foreach ($name in @('ghi', 'mojave')) {
        $target = Join-Path $guard "$name.exe"
        [IO.File]::WriteAllText($target, "existing $name installation sentinel")
        $sentinels[$name] = (Get-FileHash -LiteralPath $target).Hash
    }
    foreach ($name in @('ghi', 'mojave')) {
        Set-Content -LiteralPath (Join-Path $copy "$name.sha256") -Value ('0' * 64)
        $rejected = $false
        try { & (Join-Path $copy 'install.ps1') -InstallDir $guard -NoPath } catch { $rejected = $_.Exception.Message.Contains("$name executable checksum mismatch") }
        if (!$rejected) { throw "Corrupt $name bundle was not rejected." }
        foreach ($targetName in @('ghi', 'mojave')) {
            if ((Get-FileHash -LiteralPath (Join-Path $guard "$targetName.exe")).Hash -ne $sentinels[$targetName]) { throw 'Corrupt bundle modified an existing installation.' }
        }
        Copy-Item -LiteralPath (Join-Path $bundlePath "$name.sha256") -Destination (Join-Path $copy "$name.sha256") -Force
    }
    $project = Join-Path $work 'new project'
    Invoke-Checked $ghi @('init', $project) | Write-Output
    if (!(Test-Path -LiteralPath (Join-Path $project 'mojave.json')) -or !(Test-Path -LiteralPath (Join-Path $project 'tests') -PathType Container)) { throw 'Project skeleton is incomplete.' }
    Push-Location $project
    try { Invoke-Checked $mojave @('install') | Write-Output } finally { Pop-Location }
    Invoke-Checked $ghi @('check', $project) | Write-Output
    if ((Invoke-Checked $ghi @('run', $project)) -ne 'Hello from Ghi!') { throw 'Project run failed.' }
    $application = Join-Path $work 'hello.exe'
    Invoke-Checked $ghi @('build', '-o', $application, $project) | Write-Output
    if ((Invoke-Checked $application @()) -ne 'Hello from Ghi!') { throw 'Standalone application failed.' }
    Write-Output "PASS: Windows portable $Version, no system Go, fresh caches, reinstall, checksum rejection, init, Mojave install, check, run, build and standalone execution."
} finally {
    foreach ($key in $keys) { [Environment]::SetEnvironmentVariable($key, $saved[$key], 'Process') }
}
