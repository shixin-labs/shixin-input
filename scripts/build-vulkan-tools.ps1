# Build a minimal Vulkan SDK from pinned source. This installs no driver and
# runs no downloaded executables; glslc is built with the native MSVC compiler.
[CmdletBinding()]
param(
    [ValidateSet('amd64','arm64')][string]$Arch='amd64',
    [ValidateRange(1,2147483647)][int]$Jobs=[Environment]::ProcessorCount
)
$ErrorActionPreference='Stop'
if (-not $IsWindows) { throw 'Vulkan build tools require Windows and PowerShell 7' }
$root=Split-Path $PSScriptRoot -Parent
$work=Join-Path $root "build/vulkan-tools-$Arch"
$sdk=Join-Path $work 'sdk'
$source=Join-Path $work 'source'
$target=if ($Arch -eq 'arm64') {'ARM64'} else {'x64'}
if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() -ne $target) { throw 'Build Vulkan tools on the matching native architecture' }
function Check([string]$step) { if ($LASTEXITCODE -ne 0) { throw "$step failed (exit $LASTEXITCODE)" } }
function Source([string]$dir,[string]$repo,[string]$revision) {
    if (-not (Test-Path "$dir/.git")) {
        New-Item -ItemType Directory -Force $dir | Out-Null
        & git -C $dir init; Check 'initialize source checkout'
        & git -C $dir config core.longpaths true; Check 'enable long source paths'
        & git -C $dir remote add origin $repo; Check 'set source remote'
        & git -C $dir fetch --depth 1 origin $revision; Check 'fetch pinned source'
        & git -C $dir checkout --detach FETCH_HEAD; Check 'check out pinned source'
    }
    $actual=(& git -C $dir rev-parse HEAD).Trim(); Check 'read source revision'
    if ($actual -ne $revision) { throw "Unexpected source revision in $dir" }
    if (& git -C $dir status --porcelain --untracked-files=no) { throw "Modified source in $dir" }
}
New-Item -ItemType Directory -Force $work | Out-Null
Set-Content "$root/build/go.mod" 'module shixin/build' -Encoding ascii
$shaderc=Join-Path $source 'shaderc'
Source $shaderc 'https://github.com/google/shaderc.git' '73743588fe9c39f2f1c780a087d94afac691a189'
# The required compiler dependencies match shaderc's DEPS at that revision.
Source "$shaderc/third_party/glslang" 'https://github.com/KhronosGroup/glslang.git' 'd213562e35573012b6348b2d584457c3704ac09b'
Source "$shaderc/third_party/spirv-tools" 'https://github.com/KhronosGroup/SPIRV-Tools.git' '19042c8921f35f7bec56b9e5c96c5f5691588ca8'
Source "$shaderc/third_party/spirv-headers" 'https://github.com/KhronosGroup/SPIRV-Headers.git' '01e0577914a75a2569c846778c2f93aa8e6feddd'
Source "$source/headers" 'https://github.com/KhronosGroup/Vulkan-Headers.git' '19725e4d48082fe78e26622b15d3080ccd54112b'
Source "$source/loader" 'https://github.com/KhronosGroup/Vulkan-Loader.git' '0a278cc725089cb67bf6027076e5d72f97c04d86'
$out=Join-Path $work 'shaderc'
& cmake -S $shaderc -B $out -A $target -DCMAKE_BUILD_TYPE=Release -DSHADERC_SKIP_TESTS=ON -DSHADERC_SKIP_EXAMPLES=ON -DSHADERC_SKIP_COPYRIGHT_CHECK=ON -DSHADERC_ENABLE_SHARED_CRT=OFF -DSPIRV_SKIP_EXECUTABLES=ON -DENABLE_GLSLANG_BINARIES=OFF
Check 'configure shader compiler'
& cmake --build $out --config Release --target glslc_exe --parallel $Jobs
Check 'build shader compiler'
New-Item -ItemType Directory -Force "$sdk/Bin","$sdk/Include","$sdk/Lib" | Out-Null
Copy-Item "$out/glslc/Release/glslc.exe" "$sdk/Bin/glslc.exe" -Force
Copy-Item "$source/headers/include/*" "$sdk/Include" -Recurse -Force
& cmake -S "$shaderc/third_party/spirv-headers" -B "$work/spirv-headers" -A $target "-DCMAKE_INSTALL_PREFIX=$sdk"
Check 'configure SPIR-V headers'
& cmake --build "$work/spirv-headers" --config Release --target install
Check 'install SPIR-V headers'
# An import library is sufficient for linking. The end user's Vulkan loader
# comes from their GPU driver; this SDK never ships a replacement loader DLL.
$linkerLine=Get-Content "$out/CMakeCache.txt" | Where-Object { $_ -match '^CMAKE_LINKER:FILEPATH=' } | Select-Object -First 1
if (-not $linkerLine) { throw 'CMake did not record the MSVC linker' }
$linker=$linkerLine.Substring($linkerLine.IndexOf('=')+1)
$lib=if ($Arch -eq 'arm64') {"$sdk/Lib-ARM64"} else {"$sdk/Lib"}
New-Item -ItemType Directory -Force $lib | Out-Null
& $linker /lib /nologo "/machine:$target" "/def:$source/loader/loader/vulkan-1.def" "/out:$lib/vulkan-1.lib"
Check 'create Vulkan import library from official exports'
& "$sdk/Bin/glslc.exe" --version; Check 'run source-built shader compiler'
Write-Host "Vulkan build SDK: $sdk"
