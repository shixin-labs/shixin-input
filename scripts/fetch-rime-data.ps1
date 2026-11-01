# Windows equivalent of fetch-rime-data.sh. Downloads stay under build/.
# Arguments: URL (rime-ice nightly full.zip release on GitHub, below).
# No script-specific environment variables; paths derive from the repository.
[CmdletBinding()]
param(
    [string]$URL = 'https://github.com/iDvel/rime-ice/releases/download/nightly/full.zip'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$downloads = Join-Path $root 'build/downloads'
$out = Join-Path $root 'build/rime-data'
New-Item -ItemType Directory -Force $downloads | Out-Null
Set-Content -Path (Join-Path $root 'build/go.mod') -Value 'module shixin/build' -Encoding ascii
$archive = Join-Path $downloads 'rime-ice-full.zip'
Invoke-WebRequest -Uri $URL -OutFile $archive
if (Test-Path $out) { Remove-Item -Recurse -Force $out }
Expand-Archive -LiteralPath $archive -DestinationPath $out
# Shixin replaces rime-ice's Lua components with native librime-shixin.
$lua = Join-Path $out 'lua'
if (Test-Path $lua) { Remove-Item -Recurse -Force $lua }
Write-Host "Rime data ready in $out"
