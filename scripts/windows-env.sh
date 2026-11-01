#!/usr/bin/env bash
# Shared defaults for windows-qemu.sh and windows-vm.sh; source after fail().
# Environment (empty values use defaults):
# HOME: current user's home; PATH: tool discovery.
# SHIXIN_VMROOT: $HOME/shixin-vm (must exist).
# SHIXIN_WINDOWS_VM: shixin-win11; SHIXIN_WINDOWS_USER: shixin.
# SHIXIN_WINDOWS_SSH_PORT: 2222; SHIXIN_WINDOWS_VNC_PORT: 5907 (loopback).
# SHIXIN_WINDOWS_CPUS: 4; SHIXIN_WINDOWS_MEM: MEM, then 2048 MiB.
# MEM: legacy RAM alias; SHIXIN_WINDOWS_DISK: 64G (new disks only).
# SHIXIN_WINDOWS_ISO: Windows11_ARM64.iso; SHIXIN_WINDOWS_TOOLS_ISO:
# utm-guest-tools.iso (absolute paths or filenames under $SHIXIN_VMROOT/iso).
# SHIXIN_WINDOWS_VNCDO: $SHIXIN_VMROOT/shixin/$SHIXIN_WINDOWS_VM/venv/bin/vncdo.
# SHIXIN_WINDOWS_KEY_DELAY: 50 ms; QEMU_FIRMWARE:
# $(brew --prefix qemu)/share/qemu/edk2-aarch64-code.fd (discovered on start).
# These variables are consumed by the scripts sourcing this file.
# shellcheck disable=SC2034

VMROOT="${SHIXIN_VMROOT:-$HOME/shixin-vm}"
NAME="${SHIXIN_WINDOWS_VM:-shixin-win11}"
SSH_PORT="${SHIXIN_WINDOWS_SSH_PORT:-2222}"
VNC_PORT="${SHIXIN_WINDOWS_VNC_PORT:-5907}"
CPUS="${SHIXIN_WINDOWS_CPUS:-4}"
MEMORY="${SHIXIN_WINDOWS_MEM:-${MEM:-2048}}"
DISK="${SHIXIN_WINDOWS_DISK:-64G}"
GUEST_USER="${SHIXIN_WINDOWS_USER:-shixin}"
KEY_DELAY="${SHIXIN_WINDOWS_KEY_DELAY:-50}"
FW="${QEMU_FIRMWARE:-}"
[[ -d "$VMROOT" ]] || fail "VM root does not exist: $VMROOT (set SHIXIN_VMROOT)"
VMROOT="$(cd "$VMROOT" && pwd -P)"
case "$NAME" in ''|*[!a-zA-Z0-9_-]*) fail 'SHIXIN_WINDOWS_VM must contain only letters, digits, underscores and hyphens' ;; esac
[[ $SSH_PORT =~ ^[1-9][0-9]{0,4}$ && $SSH_PORT -le 65535 ]] || fail 'SHIXIN_WINDOWS_SSH_PORT must be 1..65535'
[[ $VNC_PORT =~ ^[1-9][0-9]{0,4}$ && $VNC_PORT -ge 5900 && $VNC_PORT -le 65535 ]] || fail 'SHIXIN_WINDOWS_VNC_PORT must be 5900..65535'
[[ $CPUS =~ ^[1-9][0-9]*$ ]] || fail 'SHIXIN_WINDOWS_CPUS must be a positive integer'
[[ $MEMORY =~ ^[1-9][0-9]*$ ]] || fail 'SHIXIN_WINDOWS_MEM (or MEM) must be a positive integer in MiB'
[[ $KEY_DELAY =~ ^[0-9]+$ ]] || fail 'SHIXIN_WINDOWS_KEY_DELAY must be nonnegative milliseconds'
D="$VMROOT/qemu/$NAME"
S="$VMROOT/shixin/$NAME"
VNCDO="${SHIXIN_WINDOWS_VNCDO:-$S/venv/bin/vncdo}"
WINDOWS_ISO="${SHIXIN_WINDOWS_ISO:-Windows11_ARM64.iso}"
TOOLS_ISO="${SHIXIN_WINDOWS_TOOLS_ISO:-utm-guest-tools.iso}"
[[ $WINDOWS_ISO = /* ]] || WINDOWS_ISO="$VMROOT/iso/$WINDOWS_ISO"
[[ $TOOLS_ISO = /* ]] || TOOLS_ISO="$VMROOT/iso/$TOOLS_ISO"
VNC_DISPLAY="$((VNC_PORT - 5900))"
