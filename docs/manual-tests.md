# Manual tests

Checks to run by hand in the macOS test VM (`scripts/macos-vm.sh`, see
[AGENTS.md](../AGENTS.md#gui-testing)) when changing a feature below. Each
complements automated tests, named here, that cover the same behavior
headlessly. Never install a development build as an input method on your
own Mac.

Shared procedure:

- Upgrade the VM's installed Shixin in place (`scripts/macos-vm.sh
  upgrade`), select Shixin in Chinese mode, and use fresh TextEdit
  documents unless a check says otherwise.
- Type with `scripts/macos-vm.sh input` and check committed text exactly:
  end with `key cmd+a key cmd+c expect TEXT`. Put a sentinel on the
  clipboard first (`scripts/macos-vm.sh exec 'printf %s pending | pbcopy'`),
  so a failed copy can't pass.
- Back up the guest's `~/Library/Application Support/Shixin/settings.json`
  before changing settings, and restore it afterwards.
- Look at affected settings pages in Chinese and English, light and dark,
  at 880×640 and at the 720×480 minimum (headless:
  `go test ./cmd/shixin-settings -snapshot DIR`).

Not yet covered by the VM: Retina displays (the VM is 1×), multiple
displays and Intel Macs.

## Candidate font picker

Automated: `go test ./cmd/shixin-settings -run 'Test(Font|UnavailableFont)'`,
`go test ./internal/ui/fonts ./internal/ui/candview` (`TestMeasure` covers
every installed family).

1. Appearance → search for and pick Songti SC. `settings.json` saves the
   exact family; the preview and a real candidate panel use the serif face.
   Type `nihao`, Space: `你好`.
2. Pick Menlo; the preview shows Chinese and emoji through fallback fonts.
3. At 720×480 in dark mode, the picker and its description fit and the
   popup opens inside the window. Choose 系统默认字体 / System default font:
   `font_family` is saved empty.
4. The engine's coordinator and worker keep their process IDs throughout:
   fonts never rebuild the engine.

## Second- and third-candidate shortcuts

Automated: `TestRimeIce`, `TestRimeIceOptions` and the replay with
shortcuts on (`go test ./internal/engine -run '^Test(RimeIce|Replay)'`).

Choose each preset in Shortcuts, then:

| Preset | Input | Committed |
|---|---|---|
| Ctrl | `R123`, Ctrl+`;` | 壹佰贰拾叁元整 |
| Ctrl | `R123`, Ctrl+`'` | 一百二十三 |
| Option/Alt | `cC6*7`, Option+`;` | 6*7=42 |
| Option/Alt | `R123`, Option+`'` | 一百二十三 |

With the preset off, and in English mode, the keys pass through.

## Default punctuation

Automated: `TestEnglishPunctuationPolicies`, `TestPunctuationToggleAfterDigit`.

Choose English punctuation in General, then:

| Input | Committed |
|---|---|
| `nihao`, Space, `,.` | 你好,. |
| Ctrl+`.`, `nihao`, Space, `,.` | 你好，。 |
| Ctrl+`.`, `nihao,.` (no Space) | 你好,. |

The saved preference stays English after the temporary toggle.

## Forgetting learned words

Automated: `TestForgetCandidatePersistence` (learn, forget and verify in
separate processes), `TestMatchesSettings`,
`TestCoordinatorRebuildsOldCompiler`.

1. Learn 试验 until it outranks 实验 (type `shiyan`, pick 试验, a few times).
2. Type `nihao`, Space, then `shiyan`, highlight 试验 with the arrow keys
   and press Ctrl+Shift+Backspace. The menu refreshes without committing;
   Space now commits 实验: `你好实验`.
3. Choose Ctrl+Delete in Shortcuts, relearn 试验 and forget it with
   Ctrl+Delete. Choose 无 / None: Ctrl+Delete leaves the learning alone,
   also after the settings hot swap and in a fresh document.
4. After upgrading over a build whose compiler differed, the new default
   shortcut works without a settings edit: the coordinator rebuilds a
   generation built by an older compiler.

## Full pinyin while using shuangpin

Automated: `go test ./internal/engine -run
'Test(MixedPinyinSpellings|RimeIceMixedPinyin|ShuangpinCharts)'`.

1. Choose 小鹤双拼 and turn on 同时支持全拼 / Also accept full pinyin.
   Check `full_pinyin_fallback: true` in `settings.json`.
2. Type `nihao`, `nihc`, `zhonggo` and `vsguo`, each with Space:
   `你好你好中国中国`.
3. Switch to 微软双拼; the option stays on. `nihao` and `nihk`: `你好你好`.
4. Turn it off: `nihk` still commits 你好, and `nihao` no longer does.

## Shuangpin keyboard charts

Automated: `TestShuangpinLayouts`, `TestShuangpinCharts` (every displayed
spelling against librime), and the settings snapshots.

Choose each double-pinyin layout and open its chart from General. At
880×640 the whole chart shows; at 720×480 in dark mode the contents
scroll while the title and Done stay fixed. Done closes it without
changing settings. Full pinyin shows no chart button.

## Safari's address bar

Automated: `TestAutoSpaceAfterHiddenSelectAll`,
`TestShortcutModifierPassesWithoutEngine`.

Use a fresh address bar (Cmd+L) for each case; never submit.

| Check | Action | Expected |
|---|---|---|
| Composition | `nihao` | Marked `ni hao` in the bar; candidates below the caret, unclipped |
| First candidate | `nihao`, Space | `你好` |
| Number selection | `nihao`, 3 | `拟好` |
| Arrow selection | `nihao`, Down, Down, Space | `拟好` |
| Editing | `nihaox`, Backspace, Space | `你好` |
| Cancel | after `你`, `nihao`, Escape | Preedit and panel gone; `你` |
| English URL | English mode, type the URL below | The URL exactly |
| URL after deleting Chinese | `nihao`, Space, Cmd+A, Backspace, Shift, type the URL | The URL exactly, with no leading space |

```text
https://example.com/shixin-test?q=hello&n=123#check
```

The last case guards a fixed bug: Safari consumes Cmd+A before IMK sees
it, so the front resets auto spacing on unforwarded modifier changes
(DESIGN §7.1).

### Apps running during input-source cache repair

In the test VM, keep an app with a stale input-source cache open while
installing Shixin or running `Shixin -repair`.

1. Settings shows nothing about it; the repair is silent. The app's two
   cache files must not be deleted while it runs. `Shixin -cache-status`
   lists it as JSON. The helper invalidates the shared modification-date marker and
   cached enabled-source state once.
2. Switch away from and back to Shixin and type. Record whether the app's
   `.kbdx` now includes Shixin, whether Chinese input works, and whether the
   app or `pboard` restarted. Cache freshness alone does not pass the typing
   check. Verify Shixin's own candidate panel and, in debug mode, its key
   events: PowerPoint may silently switch to another Chinese input method.
   Select the text to replace before switching sources. Use the actual
   input menu or Control-Space; a diagnostic TIS selection returning success
   does not prove that the app accepted the switch. Check that existing
   clipboard formats remain intact.
3. For an app that has not refreshed, quit that app (the 开始使用 guide
   says to quit and reopen an app that won't switch).
   Only its two `com.apple.IntlDataCache.le*` files should be removed;
   unrelated cache files must remain. Repeated repair requests must not
   leave multiple watcher processes running.
4. `Shixin -cache-status` must no longer list the app.
5. Reopen the app, select Shixin, type and commit Chinese, then switch to
   ABC and back. Check that no new crash report appears.

`SHIXIN_TEST_CACHE_REFRESH=1 ./shixin-front.test -test.run TestCacheRefreshPreservesClipboard`
checks general clipboard change count and multiple data flavors across
repeated marker resets and enabled-state invalidations. It seeds the
clipboard with test data: VM only.

## In-app update

Needs a signed, notarized build older than the latest published release,
installed with Shixin.app's double-click installer (an ad hoc or
development build reports that it can't update itself, which is also worth
checking once).

1. Open Settings → 关于. The 更新 group must show the current version and
   do nothing until 检查更新 is clicked (watch the network: nothing leaves
   the Mac before the click).
2. Click 检查更新. With no newer release it says 已是最新版本; otherwise
   it shows the new version and its notes.
3. Click 下载并安装. A progress bar shows the download; 取消 stops it and
   leaves the group on the same offer. Try once with the network off for
   the error and 重试.
4. After the download Settings closes, and reopens on 关于 showing the new
   version. No consent dialog appears, 拾新 stays in the input menu, and
   typing works after switching to it. The download folder under
   `~/Library/Caches/Shixin/Updates` is gone.
5. `~/Library/Logs/Shixin/shixin.log` has the `update:` steps. Checking
   again says 已是最新版本.

## Windows TSF development front (VM only)

Use a disposable Windows snapshot and the matching DLL, host, backend,
and native libraries described in [Windows development](windows.md).
This is the repeatable checklist; the ARM64 results below distinguish passes,
failures and untested cases. Registration and unregistration must happen only in the VM. For host control and external
evidence storage, see [Windows VM setup](windows.md#windows-test-vm-on-macos).

1. Register with `Shixin.exe -register`, start `Shixin.exe`, and select
   拾新 in Windows's language/input settings. Confirm registration appears
   for the intended architecture; test native ARM64 and x64 emulated apps
   separately before making architecture support claims.
2. In Notepad, type `nihao` and Space; copy and assert exactly `你好`.
   Exercise digits, candidate clicks, arrows, Backspace, Escape, Enter,
   paging, Shift tap, Shift shortcuts and Ctrl shortcuts during composition.
   A key tested by TSF and then delivered must be sent once, not twice.
3. Open the tray's settings app. Confirm the same settings pages, controls,
   skins and translations as macOS; change pinyin layout, punctuation and
   appearance and confirm the live engine/host changes.
4. Switch fields, documents, apps and input sources with results pending.
   Confirm no late text or candidates reach a new context. Test selection
   replacement and focus with existing selected text: focus alone must not
   erase it. Confirm inline text is finalized on focus loss.
5. Exercise browser password fields, numeric passwords/PINs, private input
   scopes, read-only controls and disabled keyboard compartments. Keys must
   pass without reaching the engine, and stale results must stay hidden.
6. Stop, hang and restart the engine during a burst. Check passthrough and
   the 8 ms decision budget. Apply settings during typing: accepted commits
   must drain before the old connection is replaced. Keep a TSF edit session
   deferred and confirm later text never overtakes it or blocks typing.
7. Check candidate position on 100%, 150% and 200% DPI, mixed-DPI monitors,
   negative monitor origins and every work-area edge. Click a candidate
   while new results/focus changes arrive; an old menu must not select a
   word from its replacement. Candidates must not take keyboard focus.
8. Repeat in Word/Office, Chrome/Electron, Explorer and Start search.
   Elevated/AppContainer tests remain gated on the documented security work.
   Close test apps and unregister with `Shixin.exe -unregister`; confirm no
   unrelated input sources or user dictionaries changed.

### Windows QEMU setup (2026-10-31)

Setup checkpoint: **Windows installed and SSH verified**. Product checks
are recorded in the later sections. QEMU/HVF
booted Windows 11 Pro ARM64 build `26300.9457` from the NVMe disk with
2048 MiB RAM and four cores. UTM was abandoned after scripting timeouts;
the helper now delegates to the external `qemu/shixin-win11/run.sh`.
No UTM or AppleScript commands are needed.

Observed setup steps and evidence, relative to the external VM root:

- **Pass:** Windows installation progressed through 94% and entered OOBE.
  Evidence: `shixin/shixin-win11/shots/disk-reboot.png`.
- **Recovered setup failure:** first-run preparation showed “Why did my PC
  restart?” and requested network access. Shift+F10 opened an administrator
  command prompt. On this run the guest-tools CD was `E:`; running
  `pnputil /add-driver E:\Drivers\*.inf /subdirs /install` accepted 153 of
  296 packages. Older packages reported catalog or trust errors. This is
  not a claim that every guest device driver works.
- **Pass:** `ipconfig` showed the Ethernet adapter at `10.0.2.15`, with
  gateway `10.0.2.2`. Evidence:
  `shixin/shixin-win11/shots/network-driver.png`. Returning to the recovery
  screen and choosing Next reached the normal country/region OOBE page:
  `shots/network-recovery-stage.png` in the same state directory.
- **Pass:** choosing United States, US keyboard and skipping a second
  layout reached normal OOBE. `start ms-cxh:localonly` then opened the
  local-account form. The same command had returned “Class not registered”
  on the earlier recovery screen. If the console appears inactive, click
  its client area before typing. Evidence: `shots/localonly-open.png`.
- **Pass:** completed the local account and security questions, installed
  guest tools, and verified the recorded password after restarting. The
  account form's Tab order reaches Accessibility before Next; click Next
  explicitly after filling the last answer. Setup credentials and any
  screenshots showing security answers remain in private `notes/`.
- **Pass:** installed the OpenSSH Server Feature on Demand, configured
  automatic `sshd`, and installed the dedicated administrator public key
  with only Administrators and SYSTEM granted access. The created firewall
  rule was Private-only, so the NAT network initially timed out; setting
  `OpenSSH-Server-In-TCP` to profile Any enabled the forwarded connection.
  Host key login reports ARM64 and the intended local account. Evidence:
  `logs/ssh-verification.log` and `shots/ssh-firewall-status.png`.
- **Pass:** `exec`, SFTP uploads, status and screenshots work through the
  helper. OpenSSH parses `UserKnownHostsFile` itself, so paths containing
  spaces require quotes inside the `-o` argument. The helper now supplies
  those quotes and stores the host key in the external state directory.

The helper's `type` now sends explicit Shift chords for US punctuation:
raw VNC typing had changed `:` to `;`, `_` to `-`, `+` to `=` and `*` to `8`.
Actual `start ms-cxh:localonly` and quoted PowerShell commands verified the
colon and quote handling. A long encoded command at the original fast key
rate left keys repeating across apps; controller shutdown/restart recovered
the guest. The helper now defaults to 50 ms between keys, and short commands
plus a password login passed at that rate. Prefer SSH for scripts. After
guest-tools installation, the on-screen keyboard cleared a stale Caps Lock
state that VNC Caps Lock taps did not clear. Bash syntax, ShellCheck, all shifted-symbol
mapping checks against a mock controller, rejected non-ASCII/control
characters and power delegation passed; external
`notes/helper-qemu-check/checks.log` records the mock checks.

Settings UI, application input and CPU/memory measurements remain **not
run** at this setup checkpoint; native checks are recorded below. The existing macOS
VMs were left alone. Edge's first-run sync, automatic import, personalization
and startup choices were declined, as was the OneDrive backup offer. The native toolchain and application results follow below. Credentials
remain only in private external notes.

### Windows ARM64 native build checks (2026-10-31)

The test VM used Windows 11 Pro ARM64 build `26300.9457`, four virtual
cores, Go 1.27.3, PowerShell 7.6.9, CMake 4.4.5, Python 3.14 and Visual
Studio Build Tools 2022 17.14.44 (MSVC 19.44.35229, Windows SDK
10.0.26100). RAM was temporarily raised from 2048 to 6144 MiB after a clean
shutdown for toolchain installation and native builds, then returned to
2048 MiB for GUI tests. The source was commit `b7c2ade` plus the new Windows
librime/data build scripts; the librime pin was
`3438a3b860cd3525f25ee2198c35ea49a3f2d85d`.

- **Pass:** `build-windows.ps1 -Arch arm64 -Check`: native ARM64 DLL,
  client and COM smoke tests (2/2), vet and all short Go tests.
  Evidence: `logs/check-windows.log`.
- **Pass:** `fetch-rime-data.ps1` downloaded and extracted the shared data,
  excluding the Lua scripts directory. Both new scripts passed Windows
  PowerShell parser checks.
- **Pass:** `build-librime.ps1 -Arch arm64 -Test`: Shixin's native component
  suite and all 106 upstream librime cases. The build exports internal C++
  methods used by the recovery tests and links `binmode.obj` into its C++
  development executables; this fixes missing exports and fixtures that
  otherwise write CRLF into LevelDB's LF-only `CURRENT` metadata. Python's
  embeddable isolated-path mode was disabled in the test toolchain so
  OpenCC could import sibling modules. Evidence: `logs/build-rime.log`.
- **Pass:** PE inspection shows ARM64, only `dbghelp.dll`, `KERNEL32.dll`,
  `USER32.dll` and `ADVAPI32.dll` dependencies, and no Lua exports. CMake's
  external plugin loader is off and the merged module list is `shixin`.
  Evidence: `logs/rime-pe-inspection.log`.
- **Pass:** short Go tests against the actual DLL for `internal/rime`,
  `internal/engine`, `internal/generation` and `cmd/shixin-engine`, run with
  `-p 1`. The two-process user dictionary lock/preservation regression
  passed. The rerank allocation test hit its five-second read deadline in
  the initial parallel run, then passed alone (4.02 s) and in the serial
  package run. Evidence: `logs/check-rime.log`, `logs/rerank-isolated.log`.
- **Pass:** `Shixin.exe -register` returned success. Its current-user COM
  class points at the ARM64 DLL, and the user language list contains the
  Shixin TIP under Simplified Chinese while preserving English/US.
  Evidence: `logs/registration.log`, `logs/keyboard-profile.log`.

These checks do not establish real typing or candidate-window behavior.
The application matrix below is a separate GUI run with explicit scratch
settings/dictionaries and persistent test logs. Native x64 builds,
emulated-app packaging, llama/GPU scoring and physical hardware remain
unverified by this run.

### Windows ARM64 GUI results (2026-10-31)

All registration and input ran inside the disposable guest. The source and
native dependency pin match the preceding build section; no Go/C++ product
code was changed for this run. Settings and dictionaries were isolated under
`C:\vmtest\scratch\roaming\Shixin`, with process-specific `APPDATA` for
the host and Settings app, explicit engine `-user`/`-settings`, and ordinary
`LOCALAPPDATA` for per-session pipe discovery. The host, engine and clipboard
observer ran in interactive session 1; SSH commands run in session 0 and
must not launch the GUI directly. Temporary on-demand interactive tasks
supplied that bridge, without startup triggers or stored passwords.

The first GUI run had UAC disabled (`EnableLUA=0`), discovered during the
final token audit; Explorer and the test processes ran at high integrity.
The cause of that setup state was not isolated. UAC was then enabled inside
the guest, followed by a clean 2048 MiB reboot. Explorer, Notepad, the host
and both engine processes were verified at medium integrity (`0x2000`);
Edge's browser process was medium, with sandboxed child processes at lower
levels. `logs/normal/normal-integrity.txt` records those tokens. The table
below records the initial run; the normal-desktop repeat is summarized
immediately afterward. No host security settings were changed.

GUI tests used **2048 MiB**, four cores, one 800×600 ramfb display at
96 DPI (100%), Chinese UI, full pinyin initially, page size 5, horizontal
candidates, Shift `commit_code`, rerank off and no auto spacing. Later tests
changed to 自然码双拼, English punctuation and the dark candidate theme.
Evidence paths below are relative to the external `shixin/shixin-win11/`
state directory. The guest's clock differs from the host date in filenames.

| Target | Result | Evidence / limits |
|---|---|---|
| Notepad ARM64 11.2607.14.0 | **Fail:** `nihao` + Space produces literal `nihao `; DLL loaded, no candidates | `shots/notepad-slow-nihao.png`; clipboard bytes `bmloYW8g` (base64) |
| Edge ARM64 156.0.4314.58 textarea | **Pass:** exact `你好` with Space; digit 3 selected the visible `拟好`; Down/Space selected `你好`; numpad 3 selected the visible word | `shots/edge-nihao.png`, `shots/edge-highlight-down.png`, `shots/edge-numpad3.png`, `logs/gui-cases.jsonl` |
| Edge address bar | **Pass:** composition and exact `你好` commit | `shots/edge-addressbar-nihao.png`, case `edge-addressbar-space` |
| Edge focus within app | **Fail:** Ctrl+L finalizes `ni hao` twice | `shots/edge-ctrl-l-before.png`, `shots/edge-ctrl-l-after.png`; case `edge-ctrl-l-fresh-repro` |
| Edge focus between apps | **Pass** for the tested Alt+Tab round trip: one finalized `ni hao`, no stale panel in the other app | case `edge-fresh-alt-tab-finalization`; broader races remain untested |
| Edge candidate mouse input | **Pass:** candidate click commits and following letters continue in the textarea; **fail:** clicks below the panel do not focus the password input | `shots/edge-candidate-click-after.png`, `shots/edge-password-click-after-six-seconds.png` |
| Edge password, numeric password/PIN, read-only | **Pass** for direct focus with no pending menu: literal `nihao`, literal `1234`, unchanged read-only sentinel, no visible candidates | `shots/secure-password-final.png`, `shots/secure-pin-final.png`; final fixture JSON confirms all four field values |
| Explorer rename, version 10.0.26100.8117 | **Fail:** 自然码 `nihk` produces `nin chi hang kao`, then `您吃行靠.txt` | `shots/explorer-shuangpin-fresh.png`, `logs/explorer-result.json`; Edge commits `你好` for the same keys |
| Explorer search | **Fail:** literal `nihk`, no candidates | `shots/explorer-search-nihk.png` |
| Start search, SearchHost 2608.25008.200.0 | **Fail:** literal `nihk`, no candidates | `shots/start-search-nihk.png`; security/integrity cause not isolated |
| WordPad, Chrome, Word/Office | **Not available** in the guest | No substitute app was used to claim coverage |

**Normal-desktop repeat:** interactive medium-integrity registration
failed with `0x80004005`; elevated registration succeeded. The medium host
and engine then reproduced the Notepad fallback (`nihk `), Explorer rename
(`您吃行靠.txt`), Explorer/Start search fallback, Ctrl+L duplication and
candidate-region click failure. Evidence: `shots/normal-notepad-nihk.png`,
`normal-explorer-rename.png`, `normal-explorer-search.png`,
`normal-start-search.png`, `normal-ctrl-l-duplicate.png`,
`normal-candidate-click-blocked.png` in the same `shots/` directory, and
`logs/normal/normal-explorer-result.json`.

Normal Edge passed exact `你好.` from 自然码 `nihk`, Space and period;
digit 3 selected `拟好`; composing Shift committed `nihk` once and switched
to English `abc`; idle Shift returned to Chinese; paging and Escape worked;
the address bar committed exact `你好`; and the fresh secure-field fixture
confirmed literal password/PIN values and unchanged read-only content.
Cases prefixed `normal-` in `logs/gui-cases.jsonl` record those assertions.
The normal Windows guide rendered (`shots/normal-settings-guide.png`).
The tray context menu opened Settings at medium integrity
(`shots/normal-tray-menu.png`, `shots/normal-tray-settings-open.png`).
The Settings UI changed back to full pinyin in a successful 15.09-second
swap, with a 222.91 ms release-to-ready gap. A fresh Edge field committed
exact `你好` afterward; killing the worker then recovered automatically and
typing passed again (`normal-worker-restart-confirmed`). Two earlier
target-window attempts (`normal-full-pinyin-after-hot-swap` and
`normal-worker-restart`) lacked a confirmed foreground target and returned
empty clipboard values; they are excluded in favor of the `-confirmed`
cases and screenshots. This run
does not prove uninterrupted key delivery across the migration gap.

Additional Edge checks passed: PageDown/PageUp changed and restored the
visible page; Backspace then Enter committed exact `niha`; Escape cancelled
to an empty fixture; composing Shift committed exact `nihao` once and
switched to English (`abc`); idle Shift returned to Chinese. A numpad key
selected after each VNC Num Lock toggle, but QEMU's keysym translation does
not establish physical keyboard Num Lock-off navigation behavior. Ctrl+A/C
worked after committed text. An empty-selection copy leaves the Windows
clipboard unchanged, so the Escape check uses a fixture JSON readout rather
than treating a stale clipboard sentinel as text. Final clipboard assertions
use UTF-8 bytes encoded as base64 over SSH, preserving trailing spaces. The
initial Notepad log trimmed whitespace in the host helper; a separate
base64 read and the normal-desktop repeat confirm the trailing space.

All nine Settings pages rendered in Chinese, including the Windows guide
(`shots/settings-guide-actual.png`) and the scratch user-data path on About.
A maximized window fits the small display; its initial centered bounds
extended offscreen. Screenshots `settings-{appearance,input,ai,shortcuts,
dictionary,apps,performance,about}.png` record the other pages. The first
live layout apply failed with `builder failed: exit status 1`; the old
engine remained available. A later retry succeeded, with a temporary
external builder-output probe, then the original executable was restored.
`logs/builder-probe.log` records the actual arguments and successful build.
The engine reports successful swaps in 16.60 and 16.87 seconds, with
release-to-ready gaps of 38.84 and 52.49 ms. Edge then committed exact
`你好.` from `nihk`, Space, period; the dark theme appeared in the actual
candidate window (`shots/edge-shuangpin-nihk.png`). This passes those live
settings changes; it does not pass every setting, skin or translation.

Worker termination recovered automatically: the coordinator replaced the
worker and exact `你好` typing passed (`edge-after-worker-restart`). In a
separate suspension test, letters stayed invisible until the worker was
resumed, then appeared as composition; immediate literal fallback was
**not** established (`shots/engine-hang-{fallback,after-resume}.png`). No
real-app 8 ms ASK timing claim follows from the portable tests or VNC.

Candidate placement passed the ordinary top-left field and a bottom-right
caret: the panel moved above the caret and stayed within the right work-area
edge (`shots/candidate-bottom-right-focused.png`). **Not run:** 150%/200%
DPI, mixed monitors, negative origins, physical Num Lock-off behavior,
private/disabled TSF compartments, cross-integrity/AppContainer
authorization, deferred edit-session ordering under forced races, x64 emulated applications, English
UI, all skins, llama/GPU scoring, signed packaging and updater installation.
The single fixed-resolution display and absent apps limit this run.

Resource samples use CPU-time differences, not cumulative readings. The
normal-desktop 30.17-second idle sample and a monotonic 13.96-second
50-key burst are in `logs/normal/normal-idle-processes.json` and
`logs/normal/normal-burst-processes.json`. The burst committed
`你好` ten times exactly, including at the helper's normal 50 ms key pace.
Settings was closed for these samples; all Edge subprocesses are retained
in the raw records, without attributing their combined cost solely to TSF.

| Process | Idle CPU seconds | Burst CPU seconds | Burst working set / private memory (MiB) |
|---|---:|---:|---:|
| Candidate host | 0.0625 | 0.1719 | 12.3 / 69.4 |
| Coordinator | 0.1094 | 0.0313 | 19.6 / 63.0 |
| Engine worker | 0.0625 | 0.0156 | 22.6 / 81.3 |

In the initial UAC-disabled run, memory pressure was substantial: one
sample had about 225 MiB of physical memory free and about 452 MiB of
commit space remaining. The performance
page recorded p50 576 µs, p99 114.69 ms and p99.9 669.30 ms over a mixed
168-key session (`shots/settings-performance.png`). These are a failed
engine latency target in this constrained VM, not a native hardware
benchmark or proof that an app thread blocked for those durations.

`logs/gui-cases.jsonl` includes exploratory cases as well as final checks:
`edge-escape-cancel` used an invalid empty-copy oracle, and
`edge-password-tab-and-focus` used Tab during composition (candidate
navigation). The final fresh fixture cases supersede those attempts;
`edge-fresh-focus-ctrl-l` and `edge-ctrl-l-fresh-repro` reproduce the actual
duplication. The suspension fixture inspection was intercepted by the
candidate flyout and is not an exact text assertion. Product failures and
repro steps are in [Known issues](KNOWN-ISSUES.md#windows-validation-and-release-support).

Cleanup closed the test apps and Shixin processes, stopped the clipboard
observer, removed temporary tasks and restored the original English/US
language list. **Fail:** limited unregistration returned `0x80004005` and
left the machine TIP registered. Elevated unregistration also returned this
error but removed that machine entry; restoring the original language list
removed the remaining user TIP cache. **Pass:** final machine/user TIP keys
and the user COM class are absent, no Shixin processes or test tasks remain,
and the ordinary account's `AppData\Roaming\Shixin` directory was never
created. Evidence: `logs/normal/normal-{unregister,elevated-unregister}.{log,json}`,
`normal-after-{limited,elevated}-unregister.json` and `normal-cleanup.json`
in the same directory. The VM is left running at 2048 MiB with UAC enabled;
scratch data and evidence are preserved. The earlier UAC-disabled logs are
also preserved under `logs/high-integrity/`.

### Windows app matrix and evidence

For each run record the source commit, Windows build, guest architecture,
application version and architecture, DPI, selected keyboard, and settings.
Keep screenshots under the external Windows state directory's `shots/`,
logs under `logs/`, and private setup details under `notes/`. Enable
[explicit Windows logs](windows.md#collecting-development-logs) before a
run. Attach evidence
paths to every result; use **pass**, **fail**, **blocked**, or **not run**.
A screenshot of registration alone does not pass a typing test.

Repeat the composition checks above in each available target:

| Application | Targets |
|---|---|
| Notepad | Fresh document, selected-text replacement, new document, save dialog |
| WordPad | Fresh document if installed; otherwise record not available |
| Edge and Chrome | Address bar and an ordinary multiline text field; separate password and PIN checks |
| Explorer | File rename and search box |
| Start | Search box; record any low-integrity/AppContainer restriction |
| Word/Office | Document if installed; otherwise record not available |

In a fresh ordinary field type `nihao`, Space, select all and copy; assert
exactly `你好`. Reset the document between selection cases, and record the
visible candidate order before checking digit and numpad selection, arrows,
mouse clicks and paging. Check Num Lock on and off separately. Press Shift
alone while composing: record whether the inline code is finalized once and
the expected Chinese/English mode takes effect; Shift combined with another
key must preserve the shortcut. Check Shift again while idle.

Leave an uncommitted composition, switch apps and return; check that text
is finalized in the original field only and no stale panel follows the new
focus. Repeat with selected text already present, then with an ordinary field
followed by a password/PIN field. Screenshot candidate placement at the
screen edges and each tested DPI; list mixed monitors as not run on a
single-display VM. Confirm clicking the panel preserves keyboard focus.

Open Settings from the tray, inspect its Windows 开始使用 guide, and change
layout, punctuation and appearance. Confirm the saved setting, live engine
behavior and host appearance independently. Stop and restart the engine,
then change a setting requiring a generation rebuild during a typing burst;
record literal fallback keys and any missing or duplicated commits.

Before and after typing, capture process IDs, CPU time and working sets for
`Shixin`, `shixin-engine`, `shixin-settings` and the test application hosting
the TSF DLL. Compare CPU-time deltas over a stated idle interval and during
a stated burst; a single cumulative CPU reading is not an idle-usage result.
Preserve the native build/check log and note every skipped dependency,
application or security scenario. Put reproducible product failures in
[Known issues](KNOWN-ISSUES.md#windows-validation-and-release-support).
