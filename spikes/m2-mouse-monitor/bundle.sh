#!/usr/bin/env bash
# Builds MouseMonitor.app (LSUIElement) into build/spike/m2-mouse-monitor.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
APP="$ROOT/build/spike/m2-mouse-monitor/MouseMonitor.app"
rm -rf "$APP" && mkdir -p "$APP/Contents/MacOS"
(cd "$ROOT" && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o "$APP/Contents/MacOS/MouseMonitor" ./spikes/m2-mouse-monitor)
cat >"$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>dev.shixin.spike.mousemonitor</string>
<key>CFBundleExecutable</key><string>MouseMonitor</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSUIElement</key><true/>
</dict></plist>
PLIST
codesign -s - --force "$APP" 2>/dev/null || true
echo "$APP"
