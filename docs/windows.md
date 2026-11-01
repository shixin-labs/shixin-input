# Windows development

Windows development builds include the C++ TSF DLL, a Go candidate/tray
host, the shared engine and settings GUI, and offline tools. Both x64
(`amd64`) and ARM64 (`arm64`) are build targets. This is an experimental
front: ARM64 native builds, librime, registration and Edge typing have been
tested in a Windows VM, with substantial app compatibility failures. See
the [status summary](#status-2026-10-31) and
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
# Visual Studio C++, CMake, Git, tar and Python 3 on PATH; PowerShell 7.
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
Tools are resolved through PATH; CMake discovers the installed Visual Studio
generator. `-Generator` overrides `CMAKE_GENERATOR` (unset by default); use a
Visual Studio generator supporting `-A`. `-Jobs` defaults to the host's logical
processor count. `fetch-rime-data.ps1 -URL` overrides the nightly rime-ice
release URL shown in that script. Both scripts derive all working paths from
the repository and require no fixed tool installation paths.
Python must allow imports from the script directory; a normal Python 3
installation works. An isolated embeddable distribution needs its `._pth`
configuration adjusted before OpenCC's sibling imports work.

The development DLL exports the internal methods needed by librime's
recovery tests. Its C++ development executables link
[`binmode.obj`](https://learn.microsoft.com/en-us/cpp/c-runtime-library/fmode?view=msvc-170)
so test fixtures write exact LF metadata. This fixes Windows test linkage
and line-ending behavior through build configuration. The pinned fork is
not edited.

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
Elevated unregistration now returns success: the profile API removes its
categories too, so no redundant category removal follows it. At medium
integrity that API still fails against a machine TIP; the COM class is
preserved on failure. Verify both user and machine state and preserve the
named API/HRESULT diagnostics described in
[known issues](KNOWN-ISSUES.md#windows-per-user-unregistration-remains-unavailable).

## Collecting development logs

A normal `Shixin.exe` launch appends bounded background diagnostics to
`%LOCALAPPDATA%\Shixin\Logs\shixin.log`. Its supervisor-started engine
uses `engine.log` in the same directory, including after automatic restarts.
The files are opened at startup and writes run in the logger goroutine;
key callbacks do no file I/O. Startup errors are flushed before the host
exits. If the platform directory cannot be opened, logging falls back to stderr.
The engine's `-log PATH` and `-log -` still override the default.

To collect diagnostics, preserve both files before restarting the fixture.
A medium-integrity VM launch with no `-log` argument was verified by
making librime temporarily unavailable: `engine.log` retained the DLL load
failure, `shixin.log` retained supervisor retries, and both retained recovery
when librime was restored. Never log typed text or candidates in normal builds.

## Windows test VM on macOS

On Apple Silicon, use `scripts/windows-qemu.sh` for QEMU/HVF power and
VNC control, and `scripts/windows-vm.sh` for SSH, transfers and paced input.
Both use `SHIXIN_VMROOT` (default `~/shixin-vm`), which must already exist;
neither creates the root. Choose storage with room for the guest disk (64 GiB
by default). The Windows VM is separate from the Tart VMs. Both helpers source
`scripts/windows-env.sh` for defaults; no VM configuration is duplicated.

| Environment variable | Default / meaning |
|---|---|
| `SHIXIN_VMROOT` | `$HOME/shixin-vm`; existing host storage root |
| `SHIXIN_WINDOWS_VM` | `shixin-win11`; VM name and state-directory name (letters, digits, `_`, `-`) |
| `SHIXIN_WINDOWS_USER` | `shixin`; guest SSH account |
| `SHIXIN_WINDOWS_SSH_PORT` | `2222`; loopback port forwarded to guest SSH port 22 |
| `SHIXIN_WINDOWS_VNC_PORT` | `5907`; loopback VNC port, 5900–65535; QEMU display is port minus 5900 |
| `SHIXIN_WINDOWS_CPUS` | `4`; virtual CPU count |
| `SHIXIN_WINDOWS_MEM` | `2048` MiB; falls back to legacy `MEM` when unset |
| `MEM` | Legacy RAM alias; `SHIXIN_WINDOWS_MEM` takes precedence |
| `SHIXIN_WINDOWS_DISK` | `64G`; size used only when creating a new disk |
| `SHIXIN_WINDOWS_ISO` | `Windows11_ARM64.iso`; absolute path or filename relative to `$SHIXIN_VMROOT/iso` |
| `SHIXIN_WINDOWS_TOOLS_ISO` | `utm-guest-tools.iso`; absolute path or filename relative to `$SHIXIN_VMROOT/iso` |
| `SHIXIN_WINDOWS_VNCDO` | `$SHIXIN_VMROOT/shixin/$SHIXIN_WINDOWS_VM/venv/bin/vncdo`; executable path or command on PATH |
| `SHIXIN_WINDOWS_KEY_DELAY` | `50` ms per key; applies to both helpers |
| `QEMU_FIRMWARE` | Discovered on start via `$(brew --prefix qemu)/share/qemu/edk2-aarch64-code.fd`; set a firmware file path without Homebrew |
| `HOME`, `PATH` | Inherited home directory and tool search path; `qemu-system-aarch64`, `qemu-img`, SSH tools and other utilities are found on PATH |

Empty overrides use the defaults. SSH and VNC always bind to loopback. Change
ports, firmware or resources after a clean shutdown; changing disk size does
not resize an existing disk. Changing the VM name selects separate state.

| Path relative to the VM root | Contents |
|---|---|
| `iso/` | Windows ARM64 ISO and guest-tools ISO |
| `qemu/$SHIXIN_WINDOWS_VM/` | `disk.qcow2`, `efivars.fd`, monitor socket, PID file, QEMU log and screenshots |
| `shixin/$SHIXIN_WINDOWS_VM/` | SSH key, `known_hosts`, `venv/`, screenshots, logs and private setup notes |

Install Homebrew QEMU and prepare the state and VNC client:

```sh
brew install qemu
export SHIXIN_VMROOT="$HOME/shixin-vm"
export SHIXIN_WINDOWS_VM=shixin-win11
mkdir -p "$SHIXIN_VMROOT/iso"
scripts/windows-vm.sh init-state
S="$SHIXIN_VMROOT/shixin/$SHIXIN_WINDOWS_VM"
python3 -m venv "$S/venv"
"$S/venv/bin/python" -m pip install vncdotool
```

Place a Windows 11 ARM64 installation ISO named `Windows11_ARM64.iso` and a
virtio guest-tools ISO named `utm-guest-tools.iso` in `iso/`. Override their
paths with `SHIXIN_WINDOWS_ISO` and `SHIXIN_WINDOWS_TOOLS_ISO`.
The controller uses `QEMU_FIRMWARE` or discovers Homebrew's QEMU firmware;
it reports a clear error if neither is available. On first start it
creates the NVMe disk and writable UEFI variables. It uses aarch64 `virt`,
HVF, four cores, ramfb and a USB keyboard/tablet. VNC binds to
**127.0.0.1:5907**; **127.0.0.1:2222** forwards to guest SSH port 22.
RAM defaults to 2048 MiB; after a clean shutdown, use
`SHIXIN_WINDOWS_MEM=6144 scripts/windows-vm.sh boot` for heavy native builds.

```sh
scripts/windows-qemu.sh start
scripts/windows-qemu.sh status
scripts/windows-qemu.sh shot setup
```

If UEFI opens its shell instead of Setup, run `fs0:` followed by
`\efi\boot\bootaa64.efi`. QEMU 11.1's `tpm-tis-device` fails under HVF with
`HV_BAD_ARGUMENT`, so this test VM omits TPM. In Setup, press Shift+F10 and
set the bypass values before continuing:

```bat
for %v in (BypassTPMCheck BypassSecureBootCheck BypassRAMCheck BypassCPUCheck) do reg add HKLM\SYSTEM\Setup\LabConfig /v %v /t REG_DWORD /d 1 /f
```

This guest does not validate supported production Windows hardware. On
reboot, leave the installer's “Press any key” prompt alone to boot NVMe.
If first-run preparation requests network recovery, install the virtio
network drivers from the guest-tools CD in a Shift+F10 console:

```bat
pnputil /add-driver E:\Drivers\*.inf /subdirs /install
```

Replace `E:` with the guest-tools drive letter. Some bundled driver versions
may be rejected; verify the network adapter with `ipconfig`, then continue
setup. At the normal OOBE network page, `start ms-cxh:localonly` opens the
local-account route on builds that support it; the recovery screen may
return “Class not registered”. Create a local `shixin` account and keep
credentials private.

Enable OpenSSH Server in an administrator PowerShell console in the guest:

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service sshd -StartupType Automatic
Start-Service sshd
Set-NetFirewallRule -Name OpenSSH-Server-In-TCP -Enabled True -Profile Any
```

Authorize the state's `id_ed25519.pub`. Administrator accounts use
`%ProgramData%\ssh\administrators_authorized_keys`, with inheritance removed
and access limited to Administrators and SYSTEM; see Microsoft's
[key setup instructions](https://learn.microsoft.com/en-us/windows-server/administration/openssh/openssh_keymanagement).
Confirm the firewall rule covers the guest NAT network's profile.

```sh
scripts/windows-vm.sh status
scripts/windows-vm.sh exec '$PSVersionTable; Get-ComputerInfo -Property WindowsVersion,OsBuildNumber'
scripts/windows-vm.sh put scripts/build-windows.ps1 build-windows.ps1
scripts/windows-vm.sh get build.log "$S/logs/build.log"
scripts/windows-vm.sh shot windows-desktop
scripts/windows-vm.sh keys super-r
scripts/windows-vm.sh type notepad
scripts/windows-vm.sh keys enter
scripts/windows-vm.sh click 400 300
scripts/windows-vm.sh exec 'Stop-Computer'
scripts/windows-vm.sh status
```

`exec` encodes literal PowerShell as UTF-16LE and requests UTF-8 output.
`put` and `get` use SFTP; save collected files under the Windows state
directory. `SHIXIN_WINDOWS_USER` and `SHIXIN_WINDOWS_SSH_PORT` override the
account and forwarded SSH port. SSH uses a dedicated `known_hosts`, accepts
first-use keys and rejects changed keys. Launch GUI tests in the interactive
desktop session; SSH commands run in a separate noninteractive session.

QEMU maps raw shifted punctuation keysyms to their base keys. The helper's
`type` sends printable US ASCII with explicit Shift chords; use `keys` for
Enter and Tab. Input defaults to 50 ms per key
(`SHIXIN_WINDOWS_KEY_DELAY` overrides milliseconds). Fast long bursts can
leave keys repeating; prefer SSH for scripts. After guest-tools installation,
use the on-screen keyboard to clear a stale Caps Lock state if needed.
`shot` saves a screenshot under both VM and helper state directories.
`stop` requests ACPI shutdown, waits up to 60 seconds, then terminates QEMU;
for normal shutdown, request `Stop-Computer` over SSH and confirm it stopped.

## Status (2026-10-31)

- Windows 11 Pro ARM64 build 26300.9457 ran under QEMU/HVF with four cores;
  native builds used 6144 MiB, GUI checks used 2048 MiB.
- ARM64 `build-windows.ps1 -Check`, vet and short Go tests passed.
  Native librime/Shixin tests (106 upstream cases) and serial short Go
  tests against the DLL passed, including dictionary lock preservation.
- Edge 156.0.4314.58 passed exact commits, selection, paging, Shift mode
  switching, directly focused password/PIN guards and basic placement.
  Settings rendered; live layout/theme/punctuation changes and worker recovery passed.
- A subsequent medium-integrity check with UAC enabled verified exact `你好`
  in Notepad and Explorer search after fixing empty input-scope handling.
  Start search still falls back to Latin text. Explorer rename with 自然码
  also passed exact `你好.txt` after normalizing the repeat count between
  the shell’s test-key and key-delivery callbacks.
- Edge Ctrl+L now finalizes `ni hao` once: the client ends the marked range
  with its pooled edit request before passing an unhandled app shortcut and
  invalidates pending replies. The exact copied text passed with UAC enabled.
- The candidate flyout clips its native region to the measured rounded card.
  With a visible menu, clicking the password field below it passes literal
  input; clicking the first candidate still commits exact `你好`.
- Registration needing elevation remains.
  See [known issues](KNOWN-ISSUES.md#windows-validation-and-release-support).
- x64/emulated apps, Office/Chrome, mixed DPI, GPU scoring and packaging remain unverified.

Debug native callback routing with `-DSHIXIN_TSF_TRACE=ON` in the TSF
CMake configuration. This opt-in build queues fixed numeric records on the
apartment and writes them only on the pipe worker, under
`%USERPROFILE%\AppData\LocalLow\Shixin\tsf-<pid>-<thread>.log`.
It includes activation, focus, input-scope and edit-request HRESULTs and
key callback codes, so use it only for explicit development reproductions.
The default build omits tracing.

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
fonts and clicks. Its native rounded window region matches the measured card,
so the rest of the prewarmed transparent canvas passes app clicks through.
A click includes its displayed sequence; TSF rejects
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
