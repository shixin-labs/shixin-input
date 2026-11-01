#!/usr/bin/env bash
# Windows 11 ARM64 test VM: QEMU with Apple's hypervisor (HVF) and UEFI.
# No TPM: QEMU 11.1's tpm-tis-device fails under HVF (HV_BAD_ARGUMENT), so
# setup bypasses checks via HKLM\SYSTEM\Setup\LabConfig. See docs/windows.md.
# Display/input: VNC 127.0.0.1:5907; guest SSH: 127.0.0.1:2222 by default.
set -euo pipefail
VMROOT="${SHIXIN_VMROOT:-$HOME/shixin-vm}"
fail() { echo "windows-qemu: $*" >&2; exit 1; }
usage() {
  echo 'usage: windows-qemu.sh start|stop|status|shot NAME|keys KEY...|type TEXT|click X Y' >&2
  exit 2
}
[[ -d "$VMROOT" ]] || fail "VM root does not exist: $VMROOT (set SHIXIN_VMROOT)"
VMROOT="$(cd "$VMROOT" && pwd -P)"
D="$VMROOT/qemu/shixin-win11"
ISO="$VMROOT/iso"
SSH_PORT="${SHIXIN_WINDOWS_SSH_PORT:-2222}"
vnc() { "$VMROOT/shixin/shixin-win11/venv/bin/vncdo" -s 127.0.0.1::5907 "$@"; }
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
  FW="$(brew --prefix qemu)/share/qemu/edk2-aarch64-code.fd"
  WINDOWS_ISO="$ISO/${SHIXIN_WINDOWS_ISO:-Windows11_ARM64.iso}"
  TOOLS_ISO="$ISO/${SHIXIN_WINDOWS_TOOLS_ISO:-utm-guest-tools.iso}"
  for file in "$FW" "$WINDOWS_ISO" "$TOOLS_ISO"; do
    [[ -f "$file" ]] || fail "required file missing: $file (see docs/windows.md)"
  done
  mkdir -p "$D"
  [[ -f "$D/disk.qcow2" ]] || qemu-img create -q -f qcow2 "$D/disk.qcow2" 64G
  [[ -f "$D/efivars.fd" ]] || dd if=/dev/zero of="$D/efivars.fd" bs=1m count=64 2>/dev/null
  qemu-system-aarch64 -name shixin-win11 -M virt,highmem=on -accel hvf -cpu host -smp 4 -m "${MEM:-2048}" \
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
    -vnc 127.0.0.1:7 -monitor unix:"$D/monitor.sock",server,nowait -display none \
    -daemonize -pidfile "$D/qemu.pid" -D "$D/qemu.log"
  echo 'started (VNC 127.0.0.1:5907)'
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
