#!/usr/bin/env bash
# Drive only the external-SSD Windows test VM. See docs/windows.md.
set -euo pipefail
VMROOT="${SHIXIN_WINDOWS_VMROOT:-${SHIXIN_VMROOT:-/Volumes/Samsung SSD 990 Pro 2TB/vm}}"

fail() { echo "windows-vm: $*" >&2; exit 1; }
usage() {
  echo 'usage: windows-vm.sh init-state|boot|stop|status|exec POWERSHELL|put LOCAL GUEST|get GUEST LOCAL|shot NAME|keys KEY...|type TEXT|click X Y' >&2
  exit 2
}
[[ $# -gt 0 ]] || usage
# Resolve symlinks before creating anything: an absent SSD must never make
# mkdir silently create VM storage on the internal disk.
[[ -d "$VMROOT" ]] || fail "mount the external SSD first: $VMROOT"
VMROOT="$(cd "$VMROOT" && pwd -P)"
case "$VMROOT" in /Volumes/*/vm) ;; *) fail 'VM root must be an existing /Volumes/<external disk>/vm directory' ;; esac
S="$VMROOT/shixin/shixin-win11"
VM="$VMROOT/qemu/shixin-win11"
RUN="$VM/run.sh"
KEY="$S/id_ed25519"
SSH_PORT="${SHIXIN_WINDOWS_SSH_PORT:-2222}"
GUEST_USER="${SHIXIN_WINDOWS_USER:-shixin}"
ssh_options=(-o BatchMode=yes -o IdentitiesOnly=yes -o ConnectTimeout=10
  -o StrictHostKeyChecking=accept-new -o "UserKnownHostsFile=$S/known_hosts"
  -i "$KEY")
run_vm() {
  [[ -x "$RUN" ]] || fail "QEMU controller missing: $RUN (see docs/windows.md)"
  "$RUN" "$@"
}
need_ssh() { [[ -f "$KEY" ]] || fail 'run init-state and configure guest OpenSSH key authentication first'; }
case "$1" in
init-state)
  [[ $# -eq 1 ]] || usage
  umask 077
  mkdir -p "$S"/{shots,logs,notes,downloads}
  [[ -f "$KEY" ]] || ssh-keygen -q -t ed25519 -N '' -C shixin-win11-vm -f "$KEY"
  echo "State: $S"
  echo "Guest authorized key: $KEY.pub"
  ;;
boot) [[ $# -eq 1 ]] || usage; run_vm start ;;
stop) [[ $# -eq 1 ]] || usage; run_vm stop ;;
status) [[ $# -eq 1 ]] || usage; run_vm status ;;
exec)
  [[ $# -eq 2 ]] || usage
  need_ssh
  # Pass literal PowerShell as UTF-16LE, without cmd.exe quote expansion.
  encoded="$(printf '%s' "$2" | iconv -f UTF-8 -t UTF-16LE | base64 | tr -d '\r\n')"
  ssh "${ssh_options[@]}" -p "$SSH_PORT" "$GUEST_USER@127.0.0.1" \
    "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand $encoded"
  ;;
put)
  [[ $# -eq 3 ]] || usage
  need_ssh
  scp "${ssh_options[@]}" -P "$SSH_PORT" "$2" "$GUEST_USER@127.0.0.1:$3"
  ;;
get)
  [[ $# -eq 3 ]] || usage
  need_ssh
  # Collected guest artifacts must remain on the external SSD too.
  parent="$(cd "$(dirname "$3")" && pwd -P)"
  case "$parent/" in "$S/"*) ;; *) fail "save guest files under $S" ;; esac
  scp "${ssh_options[@]}" -P "$SSH_PORT" "$GUEST_USER@127.0.0.1:$2" "$3"
  ;;
shot)
  [[ $# -eq 2 ]] || usage
  case "$2" in ''|*[!a-zA-Z0-9_-]*) fail 'screenshot name may contain letters, digits, underscores and hyphens' ;; esac
  mkdir -p "$S/shots"
  run_vm shot "$2"
  cp "$VM/shots/$2.png" "$S/shots/$2.png"
  echo "$S/shots/$2.png"
  ;;
keys) shift; [[ $# -gt 0 ]] || usage; run_vm keys "$@" ;;
type) [[ $# -eq 2 ]] || usage; run_vm type "$2" ;;
click)
  [[ $# -eq 3 && $2 =~ ^[0-9]+$ && $3 =~ ^[0-9]+$ ]] || usage
  run_vm click "$2" "$3"
  ;;
*) usage ;;
esac
