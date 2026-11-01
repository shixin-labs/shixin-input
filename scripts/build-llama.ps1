# Build the pinned llama.cpp for Windows 智能排序. Requires PowerShell 7,
# Visual Studio C++, CMake and an installed Vulkan SDK (ARM64 components on
# ARM64). No SDK installers or executable downloads are run by this script.
# -NoVulkan is for CI/VM tests only: that build cannot score on the CPU.
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',
    [switch]$NoVulkan,
    [switch]$Test,
    [switch]$RequireAccelerator,
    [ValidateRange(1, 2147483647)]
    [int]$Jobs = [Environment]::ProcessorCount
)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'This script requires Windows' }
if ($RequireAccelerator -and (-not $Test -or $NoVulkan)) { throw '-RequireAccelerator requires -Test and a Vulkan build' }
if (-not $NoVulkan -and -not $env:VULKAN_SDK) { throw 'Install the Vulkan SDK and set VULKAN_SDK (include its ARM64 components for ARM64)' }
$root = Split-Path $PSScriptRoot -Parent
$source = Join-Path $root 'third_party/llama.cpp'
$variant = if ($NoVulkan) { '-no-vulkan' } else { '' }
$work = Join-Path $root "build/llama-windows-$Arch$variant"
$lib = Join-Path $work 'lib'
$out = Join-Path $work 'out'
$target = if ($Arch -eq 'arm64') { 'ARM64' } else { 'x64' }
function Check([string]$step) {
    if ($LASTEXITCODE -ne 0) { throw "$step failed (exit $LASTEXITCODE)" }
}
$savedOS=$env:GOOS; $savedArch=$env:GOARCH; $savedCGO=$env:CGO_ENABLED
$savedLlama=$env:SHIXIN_LLAMA; $savedGPU=$env:SHIXIN_TEST_REQUIRE_ACCELERATOR
$savedNoGPU=$env:SHIXIN_TEST_NO_ACCELERATOR
Push-Location $root
try {
    if (-not (Test-Path "$source/CMakeLists.txt")) {
        & git submodule update --init --depth 1 third_party/llama.cpp; Check 'initialize pinned llama.cpp'
    }
    New-Item -ItemType Directory -Force $work | Out-Null
    Set-Content (Join-Path $root 'build/go.mod') 'module shixin/build' -Encoding ascii
    $vulkan = if ($NoVulkan) { 'OFF' } else { 'ON' }
    $options = @('-A', $target, '-DCMAKE_CONFIGURATION_TYPES=Release', '-DCMAKE_BUILD_TYPE=Release',
        # ggml_fopen returns FILE* across DLLs. A separate /MT CRT in each
        # DLL invalidates its descriptor table; all modules must share /MD.
        '-DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreadedDLL', '-DBUILD_SHARED_LIBS=ON',
        '-DLLAMA_BUILD_COMMON=OFF', '-DLLAMA_BUILD_TESTS=OFF', '-DLLAMA_BUILD_TOOLS=OFF',
        '-DLLAMA_BUILD_EXAMPLES=OFF', '-DLLAMA_BUILD_SERVER=OFF', '-DLLAMA_BUILD_APP=OFF',
        '-DLLAMA_BUILD_UI=OFF', '-DLLAMA_OPENSSL=OFF', '-DLLAMA_BUILD_HTML=OFF',
        '-DGGML_BACKEND_DL=ON', "-DGGML_VULKAN=$vulkan", '-DGGML_METAL=OFF',
        '-DGGML_OPENMP=OFF', '-DGGML_BLAS=OFF', '-DGGML_RPC=OFF', '-DGGML_CUDA=OFF',
        '-DGGML_OPENCL=OFF', '-DGGML_CPU_KLEIDIAI=OFF', '-DGGML_CCACHE=OFF',
        '-DGGML_NATIVE=OFF', '-DGGML_AVX=OFF', '-DGGML_AVX2=OFF', '-DGGML_BMI2=OFF',
        '-DGGML_FMA=OFF', '-DGGML_F16C=OFF')
    if ($Arch -eq 'arm64') {
        # This pinned version requires NATIVE for MSVC ARM. Fix its feature
        # probes to the ARMv8 baseline instead of specializing for the builder.
        $options += @('-DGGML_NATIVE=ON', '-DGGML_MACHINE_SUPPORTS_dotprod=OFF',
            '-DGGML_MACHINE_SUPPORTS_i8mm=OFF', '-DGGML_MACHINE_SUPPORTS_sve=OFF',
            '-DGGML_MACHINE_SUPPORTS_sme=OFF', '-DGGML_MACHINE_SUPPORTS_fma=ON')
        if (-not $NoVulkan) {
            $vkLib = Join-Path $env:VULKAN_SDK 'Lib-ARM64/vulkan-1.lib'
            if (-not (Test-Path $vkLib)) { $vkLib = Join-Path $env:VULKAN_SDK 'Lib/vulkan-1.lib' }
            $options += "-DVulkan_LIBRARY=$vkLib"
        }
    }
    & cmake -S $source -B $out @options; Check 'configure llama.cpp'
    $targets = @('llama', 'ggml-cpu')
    if (-not $NoVulkan) { $targets += 'ggml-vulkan' }
    & cmake --build $out --config Release --target @targets --parallel $Jobs; Check 'build llama.cpp'
    # Recreate only the owned staging directory; never leave stale backends.
    Remove-Item $lib -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory $lib | Out-Null
    $names = @('llama', 'ggml', 'ggml-base', 'ggml-cpu')
    if (-not $NoVulkan) { $names += 'ggml-vulkan' }
    foreach ($name in $names) { Copy-Item "$out/bin/Release/$name.dll" $lib }
    # Stage app-local release CRTs from this compiler's redistributable tree.
    # Keep Microsoft's signatures/versions; these are never Shixin-stamped.
    # Visual Studio generators record CXX in compiler metadata, not the cache.
    $compilerInfo = Get-ChildItem "$out/CMakeFiles/*/CMakeCXXCompiler.cmake" | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if (-not $compilerInfo) { throw 'CMake did not record the selected C++ compiler' }
    $compilerMatch = [regex]::Match((Get-Content $compilerInfo.FullName -Raw), '(?m)^set\(CMAKE_CXX_COMPILER "([^"]+)"\)')
    if (-not $compilerMatch.Success) { throw 'Cannot read the selected C++ compiler path' }
    $compiler = $compilerMatch.Groups[1].Value
    $vc = Split-Path $compiler -Parent
    while ($vc -and -not (Test-Path "$vc/Redist/MSVC")) { $vc = Split-Path $vc -Parent }
    if (-not $vc) { throw 'Cannot locate the compiler Visual C++ redistributables' }
    $redistVersion = (Get-Content "$vc/Auxiliary/Build/Microsoft.VCRedistVersion.default.txt").Trim()
    $crtArch = if ($Arch -eq 'arm64') { 'arm64' } else { 'x64' }
    $crt = "$vc/Redist/MSVC/$redistVersion/$crtArch/Microsoft.VC143.CRT"
    if (-not (Test-Path $crt)) { throw "Install the Visual C++ $crtArch release redistributables" }
    $crtFiles = Get-ChildItem $crt -File -Filter '*.dll'
    $machine = if ($Arch -eq 'arm64') { 0xaa64 } else { 0x8664 }
    foreach ($file in $crtFiles) {
        # Microsoft's ARM64 redist also contains the x64-only exception DLL
        # for emulated applications; the native scorer must not stage it.
        $bytes=[IO.File]::ReadAllBytes($file.FullName)
        $pe=[BitConverter]::ToInt32($bytes,0x3c)
        if ([BitConverter]::ToUInt16($bytes,$pe+4) -eq $machine) { Copy-Item $file.FullName $lib }
    }
    if (-not $NoVulkan) {
        New-Item -ItemType Directory "$lib/licenses" | Out-Null
        $notice = "$lib/licenses/Khronos-headers.txt"
        Copy-Item "$root/packaging/windows/Khronos-headers.txt" $notice
        # Retain the actual SDK headers' copyright notices as SDKs advance.
        $copyrights = foreach ($headers in @('vulkan','spirv')) {
            $include = Join-Path $env:VULKAN_SDK "Include/$headers"
            if (Test-Path $include) {
                Get-ChildItem $include -Recurse -File | Where-Object Extension -In @('.h','.hpp','.hxx') | ForEach-Object {
                    Get-Content $_.FullName -TotalCount 15 | Where-Object { $_ -match '(?i)copyright' }
                }
            }
        }
        Add-Content $notice "`nCopyright notices from the build SDK headers:`n" -Encoding utf8
        $copyrights | Sort-Object -Unique | Add-Content $notice -Encoding utf8
        $development = Join-Path $root "build/windows-$Arch"
        New-Item -ItemType Directory -Force $development | Out-Null
        Copy-Item "$lib/*.dll" $development -Force
    }
    if ($Test) {
        $env:GOOS='windows'; $env:GOARCH=$Arch; $env:CGO_ENABLED='0'
        $env:SHIXIN_LLAMA=$lib
        $env:SHIXIN_TEST_REQUIRE_ACCELERATOR=if ($RequireAccelerator) { '1' } else { '' }
        $env:SHIXIN_TEST_NO_ACCELERATOR=if ($NoVulkan) { '1' } else { '' }
        $testOptions = @('-count=1', '-p=1') # GPU latency tests must run alone.
        if (-not $RequireAccelerator) { $testOptions += '-short' }
        & go test @testOptions ./internal/llama ./internal/ai ./internal/rerank ./cmd/shixin-ai
        Check 'Windows binding, scorer transport and no-CPU fallback tests'
    }
    Write-Host "Built Windows llama.cpp ($Arch) in $lib"
} finally {
    $env:GOOS=$savedOS; $env:GOARCH=$savedArch; $env:CGO_ENABLED=$savedCGO
    $env:SHIXIN_LLAMA=$savedLlama; $env:SHIXIN_TEST_REQUIRE_ACCELERATOR=$savedGPU
    $env:SHIXIN_TEST_NO_ACCELERATOR=$savedNoGPU
    Pop-Location
}
