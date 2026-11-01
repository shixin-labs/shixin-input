# Installs real unsigned packages. Run ONLY in an elevated disposable Windows VM.
# GUI/UAC interaction and application typing remain separate manual checks.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('amd64','arm64')][string]$Arch,
    [Parameter(Mandatory)][string]$Version,
    [switch]$DisposableVM
)
$ErrorActionPreference='Stop'
if (-not $IsWindows -or -not $DisposableVM) { throw 'This check requires -DisposableVM on Windows' }
if ((& go env GOHOSTARCH).Trim() -ne $Arch) { throw 'Installer lifecycle checks require matching native Windows' }
$identity=[Security.Principal.WindowsIdentity]::GetCurrent()
if (-not ([Security.Principal.WindowsPrincipal]::new($identity)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Use an elevated disposable VM' }
$root=Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$install=Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'Shixin'
$arp='HKLM:/Software/Microsoft/Windows/CurrentVersion/Uninstall/Shixin'
$class='Software\Classes\CLSID\{AAC1A491-E694-4610-96B5-A476A1441224}'
$runKey='HKCU:/Software/Microsoft/Windows/CurrentVersion/Run'
if ((Test-Path $install) -or (Test-Path $arp) -or (Test-Path "HKLM:/$class") -or (Test-Path "HKCU:/$class") -or
    ((Get-ItemProperty $runKey -Name Shixin -ErrorAction SilentlyContinue).Shixin)) { throw 'Existing Shixin state; refusing to change this machine' }
$work=Join-Path $root ("build/install-test-$Arch-"+[Guid]::NewGuid().ToString('N'))
$payload=Join-Path $root "build/package-windows-$Arch-unsigned-preview/payload"
$msi=Join-Path $root "build/package-windows-$Arch-unsigned-preview/Shixin.msi"
$setup=Join-Path $root "build/dist/preview/ShixinSetup-$Arch-unsigned-preview.exe"
New-Item -ItemType Directory -Force $work | Out-Null
$savedApp=$env:APPDATA; $savedLocal=$env:LOCALAPPDATA
$env:APPDATA=Join-Path $work 'roaming'; $env:LOCALAPPDATA=Join-Path $work 'local'
New-Item -ItemType Directory -Force "$env:APPDATA/Shixin/Rime",$env:LOCALAPPDATA | Out-Null
$settings=Join-Path $env:APPDATA 'Shixin/settings.json'
$dictionary=Join-Path $env:APPDATA 'Shixin/Rime/preserved-fixture'
Set-Content $settings '{"version":1,"font_size":19,"auto_space":true}' -Encoding utf8NoBOM
Set-Content $dictionary 'synthetic dictionary fixture' -Encoding utf8NoBOM
$settingsHash=(Get-FileHash $settings).Hash; $dictionaryHash=(Get-FileHash $dictionary).Hash
$holder=$null; $profileEnabled=$false
function Check([string]$step) { if ($LASTEXITCODE -ne 0) { throw "$step failed (exit $LASTEXITCODE)" } }
function Front([string[]]$Arguments) {
    $p=Start-Process "$install/Shixin.exe" -ArgumentList $Arguments -Wait -PassThru -RedirectStandardError "$work/front.err"
    if ($p.ExitCode -ne 0) { throw "Front $Arguments failed: $(Get-Content "$work/front.err" -Raw)" }
}
function MSI([string]$Path,[string]$Log,[int[]]$Expected=@(0,3010),[switch]$Remove) {
    $action=if ($Remove) {'/x'} else {'/i'}
    # Windows Installer rejects forward-slash package paths even though most
    # other Win32 APIs accept them. Keep literal Windows paths in its argv.
    $Path=$Path.Replace('/','\'); $Log=$Log.Replace('/','\')
    $p=Start-Process "$env:SystemRoot/System32/msiexec.exe" -ArgumentList "$action `"$Path`" /qn /norestart REBOOT=ReallySuppress /l*v `"$Log`"" -Wait -PassThru
    if ($p.ExitCode -notin $Expected) { throw "MSI returned $($p.ExitCode); log: $Log" }
    Write-Host "MSI $action exit $($p.ExitCode)"
}
function Verify([string]$ExpectedPayload) {
    foreach ($file in (Get-ChildItem $ExpectedPayload -Recurse -File)) {
        $relative=[IO.Path]::GetRelativePath($ExpectedPayload,$file.FullName)
        $actual=Join-Path $install $relative
        if (-not (Test-Path $actual) -or (Get-FileHash $actual).Hash -ne (Get-FileHash $file.FullName).Hash) { throw "Installed bytes differ: $relative" }
    }
    if ((Get-FileHash $settings).Hash -ne $settingsHash -or (Get-FileHash $dictionary).Hash -ne $dictionaryHash) { throw 'Installer changed user data' }
    foreach ($file in (Get-ChildItem $install -File -Filter '*.dll' | Where-Object Name -Match '^(msvcp|vcruntime|concrt|vccorlib)')) {
        $signature=Get-AuthenticodeSignature $file.FullName
        if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'O=Microsoft Corporation(?:,|$)') { throw "Invalid Microsoft signature: $($file.Name)" }
    }
}
Push-Location $root
try {
    $p=Start-Process $setup -ArgumentList '-verify' -Wait -PassThru -RedirectStandardOutput "$work/verify.out" -RedirectStandardError "$work/verify.err"
    if ($p.ExitCode -ne 0 -or (Get-Content "$work/verify.out" -Raw) -notmatch ([regex]::Escape("$Version $Arch unsigned-preview (integrity only)"))) { throw 'Bootstrapper verification failed' }
    MSI $msi "$work/install.log"
    Verify $payload
    $oldCode=(Get-ItemProperty $arp).ProductCode
    $profileEnabled=$true; Front @('-user-register')
    Front @('-added')
    # Exercise the installed builder, DLL, engine, prebuilt dictionaries and
    # IPC with public codes. Capture owns a warm-only scratch user directory.
    & "$install/shixin-builder.exe" -lib "$install/rime.dll" -data "$install/data" -prebuilt "$install/prebuilt" -output "$work/generation"
    Check 'build isolated installed generation'
    # Windows IPC trusts sibling executables. Place the disposable client
    # beside the installed engine; never weaken that identity check for tests.
    $capture=Join-Path $install 'ShixinCaptureTest.exe'
    try {
        & go build -o $capture ./tools/menu-capture; Check 'build capture fixture'
        Set-Content "$work/codes.jsonl" '{"code":"nihao"}' -Encoding utf8NoBOM
        & $capture -engine "$install/shixin-engine.exe" -generation "$work/generation" -data "$install/data" -prebuilt "$install/prebuilt" -lib "$install/rime.dll" -profile ('0'*64) -input "$work/codes.jsonl" -output "$work/menus.jsonl" -limit 5 -timeout 2m
        Check 'installed engine IPC and dictionary replay'
    } finally { Remove-Item $capture -ErrorAction SilentlyContinue }
    if (-not ((Get-Content "$work/menus.jsonl" | ForEach-Object {($_ | ConvertFrom-Json).candidates.text}) -contains '你好')) { throw 'Installed dictionary did not return the expected public candidate' }
    # The synthetic next package is ONLY a lifecycle fixture, never uploaded.
    $v=[version]$Version
    if ($v.Build -ge 65535) { throw 'Choose a version with room for an upgrade fixture' }
    $next="$($v.Major).$($v.Minor).$($v.Build+1)"
    $upPayload=Join-Path $work 'upgrade-payload'
    Copy-Item $payload $upPayload -Recurse
    Add-Content "$upPayload/licenses/Go.txt" "`nSynthetic upgrade fixture" -Encoding utf8
    $skip=if ($Arch -eq 'arm64') {'shixin_tsf.dll'} else {''}
    & go run ./tools/windows-package -stamp-dir $upPayload -version $next -skip $skip; Check 'stamp upgrade fixture'
    $upgrade=Join-Path $work 'upgrade.msi'
    & (Get-Process -Id $PID).Path -NoProfile -ExecutionPolicy Bypass -File "$PSScriptRoot/msi.ps1" -Payload $upPayload -Output $upgrade -Version $next -Arch $Arch
    Check 'author upgrade fixture'
    # Any certificate table protects an existing signed installation, even
    # when its signature is malformed or untrusted. No test trust root is added.
    $front=Join-Path $install 'Shixin.exe'
    $original=[IO.File]::ReadAllBytes($front)
    try {
        $signed=$original.Clone(); $pe=[BitConverter]::ToInt32($signed,0x3c)
        [Array]::Copy([BitConverter]::GetBytes([uint32]$signed.Length),0,$signed,$pe+168,4)
        [Array]::Copy([BitConverter]::GetBytes([uint32]8),0,$signed,$pe+172,4)
        [IO.File]::WriteAllBytes($front,$signed)
        MSI $upgrade "$work/signed-target.log" @(1603)
        if ((Get-Content "$work/signed-target.log" -Raw) -notmatch 'CustomAction CheckStopped returned actual error code') { throw 'Preview guard was not reached' }
        if ((Get-ItemProperty $arp).ProductCode -ne $oldCode) { throw 'Rejected upgrade changed product identity' }
    } finally { [IO.File]::WriteAllBytes($front,$original) }
    Verify $payload
    $key=[Microsoft.Win32.Registry]::LocalMachine.OpenSubKey("$class\InprocServer32",$true)
    if (-not $key) { throw 'Machine COM registration missing' }
    $originalClass=$key.GetValue('')
    try {
        $key.SetValue('',"$work/conflict.dll",[Microsoft.Win32.RegistryValueKind]::String)
        MSI $upgrade "$work/rollback.log" @(1603)
        if ((Get-Content "$work/rollback.log" -Raw) -notmatch 'CustomActionSchedule\(Action=RegisterTIP' -or (Get-ItemProperty $arp).ProductCode -ne $oldCode) { throw 'Registration rollback was not exercised' }
        Verify $payload
    } finally { $key.SetValue('',$originalClass,[Microsoft.Win32.RegistryValueKind]::String); $key.Close() }
    $ready=Join-Path $work 'holder.ready'; $stop=Join-Path $work 'holder.stop'
    $holder=Start-Process "$root/build/tsf-$Arch/Release/shixin_dll_hold.exe" -ArgumentList "`"$install/shixin_tsf.dll`" `"$ready`" `"$stop`"" -PassThru
    for ($i=0;$i -lt 100 -and -not (Test-Path $ready);$i++) { Start-Sleep -Milliseconds 100 }
    if (-not (Test-Path $ready) -or $holder.HasExited) { throw 'TSF holder failed to start' }
    MSI $upgrade "$work/upgrade.log"
    if ($holder.HasExited) { throw 'Installer terminated the application holding TSF' }
    Verify $upPayload
    Front @('-added')
    Set-Content $stop 'release'
    if (-not $holder.WaitForExit(10000) -or $holder.ExitCode -ne 0) { throw 'TSF holder did not exit cleanly' }
    $holder=$null
    Front @('-user-unregister'); $profileEnabled=$false
    $code=(Get-ItemProperty $arp).ProductCode
    MSI $code "$work/uninstall.log" -Remove
    if ((Test-Path $arp) -or (Test-Path "HKLM:/$class") -or (Test-Path "$install/Shixin.exe")) { throw 'Uninstall left the product or COM registration' }
    if ((Get-FileHash $settings).Hash -ne $settingsHash -or (Get-FileHash $dictionary).Hash -ne $dictionaryHash) { throw 'Uninstall changed user data' }
    Write-Host "Native $Arch installation, registration, installed engine replay, signed-target protection, rollback, held-DLL upgrade and uninstall passed."
} finally {
    try {
        if ($holder) {
            Set-Content (Join-Path $work 'holder.stop') 'release'
            if (-not $holder.WaitForExit(10000)) { throw 'Test holder did not release its DLL' }
        }
        if ($profileEnabled -and (Test-Path "$install/Shixin.exe")) { Front @('-user-unregister') }
        if (Test-Path $arp) { MSI ((Get-ItemProperty $arp).ProductCode) "$work/cleanup.log" -Remove }
        $startup=(Get-ItemProperty $runKey -Name Shixin -ErrorAction SilentlyContinue).Shixin
        if ($startup -eq "`"$install\Shixin.exe`" -startup") { Remove-ItemProperty $runKey -Name Shixin }
    } finally {
        $env:APPDATA=$savedApp; $env:LOCALAPPDATA=$savedLocal
        Pop-Location
    }
}
