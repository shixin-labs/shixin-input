#!/usr/bin/env bash
# Packages Shixin.app for release (DESIGN §5.5) into build/dist/Shixin.dmg:
# builds it with our librime, signs the app and the disk image with
# SIGN_IDENTITY (a Developer ID Application identity; required) and, with
# NOTARY_PROFILE (a notarytool keychain profile, from `xcrun notarytool
# store-credentials`), notarizes and staples the disk image so Gatekeeper
# opens it without Privacy & Security → Open Anyway.
#
# The disk image holds only Shixin.app, with no Applications link: opening
# the app installs it into ~/Library/Input Methods (or upgrades it), wherever
# it runs from.
# Usage: SIGN_IDENTITY="Developer ID Application: …" [NOTARY_PROFILE=…] [VERSION=…] scripts/package-macos.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
: "${SIGN_IDENTITY:?set SIGN_IDENTITY to a Developer ID Application identity}"
APP="$ROOT/build/macos/Shixin.app"
DIST="$ROOT/build/dist"
DMG="$DIST/Shixin.dmg"
export SIGN_IDENTITY VERSION="${VERSION:-0.1.0-dev}"
LIBRIME="${LIBRIME:-$ROOT/build/librime/out/lib/librime.1.dylib}"
[ -f "$LIBRIME" ] || { echo "missing $LIBRIME; run scripts/build-librime.sh" >&2; exit 1; }
LIBRIME="$LIBRIME" "$ROOT/scripts/bundle-macos.sh" "$APP"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/shixin-dmg.XXXXXX")"
MNT="$WORK/mnt"
trap 'hdiutil detach -quiet "$MNT" 2>/dev/null || true; rm -rf "$WORK"' EXIT
mkdir -p "$WORK/src" "$MNT"
ditto "$APP" "$WORK/src/Shixin.app"
# Writable first, to give the volume Shixin's icon, then compressed.
hdiutil create -quiet -fs APFS -volname "拾新 Shixin" -srcfolder "$WORK/src" -format UDRW "$WORK/rw.dmg"
hdiutil attach -quiet -nobrowse -noautoopen -mountpoint "$MNT" "$WORK/rw.dmg"
cp "$ROOT/build/macos/icons/Shixin.icns" "$MNT/.VolumeIcon.icns"
SetFile -a C "$MNT"
rm -rf "$MNT/.fseventsd"
hdiutil detach -quiet "$MNT"
mkdir -p "$DIST"
rm -f "$DMG"
hdiutil convert -quiet "$WORK/rw.dmg" -format ULFO -o "$DMG"
codesign --force --timestamp --sign "$SIGN_IDENTITY" "$DMG"
if [ -n "${NOTARY_PROFILE:-}" ]; then
  # Notarizing the disk image notarizes the app inside it too.
  xcrun notarytool submit "$DMG" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$DMG"
  spctl --assess --type open --context context:primary-signature --verbose "$DMG"
else
  echo "not notarized (no NOTARY_PROFILE): Gatekeeper will ask for Open Anyway" >&2
fi
echo "packaged $DMG ($VERSION)"
