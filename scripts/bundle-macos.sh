#!/usr/bin/env bash
# Builds Shixin.app (DESIGN §5.5):
#   Contents/MacOS/Shixin                   the IMK front (cmd/shixin)
#   Contents/Helpers/shixin-engine          the engine (cmd/shixin-engine)
#   Contents/Helpers/shixin-builder         offline config/dictionary compiler
#   Contents/Helpers/shixin-ai              智能排序's scorer (DESIGN §7.2)
#   Contents/Helpers/Shixin Settings.app    the settings app
#   Contents/Resources/rime-data            rime-ice, without its Lua and its
#                                           dictionary sources, plus OpenCC's
#                                           data from our librime build
#   Contents/Resources/rime-prebuilt        compiled dictionaries
#   Contents/Resources/{Shixin.icns,InputSource.tiff}  app and input icons
#   Contents/Frameworks/librime.1.dylib     our librime, once built (LIBRIME=)
#   Contents/Frameworks/lib{llama,ggml*}    llama.cpp for shixin-ai, if built
#                                           (scripts/build-llama.sh; LLAMA=)
#   Contents/Resources/licenses             third-party licenses (About shows them)
# Usage: scripts/bundle-macos.sh [OUT.app]   (default build/macos/Shixin.app)
# VERSION and BUILD override the version strings. SIGN_IDENTITY (a Developer
# ID Application identity) signs with the hardened runtime; else ad hoc.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="${1:-$ROOT/build/macos/Shixin.app}"
VERSION="${VERSION:-0.1.0-dev}"
BUILD="${BUILD:-$(date +%Y%m%d%H%M%S)}"
DATA="$ROOT/build/rime-data"
[ -f "$DATA/rime_ice.schema.yaml" ] || { echo "missing $DATA; run scripts/fetch-rime-data.sh" >&2; exit 1; }

rm -rf "$APP"
C="$APP/Contents"
mkdir -p "$C/MacOS" "$C/Helpers" "$C/Resources" "$C/Frameworks"
LDFLAGS="-s -w -X main.version=$VERSION"
cd "$ROOT"
CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$C/MacOS/Shixin" ./cmd/shixin
CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$C/Helpers/shixin-engine" ./cmd/shixin-engine
CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$C/Helpers/shixin-builder" ./cmd/shixin-builder
CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$C/Helpers/shixin-ai" ./cmd/shixin-ai
LLAMA="${LLAMA:-$ROOT/build/llama/lib}"
mkdir -p "$C/Resources/licenses"
if [ -f "$LLAMA/libllama.dylib" ]; then
  # Only the names they link each other by, as plain files: symlinks in a
  # bundle break copies (the VM's shared folder) and signing.
  for n in llama ggml ggml-base ggml-cpu ggml-metal; do
    cp -L "$LLAMA/lib$n.0.dylib" "$C/Frameworks/"
  done
  cp third_party/llama.cpp/LICENSE "$C/Resources/licenses/llama.cpp (MIT).txt"
  cp third_party/llama.cpp/licenses/LICENSE-jsonhpp "$C/Resources/licenses/nlohmann-json (MIT).txt"
else
  echo "no llama.cpp in $LLAMA: 智能排序 will be unavailable (scripts/build-llama.sh)" >&2
fi

S="$C/Helpers/Shixin Settings.app/Contents"
mkdir -p "$S/MacOS" "$S/Resources"
CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$S/MacOS/shixin-settings" ./cmd/shixin-settings
sed "s/@VERSION@/$VERSION/; s/@BUILD@/$BUILD/" packaging/macos/settings/Info.plist >"$S/Info.plist"

sed "s/@VERSION@/$VERSION/; s/@BUILD@/$BUILD/" packaging/macos/Info.plist >"$C/Info.plist"
cp -R packaging/macos/zh-Hans.lproj packaging/macos/en.lproj "$C/Resources/"
cp -R packaging/macos/settings/zh-Hans.lproj packaging/macos/settings/en.lproj "$S/Resources/"
"$ROOT/scripts/build-macos-icons.sh" "$ROOT/build/macos/icons"
cp "$ROOT/build/macos/icons/Shixin.icns" "$C/Resources/"
cp "$ROOT/build/macos/icons/Shixin.icns" "$S/Resources/"
cp "$ROOT/build/macos/icons/InputSource.tiff" "$C/Resources/"
rsync -a --exclude lua --exclude '*.custom.yaml' --exclude 'weasel.yaml' --exclude 'squirrel.yaml' "$DATA/" "$C/Resources/rime-data/"
if [ -n "${LIBRIME:-}" ]; then
  cp "$LIBRIME" "$C/Frameworks/librime.1.dylib"
  for l in third_party/librime/LICENSE build/librime/src/LICENSE; do
    [ -f "$l" ] && cp "$l" "$C/Resources/licenses/librime (BSD-3-Clause).txt" && break
  done
  # OpenCC's data for 简繁, from the same build (build-librime.sh).
  OPENCC="$(dirname "$LIBRIME")/../share/opencc"
  [ -f "$OPENCC/s2t.json" ] || { echo "missing $OPENCC/s2t.json; rerun scripts/build-librime.sh" >&2; exit 1; }
  rsync -a "$OPENCC/" "$C/Resources/rime-data/opencc/"
fi
# Compile every supported schema in an isolated scratch directory. Ship
# only immutable binary dictionaries/prisms; runtime config still comes
# from the user's settings. No live Rime user directory is ever opened.
SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/shixin-prebuilt.XXXXXX")"
trap 'rm -rf "$SCRATCH"' EXIT
BUILDER_LIB="${LIBRIME:-/opt/homebrew/opt/librime/lib/librime.1.dylib}"
"$C/Helpers/shixin-builder" -lib "$BUILDER_LIB" -data "$C/Resources/rime-data" -all -output "$SCRATCH/generation"
mkdir -p "$C/Resources/rime-prebuilt"
find "$SCRATCH/generation/build" -maxdepth 1 -name '*.bin' -exec cp {} "$C/Resources/rime-prebuilt/" \;
# librime reuses a prebuilt table when its dictionary source is missing,
# so ship the tables instead of the sources (46 MB of YAML).
for d in "$C/Resources/rime-data"/*.dict.yaml; do
  t="$C/Resources/rime-prebuilt/$(basename "$d" .dict.yaml).table.bin"
  [ -f "$t" ] || { echo "no prebuilt table for $d" >&2; exit 1; }
done
find "$C/Resources/rime-data" -name '*.dict.yaml' -delete
find "$C/Resources/rime-data" -type d -empty -delete
if grep -rl "lua_" "$C/Resources/rime-data"/*.custom.yaml 2>/dev/null; then
  echo "lua_ in bundled patches" >&2; exit 1
fi

if [ -n "${SIGN_IDENTITY:-}" ]; then
  # Developer ID with the hardened runtime, inside out, as notarization
  # requires (scripts/package-macos.sh). No entitlements: nothing JITs,
  # and every library we load is signed by the same team.
  sign() { codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$@"; }
  for f in "$C"/Frameworks/*.dylib "$C"/Helpers/shixin-engine "$C"/Helpers/shixin-builder "$C"/Helpers/shixin-ai; do
    [ -e "$f" ] && sign "$f"
  done
  sign "$C/Helpers/Shixin Settings.app"
  sign "$APP"
  codesign --verify --deep --strict "$APP"
else
  codesign --force --deep --sign - "$APP" 2>&1 | grep -v 'replacing existing signature' || true
fi
echo "built $APP ($VERSION, build $BUILD)"
