#!/usr/bin/env bash
# Drive only the external-SSD Windows test VM. See docs/windows.md.
set -euo pipefail
VMROOT="${SHIXIN_WINDOWS_VMROOT:-/Volumes/Samsung SSD 990 Pro 2TB/vm}"
UTMCTL="${SHIXIN_UTMCTL:-/opt/homebrew/bin/utmctl}"
fail() { echo "windows-vm: $*" >&2; exit 1; }
usage() {
  echo 'usage: windows-vm.sh init-state|boot|stop|force-stop|status|exec POWERSHELL|put LOCAL GUEST|get GUEST LOCAL|shot NAME|vnc ARGS' >&2
  exit 2
}
[[ $# -gt 0 ]] || usage
# Resolve symlinks before creating anything: an absent SSD must never make
# mkdir silently create VM storage on the internal disk.
[[ -d "$VMROOT" ]] || fail "mount the external SSD first: $VMROOT"
VMROOT="$(cd "$VMROOT" && pwd -P)"
case "$VMROOT" in /Volumes/*/vm) ;; *) fail 'VM root must be an existing /Volumes/<external disk>/vm directory' ;; esac
S="$VMROOT/shixin/shixin-win11"
BUNDLE="$VMROOT/utm/shixin-win11.utm"
KEY="$S/id_ed25519"
SSH_PORT="${SHIXIN_WINDOWS_SSH_PORT:-22222}"
VNC_PORT="${SHIXIN_WINDOWS_VNC_PORT:-5905}"
GUEST_USER="${SHIXIN_WINDOWS_USER:-shixin}"
VNC_DO="${SHIXIN_WINDOWS_VNCDO:-$S/venv/bin/vncdo}"
ssh_options=(-o BatchMode=yes -o IdentitiesOnly=yes -o ConnectTimeout=10
  -o StrictHostKeyChecking=accept-new -o "UserKnownHostsFile=$S/known_hosts"
  -i "$KEY")
vm_id() {
  [[ -f "$BUNDLE/config.plist" ]] || fail "VM bundle missing: $BUNDLE"
  local uuid
  uuid="$(/usr/libexec/PlistBuddy -c 'Print :Information:UUID' "$BUNDLE/config.plist")" || return
  [[ $uuid =~ ^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$ ]] || fail 'invalid VM UUID in config.plist'
  printf '%s\n' "$uuid"
}
need_ssh() { [[ -f "$KEY" ]] || fail 'run init-state and configure guest OpenSSH key authentication first'; }
vnc() {
  [[ -x "$VNC_DO" ]] || fail "vncdotool missing: $VNC_DO (see docs/windows.md)"
  "$VNC_DO" -s "127.0.0.1::$VNC_PORT" "$@"
}
case "$1" in
init-state)
  [[ $# -eq 1 ]] || usage
  umask 077
  mkdir -p "$S"/{shots,logs,notes,downloads}
  [[ -f "$KEY" ]] || ssh-keygen -q -t ed25519 -N '' -C shixin-win11-vm -f "$KEY"
  echo "State: $S"
  echo "Guest authorized key: $KEY.pub"
  ;;
boot) [[ $# -eq 1 ]] || usage; id="$(vm_id)"; "$UTMCTL" start "$id" ;;
stop) [[ $# -eq 1 ]] || usage; id="$(vm_id)"; "$UTMCTL" stop "$id" --request ;;
force-stop) [[ $# -eq 1 ]] || usage; id="$(vm_id)"; "$UTMCTL" stop "$id" --force ;;
status) [[ $# -eq 1 ]] || usage; id="$(vm_id)"; "$UTMCTL" status "$id" ;;
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
  vnc capture "$S/shots/$2.png"
  echo "$S/shots/$2.png"
  ;;
vnc) shift; [[ $# -gt 0 ]] || usage; vnc "$@" ;;
*) usage ;;
esac
