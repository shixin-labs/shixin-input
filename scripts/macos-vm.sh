#!/usr/bin/env bash
# Drives Shixin.app in the shixin-test Tart VM (DESIGN §9, GUI tests).
# Set SHIXIN_VMROOT (below) first.
#   macos-vm.sh boot      run the VM (foreground; start it in the background)
#   macos-vm.sh vncenv    read the VNC URL from run.log into vnc.env
#   macos-vm.sh build     bundle into a fresh build/macos/vm/<id>/ (the VM's
#                         shared-folder view of a recreated directory can be stale)
#   macos-vm.sh upgrade   build, swap it into the installed bundle in place and
#                         restart, without registering (DESIGN §5.5)
#   macos-vm.sh logs      tail the front and engine logs
#   macos-vm.sh burst     type 14 × "jintiantianqizhenhao " into TextEdit fast
#   macos-vm.sh input ARGS native keys: key cmd+a | type '>:"' | expect TEXT
#   macos-vm.sh shot NAME capture the screen to $S/shots/NAME.png
#   macos-vm.sh vnc ARGS  raw vncdotool commands
#   macos-vm.sh exec CMD  run a shell command in the guest
# Variables in single-quoted guest commands expand inside the VM.
# shellcheck disable=SC2016
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# SHIXIN_VMROOT holds Tart's data (tart/), optionally Tart itself
# (bin/tart.app), and the VM's working files (shixin/shixin-test: vnc.env,
# run.log, the vncdotool venv, shots/).
VMROOT="${SHIXIN_VMROOT:-$HOME/shixin-vm}"
export PATH="$VMROOT/bin/tart.app/Contents/MacOS:$PATH" TART_HOME="$VMROOT/tart"
S="$VMROOT/shixin/shixin-test"
SHARED="/Volumes/My Shared Files/shixin"
# shellcheck source=/dev/null
[ -f "$S/vnc.env" ] && source "$S/vnc.env"
vd() { (cd "$S" && venv/bin/vncdo -s "127.0.0.1::$VNC_PORT" -p "$VNC_PW" "$@" 2>/dev/null); }
gx() { tart exec shixin-test sh -c "$1"; }

native_input() {
  local stage tool guest_source remote arg
  mkdir -p "$ROOT/build/macos/vm-input"
  # virtio-fs can retain stale entries for a replaced executable. Give each
  # transfer a fresh host path, as the app-bundle builds do.
  stage=$(mktemp -d "$ROOT/build/macos/vm-input/command.XXXXXX")
  tool="$stage/shixin-vm-input"
  guest_source="$SHARED/build/macos/vm-input/$(basename "$stage")/shixin-vm-input"
  (cd "$ROOT" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$tool" ./tools/vm-input)
  codesign --force --sign - --identifier im.shixin.vm-input "$tool" >/dev/null 2>&1
  # Keep a stable guest path for Accessibility. Do not replace an identical
  # executable: repeated commands retain its existing authorization.
  gx "set -e
mkdir -p \"\$HOME/Library/Caches/ShixinTest\"
if ! cmp -s '$guest_source' \"\$HOME/Library/Caches/ShixinTest/shixin-vm-input\"; then
  cp '$guest_source' \"\$HOME/Library/Caches/ShixinTest/shixin-vm-input\"
fi"
  rm -rf "$stage"
  remote='exec "$HOME/Library/Caches/ShixinTest/shixin-vm-input"'
  for arg in "$@"; do
    # POSIX single-quote escaping preserves spaces, quotes, shell symbols
    # and trailing newlines without evaluating any of the input as a shell.
    arg=${arg//\'/\'\\\'\'}
    remote+=" '$arg'"
  done
  gx "$remote"
}

case "${1:-}" in
boot)
  exec tart run shixin-test --no-graphics --vnc-experimental --dir="shixin:$ROOT:ro" >"$S/run.log" 2>&1
  ;;
vncenv)
  url=$(grep -o 'vnc://[^ ]*' "$S/run.log" | tail -1)
  printf 'VNC_PW=%s\nVNC_PORT=%s\n' "$(sed -E 's#vnc://:([^@]*)@.*#\1#' <<<"$url")" "$(sed -E 's#.*:([0-9]+)$#\1#' <<<"$url")" >"$S/vnc.env"
  cat "$S/vnc.env"
  ;;
build)
  id=$(date +%Y%m%d-%H%M%S)
  lib="$ROOT/build/librime/out/lib/librime.1.dylib"
  [ -f "$lib" ] || lib=""
  LIBRIME="$lib" BUILD="$id" "$ROOT/scripts/bundle-macos.sh" "$ROOT/build/macos/vm/$id/Shixin.app"
  echo "build/macos/vm/$id" >"$ROOT/build/macos/vm/latest"
  ;;
upgrade)
  "$0" build
  D=$(cat "$ROOT/build/macos/vm/latest")
  gx "set -e
IM=\"\$HOME/Library/Input Methods\"; ST=\"\$HOME/Library/Caches/ShixinUpgrade\"
rm -rf \"\$ST\"; mkdir -p \"\$ST\"
cp -R '$SHARED/$D/Shixin.app' \"\$ST/\"
\"\$ST/Shixin.app/Contents/MacOS/Shixin\" -upgrade-into \"\$IM/Shixin.app\"
pkill -x Shixin || true
for i in \$(seq 1 100); do pgrep -x Shixin >/dev/null || break; sleep 0.05; done
open -g \"\$IM/Shixin.app\" --args -after-upgrade
rm -rf \"\$ST\"
\"\$IM/Shixin.app/Contents/MacOS/Shixin\" -forget"
  sleep 3
  gx 'pgrep -lf "Shixin|shixin-engine"; tail -3 "$HOME/Library/Logs/Shixin/shixin.log"'
  ;;
logs)
  gx 'tail -n ${N:-15} "$HOME/Library/Logs/Shixin/shixin.log"; echo ---; tail -n ${N:-15} "$HOME/Library/Logs/Shixin/engine.log"'
  ;;
burst)
  # With Shixin active in Chinese mode, use native shortcuts rather than
  # screen-coordinate menu clicks. The sentinel prevents a stale clipboard
  # from satisfying the exact-text assertion if Cmd+C fails.
  gx 'open -a TextEdit'; sleep 1
  gx 'printf %s shixin-burst-pending | pbcopy'
  args=(key escape key cmd+n wait 500ms); want=""
  for _ in $(seq 1 14); do
    args+=(type jintiantianqizhenhao key space)
    want+="今天天气真好"
  done
  native_input "${args[@]}" wait 500ms key cmd+a key cmd+c expect "$want"
  echo "burst: exact text matched (84 characters, 14 phrases)"
  gx 'grep latency: "$HOME/Library/Logs/Shixin/shixin.log" | tail -1'
  ;;
input)
  shift
  native_input "$@"
  ;;
shot) mkdir -p "$S/shots"; vd capture "$S/shots/$2.png"; echo "$S/shots/$2.png" ;;
vnc) shift; vd "$@" ;;
exec) shift; gx "$*" ;;
*) echo "usage: macos-vm.sh boot|vncenv|build|upgrade|logs|burst|input ARGS|shot NAME|vnc ARGS|exec CMD" >&2; exit 2 ;;
esac
