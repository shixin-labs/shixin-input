# Build our pinned librime with static dependencies and merged native Shixin
# components. Run in PowerShell 7 on Windows with Visual Studio C++, CMake,
# Git, tar and Python on PATH. This never opens a user dictionary.
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'arm64',
    [switch]$Test
)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'This script requires Windows' }
$root = Split-Path $PSScriptRoot -Parent
$source = Join-Path $root 'third_party/librime'
$work = Join-Path $root "build/librime-windows-$Arch"
$scratch = Join-Path $work 'src'
$prefix = Join-Path $work 'deps'
$out = Join-Path $work 'out'
$downloads = Join-Path $root 'build/downloads'
$target = if ($Arch -eq 'arm64') { 'ARM64' } else { 'x64' }
$savedProgress = $ProgressPreference
$savedPath = $env:Path
function Check-Native([string]$step) {
    if ($LASTEXITCODE -ne 0) { throw "$step failed (exit $LASTEXITCODE)" }
}
Push-Location $root
try {
    $ProgressPreference = 'SilentlyContinue'
    New-Item -ItemType Directory -Force $downloads, $work | Out-Null
    Set-Content -Path (Join-Path $root 'build/go.mod') -Value 'module shixin/build' -Encoding ascii
    & git submodule update --init --recursive third_party/librime
    Check-Native 'initialize pinned librime'

    # Keep generated files and the plugin copy out of the source submodule.
    $copyOutput = & robocopy $source $scratch /MIR /XD .git deps build dist /XF .git /NFL /NDL /NJH /NJS /NP
    if ($LASTEXITCODE -ge 8) { throw "copy librime failed (exit $LASTEXITCODE): $copyOutput" }
    # Only Shixin may contribute a merged plugin, even if the source tree
    # happens to contain another plugin from a previous local experiment.
    Get-ChildItem (Join-Path $scratch 'plugins') -Directory | Remove-Item -Recurse -Force
    $plugin = Join-Path $scratch 'plugins/shixin'
    Copy-Item (Join-Path $root 'plugins/librime-shixin') $plugin -Recurse

    $boostArchive = Join-Path $downloads 'boost_1_89_0.tar.gz'
    if (-not (Test-Path $boostArchive)) {
        Invoke-WebRequest -Uri 'https://archives.boost.io/release/1.89.0/source/boost_1_89_0.tar.gz' -OutFile $boostArchive
    }
    $boostHash = '9de758db755e8330a01d995b0a24d09798048400ac25c03fc5ea9be364b13c93'
    if ((Get-FileHash $boostArchive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $boostHash) {
        throw 'Boost archive SHA-256 mismatch'
    }
    $boost = Join-Path $downloads 'boost_1_89_0'
    if (-not (Test-Path (Join-Path $boost 'boost/version.hpp'))) {
        # Windows librime only needs Boost headers. Avoid unpacking its
        # examples/docs and their long paths on Windows.
        & tar.exe -xzf $boostArchive -C $downloads 'boost_1_89_0/boost'
        Check-Native 'extract Boost'
    }

    # Match upstream build.bat's static CRT for every dependency and librime.
    $common = @('-G', 'Visual Studio 17 2022', '-A', $target,
        '-DCMAKE_POLICY_VERSION_MINIMUM=3.5',
        '-DCMAKE_CONFIGURATION_TYPES=Release', '-DCMAKE_BUILD_TYPE=Release',
        '-DCMAKE_CXX_FLAGS_INIT=/utf-8',
        '-DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded',
        "-DCMAKE_USER_MAKE_RULES_OVERRIDE=$source/cmake/c_flag_overrides.cmake",
        "-DCMAKE_USER_MAKE_RULES_OVERRIDE_CXX=$source/cmake/cxx_flag_overrides.cmake")
    $dependencies = @(
        @{ Name = 'glog'; Flags = @('-DBUILD_TESTING=OFF', '-DWITH_GFLAGS=OFF') },
        @{ Name = 'leveldb'; Flags = @('-DLEVELDB_BUILD_TESTS=OFF', '-DLEVELDB_BUILD_BENCHMARKS=OFF') },
        @{ Name = 'yaml-cpp'; Flags = @('-DYAML_CPP_BUILD_TESTS=OFF', '-DYAML_CPP_BUILD_TOOLS=OFF',
            '-DYAML_CPP_BUILD_CONTRIB=OFF', '-DMSVC_SHARED_RT=OFF', '-DYAML_MSVC_SHARED_RT=OFF') },
        @{ Name = 'googletest'; Flags = @('-DBUILD_GMOCK=OFF', '-DINSTALL_GTEST=ON') },
        @{ Name = 'marisa-trie'; Flags = @('-DBUILD_TESTING=OFF', '-DENABLE_TOOLS=OFF') },
        @{ Name = 'opencc'; Flags = @('-DBUILD_TESTING=OFF') }
    )
    foreach ($dependency in $dependencies) {
        $name = $dependency.Name
        $build = Join-Path $work "build/$name"
        $options = $common + @('-DBUILD_SHARED_LIBS=OFF', "-DCMAKE_INSTALL_PREFIX=$prefix") + $dependency.Flags
        & cmake -S (Join-Path $source "deps/$name") -B $build @options
        Check-Native "configure $name"
        & cmake --build $build --config Release --target install --parallel 4
        Check-Native "build $name"
    }

    $build = Join-Path $work 'build/rime'
    $options = $common + @('-DBUILD_STATIC=ON', '-DBUILD_SHARED_LIBS=ON',
        '-DBUILD_MERGED_PLUGINS=ON', '-DENABLE_EXTERNAL_PLUGINS=OFF',
        # The recovery tests exercise internal C++ classes without RIME_DLL
        # annotations. Export their methods in this development DLL too.
        '-DCMAKE_WINDOWS_EXPORT_ALL_SYMBOLS=ON',
        # CURRENT metadata requires LF bytes. The upstream test fixtures use
        # ofstream's default mode; MSVC otherwise translates LF to CRLF.
        # binmode.obj changes defaults only in these development executables.
        '-DCMAKE_EXE_LINKER_FLAGS=binmode.obj',
        '-DBUILD_TEST=ON', '-DCMAKE_DISABLE_FIND_PACKAGE_Gflags=ON',
        "-DBOOST_ROOT=$boost", "-DCMAKE_PREFIX_PATH=$prefix", "-DCMAKE_INSTALL_PREFIX=$out")
    & cmake -S $scratch -B $build @options
    Check-Native 'configure librime'
    & cmake --build $build --config Release --target install --parallel 4
    Check-Native 'build librime'

    $dll = Get-ChildItem $out -Recurse -File -Filter rime.dll | Select-Object -First 1
    if (-not $dll) { throw 'rime.dll was not installed' }
    $destination = Join-Path $root "build/windows-$Arch"
    New-Item -ItemType Directory -Force $destination | Out-Null
    Copy-Item $dll.FullName $destination -Force
    $opencc = Join-Path $prefix 'share/opencc'
    $openccOut = Join-Path $out 'share/opencc'
    New-Item -ItemType Directory -Force $openccOut | Out-Null
    Copy-Item (Join-Path $opencc '*') $openccOut -Recurse -Force
    if ($Test) {
        $env:Path = $dll.DirectoryName + ';' + $savedPath
        & ctest --test-dir $build -C Release --output-on-failure
        Check-Native 'librime and Shixin native tests'
    }
    Write-Host "Built $($dll.FullName); copied to $destination"
    Write-Host "Merge $out/share/opencc into the runtime data/opencc directory."
} finally {
    $ProgressPreference = $savedProgress
    $env:Path = $savedPath
    Pop-Location
}
