# Windows development

Windows development builds include the C++ TSF DLL, a Go candidate/tray
host, the shared engine and settings GUI, and offline tools. Both x64
(`amd64`) and ARM64 (`arm64`) are build targets. This is an experimental
front: ARM64 native builds, librime, registration and Edge typing have been
tested in a Windows VM, with substantial app compatibility failures. See
the [GUI results](manual-tests.md#windows-arm64-gui-results-2026-10-31) and
[known issues](KNOWN-ISSUES.md#windows-validation-and-release-support).
There is no signed Windows installer or updater yet.

## Build and check

Use the Go version in `go.mod`, PowerShell 7, CMake, and Visual Studio with
its C++ desktop workload and the selected target architecture:

```powershell
./scripts/build-windows.ps1 -Arch amd64
./scripts/build-windows.ps1 -Arch arm64
# On a native Windows host of the selected architecture:
./scripts/build-windows.ps1 -Arch arm64 -Check
```

Outputs under `build/windows-<arch>/` include `Shixin.exe`,
`shixin_tsf.dll`, `shixin-engine.exe`, `shixin-builder.exe`,
`shixin-ai.exe`, `shixin-settings.exe`, and `menu-capture.exe`. The script
builds without installing or registering an input source. `-SkipNative`
builds only the Go binaries, including the host, for cross-compilation.
`-Check` runs the native client/COM tests, vet, and short Go tests; it never
calls registration exports or activates a connection to a live engine.
Both native architectures have CI jobs. Cross-compiling a test binary does
not execute it.

The portable C++ codec, policy, queue, and decision tests also run outside
Windows:

```sh
cmake -S frontends/windows-tsf -B build/tsf-tests
cmake --build build/tsf-tests
ctest --test-dir build/tsf-tests --output-on-failure
```

They check the Go protocol's existing golden vectors plus the appended
caret vector; malformed/truncated input; policy speculation; ordering and
ASK deadlines; stale replies, focus changes and secure guards; queue
exhaustion/concurrent traffic; zero allocations in client decisions; and
parser stress. `shixin_client_test` also reports decision latency against
a fake transport, which is not an end-to-end Windows latency measurement.
The Windows-only COM test loads the DLL and checks its factory, interface
identity, refcounts and disconnected key routing without activating or
registering the IME. The portable tests can be built with
`-DCMAKE_CXX_FLAGS='-fsanitize=address,undefined -fno-omit-frame-pointer'`.

## Native dependencies

Build the pinned librime with statically linked dependencies and merged
`librime-shixin`, with the external plugin loader disabled and no Lua:

```powershell
# Visual Studio 2022 C++, CMake, Git, tar and Python 3 on PATH; PowerShell 7.
./scripts/fetch-rime-data.ps1
./scripts/build-librime.ps1 -Arch arm64 -Test
$out = 'build/windows-arm64'
New-Item -ItemType Directory -Force "$out/data/opencc" | Out-Null
Copy-Item build/rime-data/* "$out/data" -Recurse -Force
Copy-Item build/librime-windows-arm64/out/share/opencc/* "$out/data/opencc" -Recurse -Force
```

Use `-Arch amd64` for x64. `build-librime.ps1` keeps the source submodule
clean, builds under `build/librime-windows-<arch>/`, pins and checks the
Boost archive, installs OpenCC conversion data, and copies `rime.dll` beside
the development executables. `-Test` runs librime and Shixin's C++ tests.
Python must allow imports from the script directory; a normal Python 3
installation works. An isolated embeddable distribution needs its `._pth`
configuration adjusted before OpenCC's sibling imports work.

The development DLL exports the internal methods needed by librime's
recovery tests. Its C++ development executables link
[`binmode.obj`](https://learn.microsoft.com/en-us/cpp/c-runtime-library/fmode?view=msvc-170)
so test fixtures write exact LF metadata. This fixes Windows test linkage
and line-ending behavior through build configuration. The pinned fork is
not edited. ARM64 builds and all C++ test targets passed in the VM.

Put any additional same-architecture native DLL dependencies beside the
executables, shared Rime data under `data/`, and optional compiled
schemas under `prebuilt/`. The verified ARM64 librime imports only Windows
system DLLs. The Go loader searches its library directory and System32,
excluding the working directory and PATH.

`shixin-ai.exe` looks for `llama.dll` and its dependencies next to itself;
`SHIXIN_LLAMA` can select another directory. Windows GPU backend behavior
and scoring latency remain unverified.

Settings and live user data default to `%APPDATA%\Shixin\settings.json`
and `%APPDATA%\Shixin\Rime`. Engine tests and tools must use scratch user
directories. The native COM smoke test never activates a text service,
so it cannot connect to an installed engine or open a live dictionary.

## Registration and manual use: Windows VM only

Use a disposable Windows VM snapshot. Supply the native dependencies above,
then register the DLL from its final development directory:

```powershell
./build/windows-amd64/Shixin.exe -register
./build/windows-amd64/Shixin.exe
```

The command invokes the adjacent DLL's standard registration exports.
COM registration is written under the current user's Classes key, and TSF
profile/category APIs register a Simplified Chinese keyboard profile.
Errors propagate to the caller. Registration at a different DLL path is
rejected while the existing class points elsewhere; unregister that copy
before moving it. The command does not silently elevate or change the
currently selected keyboard. Select 拾新 under Windows's language/input
settings, then run the application checklist. The tray opens the same
settings GUI as macOS, including a Windows first-run guide. The update
installer still needs a Windows implementation.

The current registration is **not per-user-only**: ordinary medium-integrity
registration failed in the VM, while an elevated command succeeded and
created a machine TIP entry. Use an administrator shell only inside the
disposable test VM if needed to continue runtime checks, then run the host
and applications as ordinary desktop processes. See the
[registration failure](KNOWN-ISSUES.md#windows-keyboard-registration-needs-elevation).

To remove the development profile in the VM:

```powershell
./build/windows-amd64/Shixin.exe -unregister
```

Close apps using the DLL before deleting its files. An ARM64 DLL supports
native ARM64 applications; the package strategy for x64 emulated apps,
which require a matching DLL, still needs validation. The build script does
not create logon startup entries; start `Shixin.exe` before typing tests.
The tested unregistration returned an error; its machine TIP was removed
only by an elevated attempt. Verify both user and machine state and preserve
diagnostics as described
in [known issues](KNOWN-ISSUES.md#windows-unregistration-returns-failure-after-removing-registration).

## Collecting development logs

The default Windows launch does not currently configure persistent logs
(see [known issues](KNOWN-ISSUES.md#windows-development-logs-are-not-persistent-by-default)).
For a fresh VM run, explicitly start the engine with a log path before
starting the host; preserve stderr from both processes as well:

```powershell
$out = (Resolve-Path ./build/windows-arm64).Path
$logs = Join-Path $env:LOCALAPPDATA 'Shixin/test-logs'
New-Item -ItemType Directory -Force $logs | Out-Null
Start-Process (Join-Path $out 'shixin-engine.exe') -ArgumentList @(
    '-log', ('"' + (Join-Path $logs 'engine.log') + '"')
) -RedirectStandardError (Join-Path $logs 'engine-stderr.log')
Start-Process (Join-Path $out 'Shixin.exe') `
    -RedirectStandardError (Join-Path $logs 'host-stderr.log')
```

Use this only when no engine is already running. Record the actual command
and paths for the run, then copy the files into the external VM state
`logs/` directory. These are explicit test destinations, not product default
paths. A later restart by the host's supervisor returns to the default
logging behavior; record that evidence gap during restart tests.

## Windows test VM on macOS

Use QEMU directly with HVF and an external-volume controller at
`qemu/shixin-win11/run.sh`. The repository helper delegates VM power and
VNC input to that controller; it does not invoke UTM or AppleScript.
Keep disks, installation media, downloads, credentials and evidence on an
external volume. Set `SHIXIN_WINDOWS_VMROOT` (or `SHIXIN_VMROOT`) to an
existing `/Volumes/<disk>/vm` directory. It never runs Tart.

| Path relative to the VM root | Contents |
|---|---|
| `iso/` | Windows ARM64 ISO and guest-tools ISO |
| `qemu/shixin-win11/` | `run.sh`, `disk.qcow2`, `efivars.fd`, monitor socket, PID file, QEMU log and raw screenshots |
| `shixin/shixin-win11/` | SSH key, `known_hosts`, separate `venv/`, `shots/`, `logs/`, `notes/`, `downloads/` |

The controller must support `start`, `stop`, `status`, `shot NAME`,
`keys KEY...`, `type TEXT` and `click X Y`. `shot` writes
`qemu/shixin-win11/shots/NAME.png`; the helper also copies it to the evidence
`shots/` directory. Use aarch64 `virt`, HVF, UEFI pflash, four cores, a
64 GiB NVMe disk, ramfb display and USB keyboard/tablet. Bind VNC to
**127.0.0.1:5907**, and forward **127.0.0.1:2222** to guest SSH port 22.
The guest uses 2048 MiB RAM normally. After a clean guest shutdown,
`MEM=6144 scripts/windows-vm.sh boot` gives heavy native builds more memory.

QEMU 11.1's TPM device failed under HVF with `HV_BAD_ARGUMENT` in this
setup. The disposable test guest uses Windows Setup's
`HKLM\SYSTEM\Setup\LabConfig` values `BypassTPMCheck`,
`BypassSecureBootCheck`, `BypassRAMCheck` and `BypassCPUCheck` (DWORD 1).
It does not validate supported production Windows hardware. Leave the
installer's “Press any key” prompt alone on reboot so the NVMe installation
boots. An installation ISO can remain attached alongside the guest-tools CD.

Finish setup with a local `shixin` account. On the network page,
Shift+F10 and `start ms-cxh:localonly` opens the local account route on
builds that support it. Decline optional telemetry and advertising. Install
ARM64 virtio network drivers from the guest-tools CD before expecting NAT
to work. Keep the account password only in the external private `notes/`.
Initialize host state with:

```sh
scripts/windows-vm.sh init-state
S="$SHIXIN_WINDOWS_VMROOT/shixin/shixin-win11"
python3 -m venv "$S/venv"
mkdir -p "$S/tmp"
TMPDIR="$S/tmp" PIP_CACHE_DIR="$S/downloads/pip-cache" \
  "$S/venv/bin/python" -m pip install vncdotool
```

Enable Windows OpenSSH Server, its firewall rule and automatic `sshd`
startup in the guest. Authorize the external state's `id_ed25519.pub`.
Administrator accounts use
`C:\ProgramData\ssh\administrators_authorized_keys`, with inheritance
removed and access limited to Administrators and SYSTEM; see Microsoft's
[key setup instructions](https://learn.microsoft.com/en-us/windows-server/administration/openssh/openssh_keymanagement).

```sh
scripts/windows-vm.sh boot
scripts/windows-vm.sh status
scripts/windows-vm.sh exec '$PSVersionTable; Get-ComputerInfo -Property WindowsVersion,OsBuildNumber'
scripts/windows-vm.sh put scripts/build-windows.ps1 C:/Users/shixin/build-windows.ps1
scripts/windows-vm.sh get C:/Users/shixin/build.log "$S/logs/build.log"
scripts/windows-vm.sh shot windows-desktop
scripts/windows-vm.sh keys super-r
scripts/windows-vm.sh type notepad
scripts/windows-vm.sh keys enter
scripts/windows-vm.sh click 400 300
scripts/windows-vm.sh stop
```

`exec` encodes one literal PowerShell string as UTF-16LE and requests UTF-8
output, preserving Chinese text through OpenSSH. `put` and `get`
use SFTP; guest evidence must be saved under the external Windows state
directory. `SHIXIN_WINDOWS_USER` and `SHIXIN_WINDOWS_SSH_PORT` override the
account and SSH port. SSH uses a dedicated `known_hosts`, accepts first-use
keys and rejects changed keys. The controller owns VNC configuration.

QEMU maps raw shifted punctuation keysyms to their base keys (for example
`_` becomes `-` and `:` becomes `;`). The helper’s `type` command splits
printable US ASCII into ordinary text and explicit Shift chords. Use `keys`
for Enter and Tab. Input is paced at 50 ms per key by default
(`SHIXIN_WINDOWS_KEY_DELAY` overrides milliseconds); fast long bursts can
overrun the guest USB keyboard and leave keys repeating. Keep VNC commands
short and prefer SSH once it works. After guest-tools installation, check
Caps Lock; the guest’s on-screen keyboard can clear a stale lock state. The external controller's current `stop`
requests ACPI shutdown, waits up to 60 seconds and then terminates QEMU;
request guest shutdown over SSH and confirm it completed before relying on
that command for normal shutdown.

## Client and presentation architecture

`DllMain` only records the module handle. TSF activation creates the hidden
dispatch window, preallocates queues, and starts background pipe workers.
The DLL contains no Go runtime, Rime, candidate UI, file watcher or model.
A disconnected client passes keys. The client implements Go-compatible
policy lookup and speculation with one 8 ms budget for an ASK or ordering
barrier. A queue overflow, malformed peer, deadline or failure degrades to
passthrough and requests a background reconnect. Existing inline code is finalized by
a queued edit after a failed key decision, preserving it as ordinary text.
Common client decisions
allocate no heap memory. Pipe connection and cancellation run off the app
thread; deactivation requests stop without joining a worker there. Worker
module pins prevent DLL unload while a thread still runs its code.

Replies reach the TSF apartment through fixed queues and a posted message.
Each activated thread commits about 0.6 MB for them, allocated once at
activation: byte rings holding variable-length frames (128 KiB for results
and for pending edits, 16 KiB for keys and focus), the policy tables, and
scratch space. A frame is limited to 64 KiB less four bytes; a longer one
is a protocol error and reconnects.
Only one edit request is submitted at a time, so an immediately granted
session cannot overtake an older deferred edit. Each request retains its
context and epoch; focus changes, secure transitions and reconnections
invalidate stale work. Input scopes for password, private and PIN fields,
read-only state, the disabled-keyboard compartment, and secure activation
flags guard input. Scope checks run inside edit sessions; key decisions
use cached security plus the disabled compartment. A migration stops new
keys, drains queued writes, half-closes the pipe, and applies final accepted
replies before adopting the next connection. A hung drain is cancelled by
the worker after two seconds, without waiting on an app thread.

Approved display fields, without commit text, and caret rectangles go
from TSF to a separate `ui.sock` pipe. `Shixin.exe` validates the peer PID,
current context before accepting them, takes every FocusIn (the
foreground window can lag behind TSF's notification), checks the foreground
process when it draws, and keeps only the latest display. The shared candidate view supplies skins, layout,
fonts and clicks. A click includes its displayed sequence; TSF rejects
stale clicks rather than selecting from a different menu. Positioning uses
screen pixels, monitor work areas and the host's DPI. Actual text-store,
DPI and click behavior still needs VM verification.

## Runtime and security

Go IPC uses go-winio's overlapped named pipes. Message mode supplies a write
half-close while preserving the byte-stream framing. Reads and writes go
through `pipeConn`, which reuses one preallocated operation per direction on
go-winio's handle (go-winio allocates two objects per call), so they and
deadline changes allocate nothing. The default discovery
directory is `%LOCALAPPDATA%\Shixin\run-<SessionId>`, with a protected
user/System DACL. Logical endpoints map to
`\\.\pipe\shixin-<SessionId>-<UserSID>-<path-hash>`; the hash is the first
16 bytes of SHA-256 over the lowercased absolute endpoint path, in UTF-8.
`engine.sock` is a discovery record containing the absolute sibling `g-*`
worker endpoint, replaced during promotion without symlink privileges.
Readers open it with `FILE_SHARE_DELETE` so the replacement never collides
with a read; the publisher also retries a sharing violation for up to 1 s.
Control, UI and AI endpoints map directly to pipes.

Pipe DACLs currently allow only the current user. Go checks peer SID/session
in both directions; native clients validate the server's SID/session and
require a sibling `shixin-engine.exe` or `Shixin.exe` image, respectively.
This development transport does not enable AppContainer/low-integrity
clients or verify Authenticode signatures. Those are release requirements.

The engine lock uses nonblocking exclusive `LockFileEx`, released on handle
close/process exit. Before dictionary attachment, LevelDB `LOCK` files must
allow an exclusive writable handle. Standbys still warm only schemas with
user dictionaries disabled; promotion still requires exclusive ownership.
Windows `StopRunning` refuses to terminate a PID from a lock file; a future
upgrade flow must stop the coordinator first. Rerank slots use native
`VirtualAlloc` memory. Engine/scorer threads request above-normal priority
and disable execution-speed throttling at startup.

## Remaining release work

- Verify registration and real typing in Windows VMs: Notepad, Office,
  Chrome/Electron, Explorer, Start search, then elevated and AppContainer
  apps after the security work. Run the checklist in `manual-tests.md`.
- Implement native auto spacing, composition display attributes and mode
  indication. Map desktop apps without explicit AppUserModelIDs to the
  shared picker IDs; packaged/explicit IDs are already reported.
- Let the front answer `-added` so the 开始使用 guide can show whether 拾新
  is added, and add startup registration. The shared settings app already
  shows Windows' guide steps and links 关于's updates to the release page
  (`cmd/shixin-settings/platform_windows.go`).
- Automate native llama builds, validate both architecture packages
  including emulation, and produce signed installers.
- Complete signature/AppContainer checks and measure actual Windows key,
  text application, candidate placement and allocation budgets before release.
