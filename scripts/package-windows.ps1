# Produce a signed Windows installer, or an explicitly unsigned manual preview.
# Requires native Windows, PowerShell 7, Go, Visual Studio C++, Windows SDK,
# fetched Rime data and our matching build-librime.ps1 output with its staged
# license notices. Uses installed SDK tools, Windows Installer and an optional
# caller-supplied SSL.com CodeSignTool directory; never downloads tools itself.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Version,
    [ValidateSet('amd64','arm64')][string]$Arch = 'amd64',
    [ValidatePattern('^[0-9A-Fa-f]{40}$')][string]$CertificateThumbprint,
    [string]$CodeSignTool,
    [string]$Java,
    [switch]$UnsignedPreview,
    [ValidateSet('CurrentUser','LocalMachine')][string]$CertificateStore = 'CurrentUser',
    [string]$TimestampServer = 'http://timestamp.digicert.com',
    [string]$SignTool,
    [string]$ManifestTool,
    [string]$Data,
    [string]$Librime,
    [string]$OpenCC,
    [string]$LibrimeLicenses,
    [string]$Llama
)
$ErrorActionPreference='Stop'
if (-not $IsWindows) { throw 'Windows packaging requires native Windows and PowerShell 7' }
if ($Version -notmatch '^(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,4})$' -or
    [int]$Matches[1] -gt 255 -or [int]$Matches[2] -gt 255 -or [int]$Matches[3] -gt 65535) { throw 'Use a stable MSI version: major.minor.build (255.255.65535 maximum)' }
$root=Split-Path $PSScriptRoot -Parent
$modes=@([bool]$UnsignedPreview,[bool]$CertificateThumbprint,[bool]$CodeSignTool) | Where-Object { $_ }
if ($modes.Count -ne 1) { throw 'Choose exactly one of -CertificateThumbprint, -CodeSignTool or -UnsignedPreview' }
$cstJar=$null
if ($CertificateThumbprint) {
    $certificate=Get-Item "Cert:/$CertificateStore/My/$CertificateThumbprint"
    if (-not $certificate.HasPrivateKey) { throw 'The signing certificate has no accessible private key' }
    if (-not ($certificate.EnhancedKeyUsageList.ObjectId -contains '1.3.6.1.5.5.7.3.3')) { throw 'The certificate must allow code signing' }
} elseif ($CodeSignTool) {
    $CodeSignTool=(Resolve-Path $CodeSignTool).Path
    $cstJar=Get-ChildItem (Join-Path $CodeSignTool 'jar') -File -Filter 'code_sign_tool-*.jar' | Sort-Object Name -Descending | Select-Object -First 1
    if (-not $cstJar) { throw "No code_sign_tool-*.jar found under $CodeSignTool/jar" }
    if (-not (Test-Path (Join-Path $CodeSignTool 'conf/code_sign_tool.properties'))) { throw "Missing conf/code_sign_tool.properties under $CodeSignTool" }
    foreach ($name in @('ES_USERNAME','ES_PASSWORD','ES_CREDENTIAL_ID','ES_TOTP_SECRET')) {
        if (-not [Environment]::GetEnvironmentVariable($name)) { throw "Environment variable $name is required for -CodeSignTool signing" }
    }
    if (-not $Java) {
        if ($env:JAVA_HOME -and (Test-Path (Join-Path $env:JAVA_HOME 'bin/java.exe'))) {
            $Java=Join-Path $env:JAVA_HOME 'bin/java.exe'
        } else {
            $Java=(Get-Command java -ErrorAction Stop).Source
        }
    }
}
if (-not $SignTool -or -not $ManifestTool) {
    $kits=(Get-ItemProperty 'HKLM:/SOFTWARE/Microsoft/Windows Kits/Installed Roots').KitsRoot10
    $sdk=Get-ChildItem (Join-Path $kits 'bin') -Directory | Where-Object Name -Match '^10\.' | Sort-Object {[version]$_.Name} -Descending | Select-Object -First 1
    $hostTool=if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') {'arm64'} else {'x64'}
    if (-not $SignTool) {
        $SignTool=Join-Path $sdk.FullName "$hostTool/signtool.exe"
        if (-not (Test-Path $SignTool)) { $SignTool=Join-Path $sdk.FullName 'x64/signtool.exe' }
    }
    if (-not $ManifestTool) {
        $ManifestTool=Join-Path $sdk.FullName "$hostTool/mt.exe"
        if (-not (Test-Path $ManifestTool)) { $ManifestTool=Join-Path $sdk.FullName 'x64/mt.exe' }
    }
}
function Check([string]$step) { if ($LASTEXITCODE -ne 0) { throw "$step failed (exit $LASTEXITCODE)" } }
function Redact([string]$text) {
    if (-not $text) { return '' }
    foreach ($secret in @($env:ES_PASSWORD,$env:ES_TOTP_SECRET)) {
        if ($secret) { $text=$text.Replace($secret,'***') }
    }
    return $text
}
$script:lastCodeSignWindow=-1
function WaitFreshTOTPWindow() {
    if ($script:lastCodeSignWindow -lt 0) { return }
    while ($true) {
        $now=[DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
        $win=[long][Math]::Floor($now / 30)
        if ($win -gt $script:lastCodeSignWindow) { return }
        $waitSec=[int]((($script:lastCodeSignWindow + 1) * 30) - $now + 1)
        Write-Host "Waiting ${waitSec}s for next eSigner TOTP window..."
        Start-Sleep -Seconds ([Math]::Max(1,$waitSec))
    }
}
function InvokeCodeSignTool([string[]]$commandArgs) {
    WaitFreshTOTPWindow
    $startWin=[long][Math]::Floor([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() / 30)
    $start=[Diagnostics.ProcessStartInfo]::new($Java)
    $start.WorkingDirectory=$CodeSignTool
    $start.UseShellExecute=$false
    $start.RedirectStandardOutput=$true
    $start.RedirectStandardError=$true
    $start.Environment['CODE_SIGN_TOOL_PATH']=$CodeSignTool
    $start.ArgumentList.Add('-Dfile.encoding=UTF-8')
    $start.ArgumentList.Add('-Xmx1024M')
    $start.ArgumentList.Add('-jar')
    $start.ArgumentList.Add($cstJar.FullName)
    foreach ($arg in $commandArgs) { $start.ArgumentList.Add($arg) }
    $process=[Diagnostics.Process]::Start($start)
    $outTask=$process.StandardOutput.ReadToEndAsync()
    $errTask=$process.StandardError.ReadToEndAsync()
    $process.WaitForExit()
    $endWin=[long][Math]::Floor([DateTimeOffset]::UtcNow.ToUnixTimeSeconds() / 30)
    $script:lastCodeSignWindow=[Math]::Max($startWin,$endWin)
    $stdout=$outTask.GetAwaiter().GetResult()
    $stderr=$errTask.GetAwaiter().GetResult()
    $combined=($stdout + "`n" + $stderr).Trim()
    if ($process.ExitCode -ne 0 -or $combined -match '(?m)^(Error|Exception)\b|Exception:|Error:|Missing required option|Unmatched argument') {
        throw "CodeSignTool $($commandArgs[0]) failed (exit $($process.ExitCode)): $(Redact $combined)"
    }
    return (Redact $combined)
}
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
    if ($UnsignedPreview) { return }
    if ($CodeSignTool) {
        $stageOut=Join-Path ([IO.Path]::GetDirectoryName($path)) ('sign-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Force $stageOut | Out-Null
        try {
            $output=InvokeCodeSignTool @(
                'sign',
                "-username=$env:ES_USERNAME",
                "-password=$env:ES_PASSWORD",
                "-credential_id=$env:ES_CREDENTIAL_ID",
                "-totp_secret=$env:ES_TOTP_SECRET",
                '-program_name=Shixin Input',
                "-input_file_path=$path",
                "-output_dir_path=$stageOut"
            )
            $signed=Join-Path $stageOut (Split-Path $path -Leaf)
            if (-not (Test-Path $signed)) { throw "CodeSignTool sign produced no output for ${path}: $output" }
            & $SignTool verify /pa /all $signed | Out-Null; Check "verify $path"
            Move-Item $signed $path -Force
        } finally {
            Remove-Item $stageOut -Recurse -Force -ErrorAction SilentlyContinue
        }
        return
    }
    $options=@('sign','/sha1',$CertificateThumbprint,'/fd','SHA256','/s','My','/d','拾新输入法 Shixin Input','/du','https://github.com/shixin-labs/shixin-input')
    if ($CertificateStore -eq 'LocalMachine') { $options+='/sm' }
    if ($TimestampServer) { $options+=@('/tr',$TimestampServer,'/td','SHA256') }
    & $SignTool @options $path | Out-Null; Check "sign $path"
    & $SignTool verify /pa /all $path | Out-Null; Check "verify $path"
}
function SignBatch([IO.FileInfo[]]$files) {
    if ($UnsignedPreview -or $files.Count -eq 0) { return }
    if (-not $CodeSignTool) {
        foreach ($f in $files) { Sign $f.FullName }
        return
    }
    $stageIn=Join-Path $work 'sign-in'
    $stageOut=Join-Path $work 'sign-out'
    Remove-Item $stageIn,$stageOut -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force $stageIn,$stageOut | Out-Null
    try {
        $seen=@{}
        foreach ($f in $files) {
            $key=$f.Name.ToLowerInvariant()
            if ($seen.ContainsKey($key)) { throw "Duplicate filename in batch signing: $($f.Name)" }
            $seen[$key]=$f.FullName
            Copy-Item $f.FullName (Join-Path $stageIn $f.Name)
        }
        $output=InvokeCodeSignTool @(
            'batch_sign',
            "-username=$env:ES_USERNAME",
            "-password=$env:ES_PASSWORD",
            "-credential_id=$env:ES_CREDENTIAL_ID",
            "-totp_secret=$env:ES_TOTP_SECRET",
            '-program_name=Shixin Input',
            "-input_dir_path=$stageIn",
            "-output_dir_path=$stageOut"
        )
        foreach ($f in $files) {
            $signed=Join-Path $stageOut $f.Name
            if (-not (Test-Path $signed)) { throw "CodeSignTool batch_sign produced no output for $($f.Name): $output" }
            & $SignTool verify /pa /all $signed | Out-Null; Check "verify $($f.Name)"
            Copy-Item $signed $f.FullName -Force
        }
    } finally {
        Remove-Item $stageIn,$stageOut -Recurse -Force -ErrorAction SilentlyContinue
    }
}
function MicrosoftCRT([string]$name) {
    return $crtNames -contains $name
}
function VerifyMicrosoftCRT([string]$path) {
    $signature=Get-AuthenticodeSignature $path
    if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'O=Microsoft Corporation(?:,|$)') { throw "Invalid Microsoft runtime signature: $path" }
}
$savedOS=$env:GOOS; $savedArch=$env:GOARCH; $savedCGO=$env:CGO_ENABLED
Push-Location $root
try {
    if ((& go env GOHOSTARCH).Trim() -ne $Arch) { throw 'Build release packages on the matching native Windows architecture' }
    $env:GOOS='windows'; $env:GOARCH=$Arch; $env:CGO_ENABLED='0'
    $crtNames=@(& go run ./tools/windows-package -crt-files); Check 'list Microsoft runtime files'
    $suffix=if ($UnsignedPreview) {'-unsigned-preview'} else {''}
    $work=Join-Path $root "build/package-windows-$Arch$suffix"
    $payload=Join-Path $work 'payload'
    Remove-Item $payload -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force $payload | Out-Null
    Set-Content (Join-Path $root 'build/go.mod') 'module shixin/build' -Encoding ascii
    $flags="-s -w -X main.version=$Version"
    $buildOptions=@()
    if ($UnsignedPreview) { $buildOptions=@('-tags','shixin_unsigned_preview') }
    # One go build per flag set compiles and links its commands in parallel.
    & go build @buildOptions -trimpath -ldflags $flags -o "$payload/" ./cmd/shixin-engine ./cmd/shixin-builder ./cmd/shixin-ai
    Check 'build console commands'
    $gui=Join-Path $work 'gui'
    Remove-Item $gui -Recurse -Force -ErrorAction SilentlyContinue
    & go build @buildOptions -trimpath -ldflags "$flags -H windowsgui" -o "$gui/" ./cmd/shixin ./cmd/shixin-settings ./cmd/shixin-setup
    Check 'build windowed commands'
    foreach ($entry in @(@('shixin','Shixin'),@('shixin-settings','shixin-settings'),@('shixin-setup','ShixinSetup'))) {
        Move-Item "$gui/$($entry[0]).exe" (Join-Path $payload ($entry[1]+'.exe'))
    }
    $target=if ($Arch -eq 'arm64') {'ARM64'} else {'x64'}
    $native=Join-Path $root "build/tsf-$Arch"
    $fwdResource=Join-Path $work 'forwarder-version.res'
    & go run ./tools/windows-package -version-res $fwdResource -version $Version; Check 'write forwarder version resource'
    & cmake -S frontends/windows-tsf -B $native -A $target "-DSHIXIN_TSF_ARM64X=$($Arch -eq 'arm64' ? 'ON' : 'OFF')" "-DSHIXIN_TSF_VERSION_RESOURCE=$fwdResource" -DSHIXIN_TSF_TRACE=OFF; Check 'configure TSF'
    & cmake --build $native --config Release --parallel; Check 'build TSF'
    & ctest --test-dir $native -C Release --output-on-failure; Check 'test TSF'
    if ($Arch -eq 'arm64') {
        $x64=Join-Path $root 'build/tsf-amd64'
        & cmake -S frontends/windows-tsf -B $x64 -A x64 -DSHIXIN_TSF_TRACE=OFF; Check 'configure x64 TSF'
        & cmake --build $x64 --config Release --parallel; Check 'build x64 TSF'
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
    if (-not $Llama) {
        & "$root/scripts/build-llama.ps1" -Arch $Arch -Test
        $Llama=Join-Path $root "build/llama-windows-$Arch/lib"
    }
    foreach ($name in @('llama','ggml','ggml-base','ggml-cpu','ggml-vulkan')) {
        Copy-Item "$Llama/$name.dll" $payload
    }
    foreach ($file in (Get-ChildItem $Llama -File -Filter '*.dll' | Where-Object { MicrosoftCRT $_.Name })) {
        VerifyMicrosoftCRT $file.FullName
        Copy-Item $file.FullName $payload
    }
    & go run ./tools/windows-package -check-llama $payload -arch $Arch; Check 'verify llama DLL architectures and imports'
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
    # build-librime.ps1 and build-llama.ps1 stage their notices next to the
    # libraries, so packaging doesn't need the submodule sources.
    if (-not $LibrimeLicenses) { $LibrimeLicenses=Join-Path $root "build/librime-windows-$Arch/out/licenses" }
    if (-not (Test-Path "$LibrimeLicenses/librime (BSD-3-Clause).txt")) { throw 'Rebuild librime to stage its license notices' }
    if (-not (Test-Path "$Llama/licenses/llama.cpp (MIT).txt")) { throw 'Rebuild llama.cpp to stage its license notice' }
    Copy-Item "$LibrimeLicenses/*" $licenses
    Copy-Item "$Llama/licenses/llama.cpp (MIT).txt" "$licenses/llama.cpp (MIT).txt"
    Copy-Item "$Llama/licenses/Khronos-headers.txt" "$licenses/Khronos-headers.txt"
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
    # The Setup filename otherwise triggers Windows' installer heuristics.
    # Keep the bootstrapper in the original user's token; only MSI elevates.
    foreach ($exe in (Get-ChildItem $payload -File -Filter *.exe)) {Manifest $exe.FullName}
    $skipVersion=if ($Arch -eq 'arm64') {'shixin_tsf.dll'} else {''}
    & go run ./tools/windows-package -stamp-dir $payload -version $Version -skip $skipVersion; Check 'stamp release file versions'
    foreach ($file in (Get-ChildItem $payload -File -Filter '*.dll' | Where-Object { MicrosoftCRT $_.Name })) { VerifyMicrosoftCRT $file.FullName }
    & "$native/Release/shixin_dll_test.exe" "$payload/shixin_tsf.dll"; Check 'test stamped public TSF DLL'
    if ($Arch -eq 'arm64') { & "$x64/Release/shixin_dll_test.exe" "$payload/shixin_tsf.dll"; Check 'test stamped public TSF DLL from x64' }
    $toSign=@(Get-ChildItem $payload -Recurse -File | Where-Object { $_.Extension -in @('.exe','.dll') -and -not (MicrosoftCRT $_.Name) })
    SignBatch $toSign
    $msi=Join-Path $work 'Shixin.msi'
    & (Get-Process -Id $PID).Path -NoProfile -ExecutionPolicy Bypass -File "$root/packaging/windows/msi.ps1" -Payload $payload -Output $msi -Version $Version -Arch $Arch
    Check 'author MSI'
    Sign $msi
    $dist=Join-Path $root 'build/dist'
    if ($UnsignedPreview) { $dist=Join-Path $dist 'preview' }
    New-Item -ItemType Directory -Force $dist | Out-Null
    $setup=Join-Path $dist "ShixinSetup-$Version-$Arch$suffix.exe"
    & go build @buildOptions -trimpath -ldflags "$flags -H windowsgui" -o $setup ./cmd/shixin-setup; Check 'build bootstrapper'
    Manifest $setup
    & go run ./tools/windows-package -exe $setup -msi $msi -version $Version -arch $Arch; Check 'attach MSI'
    Sign $setup
    $hash=(Get-FileHash $setup -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $([IO.Path]::GetFileName($setup))" | Set-Content "$setup.sha256" -Encoding ascii
    Write-Host "Release installer: $setup"
} finally {
    if ($CodeSignTool) {
        Get-ChildItem (Join-Path $CodeSignTool 'logs') -File -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
    }
    $env:GOOS=$savedOS; $env:GOARCH=$savedArch; $env:CGO_ENABLED=$savedCGO
    Pop-Location
}
