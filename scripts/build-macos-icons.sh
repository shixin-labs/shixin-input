#!/usr/bin/env bash
# Render the original brand artwork and package native macOS icon resources.
# Usage: scripts/build-macos-icons.sh [OUTPUT] (default build/macos/icons)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/build/macos/icons}"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
cd "$ROOT"
CGO_ENABLED=0 go run ./packaging/macos/icons -output "$OUT"
iconutil -c icns "$OUT/Shixin.iconset" -o "$OUT/Shixin.icns"
# Store 1x and 2x representations at the same logical 16-point size.
for scale in 1 2; do
  size=$((16 * scale))
  dpi=$((72 * scale))
  sips -s format tiff -s dpiWidth "$dpi" -s dpiHeight "$dpi" \
    "$OUT/InputSource-$size.png" --out "$OUT/InputSource-$size.tiff" >/dev/null
done
# -cathidpicheck resets both DPIs to 72 on macOS 26; preserve them so
# AppKit sees two representations of 16 points, rather than 16 and 32.
tiffutil -catnosizecheck "$OUT/InputSource-16.tiff" "$OUT/InputSource-32.tiff" \
  -out "$OUT/InputSource.tiff" >/dev/null
echo "built icons in $OUT"
