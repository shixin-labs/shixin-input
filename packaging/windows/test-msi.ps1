# Non-installing package-authoring checks. Runs on native Windows in CI.
[CmdletBinding()]
param([ValidateSet('amd64','arm64')][string]$Arch)
$ErrorActionPreference='Stop'
$root=Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$temp=Join-Path $root ('build/msi-test-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory "$temp/payload/data" -Force | Out-Null
try {
    foreach ($name in @('Shixin.exe','ShixinSetup.exe','shixin-settings.exe','shixin_tsf.dll','rime.dll','data/test.yaml')) {
        Set-Content "$temp/payload/$name" 'fixture' -Encoding ascii
    }
    # A real PE resource proves File.Version/Language are authored, rather
    # than treating native libraries as unversioned payload data.
    New-Item -ItemType Directory "$temp/versioned" | Out-Null
    Push-Location $root
    try {
        & go build -ldflags '-s -w' -o "$temp/versioned/rime.dll" ./cmd/shixin-setup
        if ($LASTEXITCODE -ne 0) {throw 'build version fixture failed'}
        & go run ./tools/windows-package -stamp-dir "$temp/versioned" -version 1.2.3
        if ($LASTEXITCODE -ne 0) {throw 'stamp version fixture failed'}
    } finally {Pop-Location}
    Copy-Item "$temp/versioned/rime.dll" "$temp/payload/rime.dll" -Force
    & (Get-Process -Id $PID).Path -NoProfile -ExecutionPolicy Bypass -File "$PSScriptRoot/msi.ps1" -Payload "$temp/payload" -Output "$temp/test.msi" -Version '1.2.3' -Arch $Arch
    if ($LASTEXITCODE -ne 0) {throw 'MSI authoring failed'}
    $i=New-Object -ComObject WindowsInstaller.Installer
    $db=$i.OpenDatabase("$temp/test.msi",0)
    function Value([string]$sql) { $v=$db.OpenView($sql); $null=$v.Execute(); $r=$v.Fetch(); $out=if ($r) {$r.StringData(1)} else {''}; $null=$v.Close(); if ($r) {[Runtime.InteropServices.Marshal]::FinalReleaseComObject($r) | Out-Null}; [Runtime.InteropServices.Marshal]::FinalReleaseComObject($v) | Out-Null; return $out }
    if ((Value 'SELECT `Value` FROM `Property` WHERE `Property`=''ProductVersion''') -ne '1.2.3') {throw ('Wrong package version: '+(Value 'SELECT `Value` FROM `Property` WHERE `Property`=''ProductVersion'''))}
    if ((Value 'SELECT `Value` FROM `Property` WHERE `Property`=''ALLUSERS''') -ne '1') {throw 'Not a machine package'}
    if ((Value 'SELECT `Condition` FROM `InstallExecuteSequence` WHERE `Action`=''InstallExecuteAgain''') -ne 'NOT (REMOVE="ALL")') {throw 'Second script flush must be skipped during uninstall'}
    if ((Value 'SELECT `Sequence` FROM `InstallExecuteSequence` WHERE `Action`=''InstallExecuteAgain''') -ne '6500') {throw 'New component ownership must execute before old product removal'}
    if ((Value 'SELECT `Sequence` FROM `InstallExecuteSequence` WHERE `Action`=''InstallExecute''') -ne '1506') {throw 'Process guard must execute before old product removal'}
    if ((Value 'SELECT `Sequence` FROM `InstallExecuteSequence` WHERE `Action`=''RemoveExistingProducts''') -ne '6550') {throw 'Upgrade is outside the rollback transaction'}
    if ((Value 'SELECT `Condition` FROM `InstallExecuteSequence` WHERE `Action`=''UnregisterTIP''') -ne 'REMOVE="ALL" AND NOT UPGRADINGPRODUCTCODE AND NOT SHIXIN_GATE') {throw 'An upgrade would remove the shared TIP'}
    if ((Value 'SELECT `Version` FROM `File` WHERE `FileName`=''rime.dll''') -ne '100.1.2.3') {throw 'Native file version missing or wrong'}
    if ((Value 'SELECT `Language` FROM `File` WHERE `FileName`=''rime.dll''') -ne '1033') {throw 'Native resource language missing'}
    if ((Value 'SELECT `Type` FROM `CustomAction` WHERE `Action`=''RegisterTIP''') -ne '3090') {throw 'Registration is not deferred as system'}
    if ((Value 'SELECT `SDDLText` FROM `MsiLockPermissionsEx` WHERE `LockObject`=''ShixinInstallDir''') -notmatch 'FRFX;;;AC') {throw 'Sandbox apps cannot load the DLLs'}
    if ((Value 'SELECT `Name` FROM `_Streams` WHERE `Name`=''payload.cab''') -ne 'payload.cab') {throw 'Missing embedded offline payload'}
    $null=$db.Export('_SummaryInformation',$temp,'summary.idt')
    $summary=Get-Content "$temp/summary.idt" -Raw
    $platform=if ($Arch -eq 'arm64') {'Arm64'} else {'x64'}
    if ($summary -notmatch "7`t$platform;1033" -or $summary -notmatch "1`t1252") {throw 'Invalid platform/codepage in package summary'}
    # Opening a session checks the actual MSI package format, rather than
    # merely the structured-storage database. No actions/install are run.
    $session=$i.OpenPackage("$temp/test.msi",1)
    if ($session.DoAction('AppSearch') -ne 1) {throw 'OS version registry search failed'}
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($session) | Out-Null
    Write-Host 'MSI authoring checks passed (nothing installed)'
} finally {
    if ($db) { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($db) | Out-Null }
    if ($i) { [Runtime.InteropServices.Marshal]::FinalReleaseComObject($i) | Out-Null }
    [GC]::Collect(); [GC]::WaitForPendingFinalizers()
    Remove-Item $temp -Recurse -Force
}
