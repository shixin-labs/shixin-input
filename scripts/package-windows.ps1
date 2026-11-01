# Produce one Authenticode-signed Windows installer with bundled data.
# Requires native Windows, PowerShell 7, Go, Visual Studio C++, Windows SDK,
# fetched Rime data and our matching build-librime.ps1 output. Uses only
# installed SDK tools and Windows Installer; never downloads executable tools.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Version,
    [ValidateSet('amd64','arm64')][string]$Arch = 'amd64',
    [Parameter(Mandatory)][ValidatePattern('^[0-9A-Fa-f]{40}$')][string]$CertificateThumbprint,
    [ValidateSet('CurrentUser','LocalMachine')][string]$CertificateStore = 'CurrentUser',
    [string]$TimestampServer = 'http://timestamp.digicert.com',
    [string]$SignTool,
    [string]$ManifestTool,
    [string]$Data,
    [string]$Librime,
    [string]$OpenCC
)
$ErrorActionPreference='Stop'
if (-not $IsWindows) { throw 'Windows packaging requires native Windows and PowerShell 7' }
if ($Version -notmatch '^(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,4})$' -or
    [int]$Matches[1] -gt 255 -or [int]$Matches[2] -gt 255 -or [int]$Matches[3] -gt 65535) { throw 'Use a stable MSI version: major.minor.build (255.255.65535 maximum)' }
$root=Split-Path $PSScriptRoot -Parent
$certificate=Get-Item "Cert:/$CertificateStore/My/$CertificateThumbprint"
if (-not $certificate.HasPrivateKey) { throw 'The signing certificate has no accessible private key' }
if (-not ($certificate.EnhancedKeyUsageList.ObjectId -contains '1.3.6.1.5.5.7.3.3')) { throw 'The certificate must allow code signing' }
if (-not $SignTool -or -not $ManifestTool) {
    $kits=(Get-ItemProperty 'HKLM:/SOFTWARE/Microsoft/Windows Kits/Installed Roots').KitsRoot10
    $sdk=Get-ChildItem (Join-Path $kits 'bin') -Directory | Where-Object Name -Match '^10\.' | Sort-Object {[version]$_.Name} -Descending | Select-Object -First 1
    $hostTool=if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') {'arm64'} else {'x64'}
    if (-not $SignTool) {$SignTool=Join-Path $sdk.FullName "$hostTool/signtool.exe"}
    if (-not $ManifestTool) {$ManifestTool=Join-Path $sdk.FullName "$hostTool/mt.exe"}
}
function Check([string]$step) { if ($LASTEXITCODE -ne 0) { throw "$step failed (exit $LASTEXITCODE)" } }
function Manifest([string]$path) {
    & $ManifestTool -nologo -manifest "$root/packaging/windows/setup.manifest" "-outputresource:$path;#1"
    Check "set asInvoker manifest in $path"
    # mt.exe may discard the Go linker's trailing COFF string table while
    # retaining its header pointer. Stripped release executables use no
    # COFF symbols/long section names; clear those stale diagnostic fields.
    $bytes=[IO.File]::ReadAllBytes($path)
    $peOffset=[BitConverter]::ToInt32($bytes,0x3c)
    [Array]::Clear($bytes,$peOffset+12,8)
    [IO.File]::WriteAllBytes($path,$bytes)
}
function Sign([string]$path) {
    $options=@('sign','/sha1',$CertificateThumbprint,'/fd','SHA256','/s','My','/d','拾新输入法 Shixin Input','/du','https://github.com/shixin-labs/shixin-input')
    if ($CertificateStore -eq 'LocalMachine') { $options+='/sm' }
    if ($TimestampServer) { $options+=@('/tr',$TimestampServer,'/td','SHA256') }
    & $SignTool @options $path | Out-Null; Check "sign $path"
    & $SignTool verify /pa /all $path | Out-Null; Check "verify $path"
}
$savedOS=$env:GOOS; $savedArch=$env:GOARCH; $savedCGO=$env:CGO_ENABLED
Push-Location $root
try {
    if ((& go env GOHOSTARCH).Trim() -ne $Arch) { throw 'Build release packages on the matching native Windows architecture' }
    $env:GOOS='windows'; $env:GOARCH=$Arch; $env:CGO_ENABLED='0'
    $work=Join-Path $root "build/package-windows-$Arch"
    $payload=Join-Path $work 'payload'
    Remove-Item $payload -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force $payload | Out-Null
    Set-Content (Join-Path $root 'build/go.mod') 'module shixin/build' -Encoding ascii
    $flags="-s -w -X main.version=$Version"
    foreach ($name in @('shixin-engine','shixin-builder','shixin-ai')) {
        & go build -trimpath -ldflags $flags -o (Join-Path $payload "$name.exe") "./cmd/$name"; Check "build $name"
    }
    foreach ($entry in @(@('shixin','Shixin'),@('shixin-settings','shixin-settings'),@('shixin-setup','ShixinSetup'))) {
        & go build -trimpath -ldflags "$flags -H windowsgui" -o (Join-Path $payload ($entry[1]+'.exe')) "./cmd/$($entry[0])"; Check "build $($entry[0])"
    }
    $target=if ($Arch -eq 'arm64') {'ARM64'} else {'x64'}
    $native=Join-Path $root "build/tsf-$Arch"
    $fwdResource=Join-Path $work 'forwarder-version.res'
    & go run ./tools/windows-package -version-res $fwdResource -version $Version; Check 'write forwarder version resource'
    & cmake -S frontends/windows-tsf -B $native -A $target "-DSHIXIN_TSF_ARM64X=$($Arch -eq 'arm64' ? 'ON' : 'OFF')" "-DSHIXIN_TSF_VERSION_RESOURCE=$fwdResource" -DSHIXIN_TSF_TRACE=OFF; Check 'configure TSF'
    & cmake --build $native --config Release; Check 'build TSF'
    & ctest --test-dir $native -C Release --output-on-failure; Check 'test TSF'
    if ($Arch -eq 'arm64') {
        $x64=Join-Path $root 'build/tsf-amd64'
        & cmake -S frontends/windows-tsf -B $x64 -A x64 -DSHIXIN_TSF_TRACE=OFF; Check 'configure x64 TSF'
        & cmake --build $x64 --config Release; Check 'build x64 TSF'
        & ctest --test-dir $x64 -C Release --output-on-failure; Check 'test emulated x64 TSF'
        & cmake --build $native --config Release --target shixin_tsf_forwarder; Check 'build ARM64X forwarder'
        Copy-Item "$native/Release/shixin_tsf.dll" "$payload/shixin_tsf_arm64.dll"
        Copy-Item "$x64/Release/shixin_tsf.dll" "$payload/shixin_tsf_x64.dll"
        Copy-Item "$native/forwarder/shixin_tsf.dll" "$payload/shixin_tsf.dll"
    } else { Copy-Item "$native/Release/shixin_tsf.dll" "$payload/shixin_tsf.dll" }
    if (-not $Librime) { $Librime=Join-Path $root "build/windows-$Arch/rime.dll" }
    if (-not $OpenCC) { $OpenCC=Join-Path $root "build/librime-windows-$Arch/out/share/opencc" }
    if (-not $Data) { $Data=Join-Path $root 'build/rime-data' }
    if (-not (Test-Path "$Data/rime_ice.schema.yaml") -or -not (Test-Path "$OpenCC/s2t.json")) { throw 'Fetch Rime data and build librime/OpenCC first' }
    Copy-Item $Librime "$payload/rime.dll"
    $runtimeData=Join-Path $payload 'data'
    New-Item -ItemType Directory $runtimeData | Out-Null
    & robocopy $Data $runtimeData /E /XD lua .git .github /XF '*.custom.yaml' weasel.yaml squirrel.yaml /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw 'copy Rime data failed' }
    New-Item -ItemType Directory -Force "$runtimeData/opencc" | Out-Null
    Copy-Item "$OpenCC/*" "$runtimeData/opencc" -Recurse -Force
    if (Get-ChildItem $runtimeData -Recurse -File | Where-Object Extension -In @('.exe','.dll')) {throw 'Rime data must not contain executable files'}
    $generation=Join-Path $work 'generation'
    Remove-Item $generation -Recurse -Force -ErrorAction SilentlyContinue
    & "$payload/shixin-builder.exe" -lib "$payload/rime.dll" -data $runtimeData -all -output $generation; Check 'compile every supported dictionary'
    New-Item -ItemType Directory "$payload/prebuilt" | Out-Null
    Copy-Item "$generation/build/*.bin" "$payload/prebuilt"
    foreach ($dict in (Get-ChildItem $runtimeData -Filter '*.dict.yaml')) {
        $table=($dict.Name -replace '\.dict\.yaml$','.table.bin')
        if (-not (Test-Path "$payload/prebuilt/$table")) { throw "Missing prebuilt table for $($dict.Name)" }
    }
    Get-ChildItem $runtimeData -Recurse -Filter '*.dict.yaml' | Remove-Item
    $licenses=Join-Path $payload 'licenses'
    New-Item -ItemType Directory $licenses | Out-Null
    Copy-Item LICENSE "$licenses/Shixin (GPL-3.0-or-later).txt"
    Copy-Item third_party/librime/LICENSE "$licenses/librime (BSD-3-Clause).txt"
    Copy-Item "$Data/LICENSE" "$licenses/rime-ice.txt"
    Copy-Item (Join-Path (& go env GOROOT).Trim() 'LICENSE') "$licenses/Go.txt"
    foreach ($module in (& go list -m -f '{{.Path}}|{{.Dir}}' all)) {
        $pair=$module.Split('|',2)
        if ($pair[0] -eq 'shixin' -or -not $pair[1]) {continue}
        $prefix=$pair[0] -replace '[^A-Za-z0-9.-]','_'
        foreach ($notice in (Get-ChildItem $pair[1] -File | Where-Object Name -Match '^(LICENSE|COPYING|NOTICE)')) {
            Copy-Item $notice.FullName (Join-Path $licenses ($prefix+'-'+$notice.Name+'.txt'))
        }
    }
    foreach ($dependency in (Get-ChildItem third_party/librime/deps -Directory)) {
        foreach ($notice in (Get-ChildItem $dependency.FullName -File | Where-Object Name -Match '^(LICENSE|COPYING|NOTICE)')) {
            Copy-Item $notice.FullName (Join-Path $licenses ($dependency.Name+'-'+$notice.Name+'.txt'))
        }
    }
    # The Setup filename otherwise triggers Windows' installer heuristics.
    # Keep the bootstrapper in the original user's token; only MSI elevates.
    foreach ($exe in (Get-ChildItem $payload -File -Filter *.exe)) {Manifest $exe.FullName}
    $skipVersion=if ($Arch -eq 'arm64') {'shixin_tsf.dll'} else {''}
    & go run ./tools/windows-package -stamp-dir $payload -version $Version -skip $skipVersion; Check 'stamp release file versions'
    & "$native/Release/shixin_dll_test.exe" "$payload/shixin_tsf.dll"; Check 'test stamped public TSF DLL'
    if ($Arch -eq 'arm64') { & "$x64/Release/shixin_dll_test.exe" "$payload/shixin_tsf.dll"; Check 'test stamped public TSF DLL from x64' }
    foreach ($binary in (Get-ChildItem $payload -Recurse -File | Where-Object Extension -In @('.exe','.dll'))) { Sign $binary.FullName }
    $msi=Join-Path $work 'Shixin.msi'
    & (Get-Process -Id $PID).Path -NoProfile -ExecutionPolicy Bypass -File "$root/packaging/windows/msi.ps1" -Payload $payload -Output $msi -Version $Version -Arch $Arch
    Check 'author MSI'
    Sign $msi
    $dist=Join-Path $root 'build/dist'
    New-Item -ItemType Directory -Force $dist | Out-Null
    $setup=Join-Path $dist "ShixinSetup-$Arch.exe"
    & go build -trimpath -ldflags "$flags -H windowsgui" -o $setup ./cmd/shixin-setup; Check 'build bootstrapper'
    Manifest $setup
    & go run ./tools/windows-package -exe $setup -msi $msi -version $Version -arch $Arch; Check 'attach signed MSI'
    Sign $setup
    Write-Host "Release installer: $setup"
} finally {
    $env:GOOS=$savedOS; $env:GOARCH=$savedArch; $env:CGO_ENABLED=$savedCGO
    Pop-Location
}
