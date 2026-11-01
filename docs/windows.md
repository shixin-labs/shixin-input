# Windows development

Windows development builds include the C++ TSF DLL, a Go candidate/tray
host, the shared engine and settings GUI, and offline tools. Both x64
(`amd64`) and ARM64 (`arm64`) are build targets. This is an experimental
front: registration and real typing have **not yet been verified in a
Windows VM**. There is no signed Windows installer or updater yet.

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

Supply a same-architecture build of our pinned librime with
`librime-shixin` merged and the dynamic plugin loader disabled; do not
use a library with Lua enabled. The native library build is not automated
by the Windows script yet. Put `rime.dll` and its DLL dependencies next
to the executables, shared Rime data under `data/`, and optional compiled
dictionaries under `prebuilt/`. The Go loader searches the library's own
directory and System32, excluding the working directory and PATH.

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
settings, then test in Notepad. The tray opens the same settings GUI as
macOS. Its first-run guide and update installer still need Windows adapters.

To remove the development profile in the VM:

```powershell
./build/windows-amd64/Shixin.exe -unregister
```

Close apps using the DLL before deleting its files. An ARM64 DLL supports
native ARM64 applications; the package strategy for x64 emulated apps,
which require a matching DLL, still needs validation. The build script does
not create logon startup entries; start `Shixin.exe` before typing tests.

## Windows test VM on macOS

Use UTM's QEMU backend for Windows ARM64 on Apple silicon. Keep the VM
bundle, installation media, downloads, credentials and captured evidence
on an external volume. Set `SHIXIN_WINDOWS_VMROOT` to the existing `/Volumes/<disk>/vm` directory
on the external volume (or use the helper's configured default). It never
runs Tart.

Storage relative to that root:

| Path | Contents |
|---|---|
| `iso/` | Windows ARM64 installation ISO and guest-tools media |
| `utm/shixin-win11.utm/` | VM bundle, disk, NVRAM and TPM state |
| `shixin/shixin-win11/` | SSH key, `known_hosts`, separate `venv/`, `shots/`, `logs/`, `notes/`, `downloads/` |

Create the VM with aarch64, UEFI, hypervisor enabled, TPM 2.0, 6144 MiB
RAM, four cores and a 64 GiB NVMe disk. Use a removable USB/CD drive for
the installer. Before installing, move any VM created in UTM's default
location onto the external volume and open it there. With the VM stopped,
set `QEMU.TPMDevice` to true in `config.plist` if the creation interface
omits TPM, then reload its configuration. Configure an emulated NAT network
with a TCP port forward from **127.0.0.1:22222** to guest port 22 and QEMU
additional arguments `-vnc 127.0.0.1:5` for setup automation. These ports
are local to the host; keep VNC bound to loopback. See UTM's
[scripting reference](https://docs.getutm.app/scripting/reference/) and
[Windows guide](https://docs.getutm.app/guides/windows/).

Finish Windows setup with a local `shixin` account, then install UTM's
[Windows guest tools](https://docs.getutm.app/guest-support/windows/).
If setup requires an account and offers no offline route, ask the operator;
never supply an unrelated Microsoft account. Initialize the host-side state:

```sh
scripts/windows-vm.sh init-state
# Install vncdotool in the VM's own external-volume venv, if absent.
VMROOT="$SHIXIN_WINDOWS_VMROOT"
S="$VMROOT/shixin/shixin-win11"
python3 -m venv "$S/venv"
mkdir -p "$S/tmp"
TMPDIR="$S/tmp" PIP_CACHE_DIR="$S/downloads/pip-cache" \
  "$S/venv/bin/python" -m pip install vncdotool
```

Inside the guest, enable OpenSSH Server and its firewall rule, start `sshd`
and set it to start automatically. Authorize `id_ed25519.pub` from the host
state directory. For an administrator account Windows uses
`C:\ProgramData\ssh\administrators_authorized_keys`, with inheritance
removed and only Administrators and SYSTEM granted access; see Microsoft's
[key setup instructions](https://learn.microsoft.com/en-us/windows-server/administration/openssh/openssh_keymanagement).
Keep guest passwords in the private external state directory, never in Git.

```sh
scripts/windows-vm.sh boot
scripts/windows-vm.sh status
scripts/windows-vm.sh exec '$PSVersionTable; Get-ComputerInfo -Property WindowsVersion,OsBuildNumber'
scripts/windows-vm.sh put scripts/build-windows.ps1 C:/Users/shixin/build-windows.ps1
scripts/windows-vm.sh get C:/Users/shixin/build.log "$S/logs/build.log"
scripts/windows-vm.sh shot windows-desktop
scripts/windows-vm.sh vnc key super-r type notepad key enter
scripts/windows-vm.sh stop            # requests guest shutdown
# Only if guest shutdown cannot complete:
scripts/windows-vm.sh force-stop
```

`exec` accepts one literal PowerShell string and encodes it as UTF-16LE
before sending it to the guest. `put` and `get` use OpenSSH SFTP; downloaded
guest files must go under the Windows state directory. The helper reads the
VM UUID from the external bundle, so another VM with a similar name is not
a target. `SHIXIN_WINDOWS_USER`, `SHIXIN_WINDOWS_SSH_PORT`,
`SHIXIN_WINDOWS_VNC_PORT`, `SHIXIN_WINDOWS_VNCDO` and `SHIXIN_UTMCTL` override
the account, ports and installed tools. SSH uses a dedicated `known_hosts`
file, accepts first-use keys and rejects changed keys. First-use Automation
permission prompts must be handled by the operator on the Mac.

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
- Automate native librime/llama builds, validate both architecture packages
  including emulation, and produce signed installers.
- Complete signature/AppContainer checks and measure actual Windows key,
  text application, candidate placement and allocation budgets before release.
