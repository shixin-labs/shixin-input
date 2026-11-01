# Build Windows development binaries without installing or registering them.
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',
    [switch]$Check,
    [switch]$SkipNative
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$savedGOOS = $env:GOOS
$savedGOARCH = $env:GOARCH
$savedCGO = $env:CGO_ENABLED
Push-Location $root
try {
    $env:GOOS = 'windows'
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = '0'
    $out = Join-Path $root "build/windows-$Arch"
    New-Item -ItemType Directory -Force $out | Out-Null
    # Keep build artifacts outside the parent Go module's package walk.
    Set-Content -Path (Join-Path $root 'build/go.mod') -Value 'module shixin/build' -Encoding ascii
    foreach ($name in @('shixin-engine', 'shixin-builder', 'shixin-ai', 'shixin-settings')) {
        & go build -o (Join-Path $out "$name.exe") "./cmd/$name"
        if ($LASTEXITCODE -ne 0) { throw "build $name failed" }
    }
    & go build -o (Join-Path $out 'Shixin.exe') ./cmd/shixin
    if ($LASTEXITCODE -ne 0) { throw 'build Windows candidate host failed' }
    if (-not $SkipNative) {
        if (-not $IsWindows) { throw 'Native TSF builds require Windows and the Visual Studio C++ toolchain; use -SkipNative for Go cross-builds' }
        $target = if ($Arch -eq 'arm64') { 'ARM64' } else { 'x64' }
        $native = Join-Path $root "build/tsf-$Arch"
        & cmake -S frontends/windows-tsf -B $native -A $target
        if ($LASTEXITCODE -ne 0) { throw 'configure TSF failed' }
        & cmake --build $native --config Release
        if ($LASTEXITCODE -ne 0) { throw 'build TSF failed' }
        Copy-Item (Join-Path $native 'Release/shixin_tsf.dll') $out -Force
    }
    & go build -o (Join-Path $out 'menu-capture.exe') ./tools/menu-capture
    if ($LASTEXITCODE -ne 0) { throw 'build menu-capture failed' }
    if ($Check) {
        if (-not $IsWindows) { throw '-Check requires a Windows host' }
        $nativeArch = (& go env GOHOSTARCH).Trim()
        if ($Arch -ne $nativeArch) { throw "-Check requires the native architecture ($nativeArch)" }
        if ($SkipNative) { throw '-Check includes the native TSF tests; remove -SkipNative' }
        & ctest --test-dir $native -C Release --output-on-failure
        if ($LASTEXITCODE -ne 0) { throw 'native TSF tests failed' }
        & go vet ./...
        if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
        & go test -short -timeout 5m ./...
        if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
    }
    Write-Host "Built Windows development binaries ($Arch) in $out"
} finally {
    $env:GOOS = $savedGOOS
    $env:GOARCH = $savedGOARCH
    $env:CGO_ENABLED = $savedCGO
    Pop-Location
}
