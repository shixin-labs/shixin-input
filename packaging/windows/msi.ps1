# Build an MSI using Windows' built-in Installer automation and makecab.
# No downloaded installer compiler or scripting custom action is required.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Payload,
    [Parameter(Mandatory)][string]$Output,
    [Parameter(Mandatory)][string]$Version,
    [ValidateSet('amd64','arm64')][string]$Arch
)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'MSI authoring requires Windows PowerShell 7' }
$Payload = (Resolve-Path $Payload).Path
$Output = [IO.Path]::GetFullPath($Output)
$work = Join-Path (Split-Path $Output) 'msi-work'
New-Item -ItemType Directory -Force $work | Out-Null
Remove-Item $Output -ErrorAction SilentlyContinue
$installer = New-Object -ComObject WindowsInstaller.Installer
$db = $installer.OpenDatabase($Output, 3) # create, transactional
function SQL([string]$statement) { try { $view=$db.OpenView($statement); $view.Execute(); $view.Close(); [Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null } catch { throw "MSI SQL failed: $statement ($($_.Exception.Message))" } }
$schemas=@{}
function Table([string]$name,[string]$columns,[string]$key) {
    SQL "CREATE TABLE ``$name`` ($columns PRIMARY KEY $key)"
    $schemas[$name]=([regex]::Matches($columns,'`[^`]+`(?= (?:CHAR|SHORT|LONG|OBJECT))') | ForEach-Object Value) -join ','
}
function Add([string]$table,[object[]]$values) {
    $marks = ($values | ForEach-Object {'?'}) -join ','
    try { $view = $db.OpenView("INSERT INTO ``$table`` ($($schemas[$table])) VALUES ($marks)") } catch {throw "MSI insert failed in ${table}: $marks ($($_.Exception.Message))"}
    $record = $installer.CreateRecord($values.Count)
    for ($i=0; $i -lt $values.Count; $i++) {
        if ($null -eq $values[$i]) { continue }
        if ($values[$i] -is [int]) { $record.IntegerData($i+1)=$values[$i] }
        else { $record.StringData($i+1)=[string]$values[$i] }
    }
    $view.Execute($record); $view.Close()
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($record) | Out-Null
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null
}
function GuidFor([string]$value) {
    $bytes=[Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes("shixin-msi-v1/$Arch/$value"))[0..15]
    return ([Guid]::new([byte[]]$bytes)).ToString('B').ToUpperInvariant()
}
Table Property '`Property` CHAR(72) NOT NULL, `Value` CHAR(0) LOCALIZABLE' '`Property`'
$code=[Guid]::NewGuid().ToString('B').ToUpperInvariant()
$upgrade=GuidFor 'upgrade'
$properties=@{
    ProductCode=$code; UpgradeCode=$upgrade; ProductName='Shixin Input'; Manufacturer='Shixin Labs'
    ProductVersion=$Version; ProductLanguage='1033'; ALLUSERS='1'; INSTALLLEVEL='1'
    ARPSYSTEMCOMPONENT='1'; REBOOT='ReallySuppress'; MSIRESTARTMANAGERCONTROL='Disable'
    SecureCustomProperties='SHIXIN_OLD;SHIXIN_NEWER;SHIXIN_GATE;SHIXIN_READY'; ARPNOREPAIR='1'; ARPNOMODIFY='1'
}
foreach ($key in $properties.Keys) { Add Property @($key,$properties[$key]) }
Table Directory '`Directory` CHAR(72) NOT NULL, `Directory_Parent` CHAR(72), `DefaultDir` CHAR(255) NOT NULL LOCALIZABLE' '`Directory`'
Add Directory @('TARGETDIR',$null,'SourceDir')
Add Directory @('ProgramFiles64Folder','TARGETDIR','.')
Add Directory @('ShixinInstallDir','ProgramFiles64Folder','Shixin')
Add Directory @('ProgramMenuFolder','TARGETDIR','.')
Add Directory @('MenuDir','ProgramMenuFolder','Shixin')
Table Component '`Component` CHAR(72) NOT NULL, `ComponentId` CHAR(38), `Directory_` CHAR(72) NOT NULL, `Attributes` SHORT NOT NULL, `Condition` CHAR(255), `KeyPath` CHAR(72)' '`Component`'
Table File '`File` CHAR(72) NOT NULL, `Component_` CHAR(72) NOT NULL, `FileName` CHAR(255) NOT NULL LOCALIZABLE, `FileSize` LONG NOT NULL, `Version` CHAR(72), `Language` CHAR(20), `Attributes` SHORT, `Sequence` LONG NOT NULL' '`File`'
Table Feature '`Feature` CHAR(38) NOT NULL, `Feature_Parent` CHAR(38), `Title` CHAR(64) LOCALIZABLE, `Description` CHAR(255) LOCALIZABLE, `Display` SHORT, `Level` SHORT NOT NULL, `Directory_` CHAR(72), `Attributes` SHORT NOT NULL' '`Feature`'
Table FeatureComponents '`Feature_` CHAR(38) NOT NULL, `Component_` CHAR(72) NOT NULL' '`Feature_`,`Component_`'
Add Feature @('Main',$null,'Shixin Input','Shixin Chinese input method',1,1,'ShixinInstallDir',0)
$dirs=@{''='ShixinInstallDir'}
$files=@{}
$sequence=0
$ddf=[Collections.Generic.List[string]]::new()
$ddf.Add('.OPTION EXPLICIT')
$ddf.Add('.Set CabinetNameTemplate=payload.cab')
$ddf.Add('.Set DiskDirectoryTemplate="'+$work+'"')
$ddf.Add('.Set Cabinet=ON'); $ddf.Add('.Set Compress=ON'); $ddf.Add('.Set CompressionType=MSZIP')
$ddf.Add('.Set MaxDiskSize=0'); $ddf.Add('.Set MaxCabinetSize=0')
foreach ($file in (Get-ChildItem $Payload -Recurse -File | Sort-Object FullName)) {
    if ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Payload cannot contain links' }
    $relative=[IO.Path]::GetRelativePath($Payload,$file.FullName)
    $parent=Split-Path $relative -Parent
    $parts=$parent -split '[\\/]'
    $current=''; $previous='ShixinInstallDir'
    foreach ($part in $parts) {
        if (-not $part) { continue }
        $current=if ($current) { "$current/$part" } else { $part }
        if (-not $dirs.ContainsKey($current)) {
            $id='d'+(GuidFor $current).Replace('-','').Trim('{}')
            $dirs[$current]=$id; Add Directory @($id,$previous,$part)
        }
        $previous=$dirs[$current]
    }
    $id='f'+(++$sequence)
    $component='c'+$sequence
    $files[$relative.Replace('\','/')]=@{ File=$id; Component=$component }
    Add Component @($component,(GuidFor $relative.ToLowerInvariant()),$previous,256,$null,$id)
    Add FeatureComponents @('Main',$component)
    $fileVersion=$null; $fileLanguage=$null
    try { $fileVersion=$installer.FileVersion($file.FullName,0); $fileLanguage=$installer.FileVersion($file.FullName,1) } catch {}
    Add File @($id,$component,$file.Name,[int]$file.Length,$fileVersion,$fileLanguage,512,$sequence)
    $ddf.Add('"'+$file.FullName+'" '+$id)
}
# A fixed protected install root is required by sibling authentication and
# prevents custom action execution from a user-writable directory.
Table CreateFolder '`Directory_` CHAR(72) NOT NULL, `Component_` CHAR(72) NOT NULL' '`Directory_`,`Component_`'
$front=$files['Shixin.exe']; $setup=$files['ShixinSetup.exe']; $settings=$files['shixin-settings.exe']
if (-not $front -or -not $setup -or -not $settings) { throw 'Missing frontend, maintenance helper or settings app' }
Add CreateFolder @('ShixinInstallDir',$front.Component)
Table MsiLockPermissionsEx '`MsiLockPermissionsEx` CHAR(72) NOT NULL, `LockObject` CHAR(72) NOT NULL, `Table` CHAR(32) NOT NULL, `SDDLText` CHAR(0) NOT NULL, `Condition` CHAR(255)' '`MsiLockPermissionsEx`'
Add MsiLockPermissionsEx @('ProtectedRoot','ShixinInstallDir','CreateFolder','D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FRFX;;;BU)(A;OICI;FRFX;;;AC)',$null)
Table Media '`DiskId` SHORT NOT NULL, `LastSequence` LONG NOT NULL, `DiskPrompt` CHAR(64) LOCALIZABLE, `Cabinet` CHAR(255), `VolumeLabel` CHAR(32), `Source` CHAR(72)' '`DiskId`'
Add Media @(1,$sequence,$null,'#payload.cab',$null,$null)
$ddfPath=Join-Path $work 'payload.ddf'
[IO.File]::WriteAllLines($ddfPath,$ddf,[Text.Encoding]::Default)
& "$env:SystemRoot/System32/makecab.exe" /F $ddfPath | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'makecab failed' }
$view=$db.OpenView('INSERT INTO `_Streams` (`Name`,`Data`) VALUES (?,?)')
$record=$installer.CreateRecord(2); $record.StringData(1)='payload.cab'; $record.SetStream(2,(Join-Path $work 'payload.cab'))
$view.Execute($record); $view.Close()
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($record) | Out-Null
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null
Table Upgrade '`UpgradeCode` CHAR(38) NOT NULL, `VersionMin` CHAR(20), `VersionMax` CHAR(20), `Language` CHAR(255), `Attributes` LONG NOT NULL, `Remove` CHAR(255), `ActionProperty` CHAR(72) NOT NULL' '`UpgradeCode`,`VersionMin`,`VersionMax`,`Language`,`Attributes`'
Add Upgrade @($upgrade,$null,$Version,$null,1,$null,'SHIXIN_OLD') # migrate features, max exclusive
Add Upgrade @($upgrade,$Version,$null,$null,258,$null,'SHIXIN_NEWER') # detect only, min inclusive
Table Signature '`Signature` CHAR(72) NOT NULL, `FileName` CHAR(255) NOT NULL LOCALIZABLE, `MinVersion` CHAR(20), `MaxVersion` CHAR(20), `MinSize` LONG, `MaxSize` LONG, `MinDate` LONG, `MaxDate` LONG, `Languages` CHAR(255)' '`Signature`'
Table AppSearch '`Property` CHAR(72) NOT NULL, `Signature_` CHAR(72) NOT NULL' '`Property`,`Signature_`'
Table RegLocator '`Signature_` CHAR(72) NOT NULL, `Root` SHORT NOT NULL, `Key` CHAR(255) NOT NULL LOCALIZABLE, `Name` CHAR(255) LOCALIZABLE, `Type` SHORT' '`Signature_`'
Add AppSearch @('ShixinMajor','OSMajor')
Add RegLocator @('OSMajor',2,'SOFTWARE\Microsoft\Windows NT\CurrentVersion','CurrentMajorVersionNumber',18)
Add AppSearch @('ShixinBuild','OSBuild')
Add RegLocator @('OSBuild',2,'SOFTWARE\Microsoft\Windows NT\CurrentVersion','CurrentBuildNumber',18)
Table LaunchCondition '`Condition` CHAR(255) NOT NULL, `Description` CHAR(255) NOT NULL LOCALIZABLE' '`Condition`'
Add LaunchCondition @('Installed OR NOT SHIXIN_NEWER','A newer or equal version of Shixin is already installed.')
Add LaunchCondition @('Installed OR (VersionNT64 >= 603 AND ShixinMajor = "#10")','Shixin requires 64-bit Windows 10 or later.')
if ($Arch -eq 'arm64') { Add LaunchCondition @('Installed OR ShixinBuild >= 22000','Shixin ARM64 requires Windows 11 or later.') }
Table Binary '`Name` CHAR(72) NOT NULL, `Data` OBJECT NOT NULL' '`Name`'
$view=$db.OpenView('INSERT INTO `Binary` (`Name`,`Data`) VALUES (?,?)')
$record=$installer.CreateRecord(2); $record.StringData(1)='SetupCheck'; $record.SetStream(2,(Join-Path $Payload 'ShixinSetup.exe'))
$null=$view.Execute($record); $null=$view.Close()
[Runtime.InteropServices.Marshal]::FinalReleaseComObject($record) | Out-Null
[Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null
Table CustomAction '`Action` CHAR(72) NOT NULL, `Type` SHORT NOT NULL, `Source` CHAR(72), `Target` CHAR(255), `ExtendedType` LONG' '`Action`'
# Type 18: installed executable; 1024: deferred; 2048: system; 256: rollback.
Add CustomAction @('CheckStopped',3074,'SetupCheck','-check-stopped',$null)
Add CustomAction @('RegisterTIP',3090,$front.File,'-register -system-registration',$null)
Add CustomAction @('RollbackRegisterTIP',3346,$front.File,'-unregister -system-registration',$null)
Add CustomAction @('UnregisterTIP',3090,$front.File,'-unregister -system-registration',$null)
Add CustomAction @('UnregisterTIPGated',3090,$front.File,'-unregister -system-registration -registration-gate "[SHIXIN_GATE]" -registration-ready "[SHIXIN_READY]"',$null)
Add CustomAction @('RollbackUnregisterTIP',3346,$front.File,'-register -system-registration',$null)
Table Registry '`Registry` CHAR(72) NOT NULL, `Root` SHORT NOT NULL, `Key` CHAR(255) NOT NULL LOCALIZABLE, `Name` CHAR(255) LOCALIZABLE, `Value` CHAR(0) LOCALIZABLE, `Component_` CHAR(72) NOT NULL' '`Registry`'
$arp='Software\Microsoft\Windows\CurrentVersion\Uninstall\Shixin'
$values=@{
 DisplayName='拾新输入法 Shixin Input'; DisplayVersion=$Version; Publisher='Shixin Labs'
 InstallLocation='[ShixinInstallDir]'; UninstallString='"[ShixinInstallDir]ShixinSetup.exe" -uninstall'
 ProductCode=$code; URLInfoAbout='https://github.com/shixin-labs/shixin-input'
 NoModify='#1'; NoRepair='#1'; EstimatedSize=('#'+[int][Math]::Ceiling(((Get-ChildItem $Payload -Recurse -File | Measure-Object Length -Sum).Sum)/1024))
}
foreach ($name in $values.Keys) { Add Registry @(('arp'+$name),2,$arp,$name,$values[$name],$setup.Component) }
Table Shortcut '`Shortcut` CHAR(72) NOT NULL, `Directory_` CHAR(72) NOT NULL, `Name` CHAR(128) NOT NULL LOCALIZABLE, `Component_` CHAR(72) NOT NULL, `Target` CHAR(255), `Arguments` CHAR(255), `Description` CHAR(255) LOCALIZABLE, `Hotkey` SHORT, `Icon_` CHAR(72), `IconIndex` SHORT, `ShowCmd` SHORT, `WkDir` CHAR(72)' '`Shortcut`'
Add Shortcut @('Settings','MenuDir','Shixin Settings',$settings.Component,('[#'+$settings.File+']'),$null,'拾新设置 Shixin Settings',$null,$null,$null,1,'ShixinInstallDir')
Table InstallExecuteSequence '`Action` CHAR(72) NOT NULL, `Condition` CHAR(255), `Sequence` SHORT' '`Action`'
$actions=@{
 FindRelatedProducts=25; AppSearch=50; LaunchConditions=100; ValidateProductID=700; CostInitialize=800; FileCost=900; CostFinalize=1000
 MigrateFeatureStates=1200; InstallValidate=1400; InstallInitialize=1500; InstallExecute=1506; RemoveExistingProducts=6550
 ProcessComponents=1600; UnpublishFeatures=1800; RemoveShortcuts=3200; RemoveFiles=3500; RemoveFolders=3600
 CreateFolders=3700; InstallFiles=4000; CreateShortcuts=4500; WriteRegistryValues=5000
 RemoveRegistryValues=2600; RegisterUser=6000; RegisterProduct=6100; PublishFeatures=6300; PublishProduct=6400; InstallExecuteAgain=6500; InstallFinalize=6600
}
foreach ($action in $actions.Keys) {
    $condition=if ($action -eq 'InstallExecuteAgain') {'NOT (REMOVE="ALL")'} else {$null}
    Add InstallExecuteSequence @($action,$condition,$actions[$action])
}
Add InstallExecuteSequence @('CheckStopped','NOT UPGRADINGPRODUCTCODE',1505)
Add InstallExecuteSequence @('RollbackUnregisterTIP','REMOVE="ALL" AND NOT UPGRADINGPRODUCTCODE',3430)
Add InstallExecuteSequence @('UnregisterTIP','REMOVE="ALL" AND NOT UPGRADINGPRODUCTCODE AND NOT SHIXIN_GATE',3440)
Add InstallExecuteSequence @('UnregisterTIPGated','REMOVE="ALL" AND NOT UPGRADINGPRODUCTCODE AND SHIXIN_GATE',3441)
Add InstallExecuteSequence @('RollbackRegisterTIP','NOT Installed AND NOT SHIXIN_OLD',4090)
Add InstallExecuteSequence @('RegisterTIP','NOT (REMOVE="ALL")',4100)
$summary=$db.SummaryInformation(20)
$summary.Property(1)=1252 # summary strings are ASCII; UTF-8 is not an ANSI summary codepage
$summary.Property(2)='Shixin Input Setup'; $summary.Property(3)='Shixin Input'; $summary.Property(4)='Shixin Labs'
$summary.Property(7)=if ($Arch -eq 'arm64') {'Arm64;1033'} else {'x64;1033'}
$summary.Property(9)=[Guid]::NewGuid().ToString('B').ToUpperInvariant()
$summary.Property(14)=500 # Windows Installer 5 (MsiLockPermissionsEx, ARM64)
$summary.Property(15)=2 # compressed, long file names, elevated per-machine
$summary.Property(18)='Shixin Labs'
$summary.Persist(); $db.Commit()
[Runtime.InteropServices.Marshal]::FinalReleaseComObject($summary) | Out-Null
[Runtime.InteropServices.Marshal]::FinalReleaseComObject($db) | Out-Null
[Runtime.InteropServices.Marshal]::FinalReleaseComObject($installer) | Out-Null
Write-Host "Authored $Output ($code)"
