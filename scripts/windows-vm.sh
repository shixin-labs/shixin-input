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
  -o StrictHostKeyChecking=accept-new -o "UserKnownHostsFile=\"$S/known_hosts\""
  -i "$KEY")
run_vm() {
  [[ -x "$RUN" ]] || fail "QEMU controller missing: $RUN (see docs/windows.md)"
  # A long burst can overrun the guest USB keyboard's report queue and
  # leave a key repeating. Keep setup input paced even on the 2 GiB guest.
  VNCDOTOOL_DELAY="${SHIXIN_WINDOWS_KEY_DELAY:-50}" "$RUN" "$@"
}
need_ssh() { [[ -f "$KEY" ]] || fail 'run init-state and configure guest OpenSSH key authentication first'; }
type_text() {
  # QEMU's USB keyboard maps shifted punctuation keysyms to their base key.
  # Send explicit Shift chords, while keeping ordinary text in large chunks.
  local remaining="$1" chunk='' ch chord LC_ALL=C
  [[ $remaining != *[![:print:]]* ]] || fail 'type accepts printable US ASCII; use keys for Enter and Tab'
  while [[ -n $remaining ]]; do
    ch="${remaining:0:1}"
    remaining="${remaining:1}"
    chord=''
    case "$ch" in
      '~') chord='shift-`' ;; '!') chord='shift-1' ;; '@') chord='shift-2' ;;
      '#') chord='shift-3' ;; '$') chord='shift-4' ;; '%') chord='shift-5' ;;
      '^') chord='shift-6' ;; '&') chord='shift-7' ;; '*') chord='shift-8' ;;
      '(') chord='shift-9' ;; ')') chord='shift-0' ;; '_') chord='shift-minus' ;;
      '+') chord='shift-=' ;; '{') chord='shift-[' ;; '}') chord='shift-]' ;;
      '|') chord='shift-bslash' ;; ':') chord='shift-;' ;; '"') chord="shift-'" ;;
      '<') chord='shift-,' ;; '>') chord='shift-.' ;; '?') chord='shift-/' ;;
    esac
    if [[ -n $chord ]]; then
      if [[ -n $chunk ]]; then run_vm type "$chunk"; chunk=''; fi
      run_vm keys "$chord"
    else
      chunk+="$ch"
    fi
  done
  if [[ -n $chunk ]]; then run_vm type "$chunk"; fi
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
type) [[ $# -eq 2 ]] || usage; type_text "$2" ;;
click)
  [[ $# -eq 3 && $2 =~ ^[0-9]+$ && $3 =~ ^[0-9]+$ ]] || usage
  run_vm click "$2" "$3"
  ;;
*) usage ;;
esac
