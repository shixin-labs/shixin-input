#!/usr/bin/env bash
# Packages Shixin.app for sharing (DESIGN §5.5) into build/dist/Shixin.zip:
# builds it with our librime, signs it with SIGN_IDENTITY (a Developer ID
# Application identity; required) and, with NOTARY_PROFILE (a notarytool
# keychain profile, from `xcrun notarytool store-credentials`), notarizes
# and staples it so Gatekeeper opens it without Privacy & Security → Open
# Anyway.
# Usage: SIGN_IDENTITY="Developer ID Application: …" [NOTARY_PROFILE=…] scripts/package-macos.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
: "${SIGN_IDENTITY:?set SIGN_IDENTITY to a Developer ID Application identity}"
APP="$ROOT/build/macos/Shixin.app"
DIST="$ROOT/build/dist"
ZIP="$DIST/Shixin.zip"
export SIGN_IDENTITY
LIBRIME="${LIBRIME:-$ROOT/build/librime/out/lib/librime.1.dylib}"
[ -f "$LIBRIME" ] || { echo "missing $LIBRIME; run scripts/build-librime.sh" >&2; exit 1; }
LIBRIME="$LIBRIME" "$ROOT/scripts/bundle-macos.sh" "$APP"
mkdir -p "$DIST"
rm -f "$ZIP"
ditto -c -k --keepParent "$APP" "$ZIP"
if [ -n "${NOTARY_PROFILE:-}" ]; then
  xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$APP"
  rm -f "$ZIP"
  ditto -c -k --keepParent "$APP" "$ZIP"
  spctl --assess --type execute --verbose "$APP"
else
  echo "not notarized (no NOTARY_PROFILE): Gatekeeper will ask for Open Anyway" >&2
fi
echo "packaged $ZIP"
