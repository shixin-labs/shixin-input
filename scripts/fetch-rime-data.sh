#!/usr/bin/env bash
# Downloads the default schema set (rime-ice / 雾凇拼音, self-contained) into
# build/rime-data. That directory is used as Rime's *shared* data dir in
# development builds and is copied into the app bundle / installer for release.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/build/rime-data"
DL="$ROOT/build/downloads"
RIME_ICE_URL="${RIME_ICE_URL:-https://github.com/iDvel/rime-ice/releases/download/nightly/full.zip}"

mkdir -p "$DL"
rm -rf "$OUT" && mkdir -p "$OUT"

echo "==> Fetching rime-ice"
curl -fL --retry 3 -o "$DL/rime-ice-full.zip" "$RIME_ICE_URL"
unzip -q -o "$DL/rime-ice-full.zip" -d "$OUT"

# Shixin never runs Lua (DESIGN §3.8); drop rime-ice's scripts entirely.
rm -rf "$OUT/lua"

echo "==> Rime data ready in $OUT"
ls "$OUT" | head -40
