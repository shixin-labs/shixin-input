#!/usr/bin/env bash
# Windows 11 ARM64 test VM: QEMU with Apple's hypervisor (HVF) and UEFI.
# No TPM: QEMU 11.1's tpm-tis-device fails under HVF (HV_BAD_ARGUMENT), so
# setup bypasses checks via HKLM\SYSTEM\Setup\LabConfig. See docs/windows.md.
# Environment (defaults; shared in windows-env.sh): HOME (user home), PATH
# (tool discovery), SHIXIN_VMROOT ($HOME/shixin-vm), SHIXIN_WINDOWS_VM
# (shixin-win11), SHIXIN_WINDOWS_USER (shixin), SHIXIN_WINDOWS_SSH_PORT (2222),
# SHIXIN_WINDOWS_VNC_PORT (5907), SHIXIN_WINDOWS_CPUS (4), SHIXIN_WINDOWS_MEM
# (MEM or 2048 MiB), MEM (legacy alias), SHIXIN_WINDOWS_DISK (64G),
# SHIXIN_WINDOWS_ISO (Windows11_ARM64.iso), SHIXIN_WINDOWS_TOOLS_ISO
# (utm-guest-tools.iso); ISO paths are absolute or relative to $VMROOT/iso.
# SHIXIN_WINDOWS_VNCDO ($VMROOT/shixin/$SHIXIN_WINDOWS_VM/venv/bin/vncdo),
# SHIXIN_WINDOWS_KEY_DELAY (50 ms), QEMU_FIRMWARE
# ($(brew --prefix qemu)/share/qemu/edk2-aarch64-code.fd; discovered on start).
# SSH/VNC bind to loopback. VNC port minus 5900 is the QEMU display number.
set -euo pipefail
fail() { echo "windows-qemu: $*" >&2; exit 1; }
# shellcheck source=windows-env.sh
# shellcheck source-path=SCRIPTDIR
source "$(dirname "$0")/windows-env.sh"
usage() {
  echo 'usage: windows-qemu.sh start|stop|status|shot NAME|keys KEY...|type TEXT|click X Y' >&2
  exit 2
}
# Keep the connection alive long enough to flush the final key/button release.
# vncdotool inserts delays between commands, but not after a final key/click.
vnc() { VNCDOTOOL_DELAY="$KEY_DELAY" "$VNCDO" -s "127.0.0.1::$VNC_PORT" "$@" pause 0.1; }
read_pid() {
  local pid
  pid="$(cat "$D/qemu.pid" 2>/dev/null)" || return 1
  [[ $pid =~ ^[1-9][0-9]*$ ]] || return 1
  printf '%s' "$pid"
}
case "${1:-status}" in
start)
  [[ $# -eq 1 ]] || usage
  if pid="$(read_pid)" && kill -0 "$pid" 2>/dev/null; then
    fail 'VM is already running'
  fi
  if [[ -z $FW ]]; then
    command -v brew >/dev/null || fail 'set QEMU_FIRMWARE to an edk2-aarch64-code.fd file, or install Homebrew QEMU'
    prefix="$(brew --prefix qemu)" || fail 'cannot discover QEMU firmware; set QEMU_FIRMWARE'
    FW="$prefix/share/qemu/edk2-aarch64-code.fd"
  fi
  [[ -f "$FW" ]] || fail "QEMU firmware missing: $FW (set QEMU_FIRMWARE)"
  for file in "$WINDOWS_ISO" "$TOOLS_ISO"; do
    [[ -f "$file" ]] || fail "required file missing: $file (see docs/windows.md)"
  done
  mkdir -p "$D"
  [[ -f "$D/disk.qcow2" ]] || qemu-img create -q -f qcow2 "$D/disk.qcow2" "$DISK"
  [[ -f "$D/efivars.fd" ]] || dd if=/dev/zero of="$D/efivars.fd" bs=1m count=64 2>/dev/null
  qemu-system-aarch64 -name "$NAME" -M virt,highmem=on -accel hvf -cpu host -smp "$CPUS" -m "$MEMORY" \
    -drive if=pflash,format=raw,readonly=on,file="$FW" \
    -drive if=pflash,format=raw,file="$D/efivars.fd" \
    -device ramfb -device qemu-xhci,id=xhci -device usb-kbd -device usb-tablet \
    -drive file="$D/disk.qcow2",if=none,id=disk,format=qcow2,discard=unmap \
    -device nvme,serial=shixin0,drive=disk,bootindex=1 \
    -drive file="$WINDOWS_ISO",if=none,id=cd0,media=cdrom,readonly=on \
    -device usb-storage,drive=cd0,removable=on,bootindex=0 \
    -drive file="$TOOLS_ISO",if=none,id=cd1,media=cdrom,readonly=on \
    -device usb-storage,drive=cd1,removable=on \
    -netdev user,id=net0,hostfwd=tcp:127.0.0.1:"$SSH_PORT"-:22 -device virtio-net-pci,netdev=net0 \
    -vnc "127.0.0.1:$VNC_DISPLAY" -monitor unix:"$D/monitor.sock",server,nowait -display none \
    -daemonize -pidfile "$D/qemu.pid" -D "$D/qemu.log"
  echo "started $NAME (VNC 127.0.0.1:$VNC_PORT)"
  ;;
stop)
  [[ $# -eq 1 ]] || usage
  if pid="$(read_pid)" && kill -0 "$pid" 2>/dev/null; then
    if [[ -S "$D/monitor.sock" ]]; then
      printf 'system_powerdown\n' | nc -U -w 2 "$D/monitor.sock" >/dev/null || true
    fi
    for ((i = 0; i < 60; i++)); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 1
    done
    kill "$pid" 2>/dev/null || true
  fi
  echo stopped
  ;;
status)
  [[ $# -le 1 ]] || usage
  if pid="$(read_pid)" && kill -0 "$pid" 2>/dev/null; then echo running; else echo stopped; fi
  ;;
shot)
  [[ $# -eq 2 ]] || usage
  case "$2" in ''|*[!a-zA-Z0-9_-]*) fail 'screenshot name may contain letters, digits, underscores and hyphens' ;; esac
  mkdir -p "$D/shots"
  vnc capture "$D/shots/$2.png"
  echo "$D/shots/$2.png"
  ;;
keys) shift; [[ $# -gt 0 ]] || usage; for key in "$@"; do vnc key "$key"; done ;;
type) [[ $# -eq 2 ]] || usage; vnc type "$2" ;;
click)
  [[ $# -eq 3 && $2 =~ ^[0-9]+$ && $3 =~ ^[0-9]+$ ]] || usage
  vnc move "$2" "$3" click 1
  ;;
*) usage ;;
esac
