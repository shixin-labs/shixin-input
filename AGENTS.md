# AGENTS.md

Guidance for contributors and coding agents working in this repository.

## Project

拾新输入法 (Shixin Input) is a cross-platform Chinese input method (macOS
now; Windows and Linux planned) built on librime, with Sogou-level
usability. Documents:

- [`docs/DESIGN.md`](docs/DESIGN.md): the design. Read it before making
  architectural changes. If a change contradicts the design, update the
  design in the same change, or discuss it first.
- [`docs/KNOWN-ISSUES.md`](docs/KNOWN-ISSUES.md): deferred work and known
  problems. Remove an entry when you fix it.
- [`docs/manual-tests.md`](docs/manual-tests.md): GUI checks to run in the
  test VM.
- [`docs/sogou-comparison.md`](docs/sogou-comparison.md): Sogou's Mac
  settings compared with Shixin's.
- [`docs/macos-input-menu-recovery.md`](docs/macos-input-menu-recovery.md):
  when macOS's input menu or an app's cache loses Shixin.
- [`docs/spikes/`](docs/spikes): experiment reports (code in `spikes/`).

Update this file as code, commands and conventions are added.

## Status

M1 (macOS MVP) is done; M2 (DESIGN §10) is in progress. Built so far:

- **Windows development front:** C++ TSF DLL, background pipes, bounded
  policy decisions, queued edit sessions, password/PIN guards and
  registration exports; a Go candidate/tray/supervisor host uses the shared
  settings app and candidate views. x64/ARM64 builds and native tests are
  configured in CI. ARM64 VM builds and Edge typing have been exercised;
  Notepad, Explorer search/rename, Edge focus and candidate clicks have
  also been exercised; Start search, per-user registration and release packaging remain open. See
  [`docs/windows.md`](docs/windows.md).

- **Input:** full pinyin and seven double-pinyin layouts (including 自然码)
  with keyboard charts; optional full pinyin inside every shuangpin layout
  (off by default; compiled into the spelling prism); fuzzy pinyin; the
  native date, number, Unicode and calculator translators.
- **Engine:** the offline builder, safe generation hot swap, bundled
  prebuilt dictionaries, and offline menu capture for evaluation.
- **macOS front:** auto spacing, secure-field detection, candidate window
  placement, the caret mode bubble, committing inline text on focus loss,
  and the double-click installer and in-place upgrader.
- **Settings app** (DESIGN §6; gaps in KNOWN-ISSUES): per-app modes,
  custom phrases, second/third-candidate shortcuts, the default
  punctuation style, a candidate font picker, forgetting learned words,
  skins and the performance page.
- **智能排序** (context-aware reranking, DESIGN §7.2), phases 1–2:
  `shixin_rerank` in librime-shixin, the engine's hold of a key's result
  (≤ 16 ms, otherwise rime's order; a menu never changes once shown), and
  `shixin-ai` running ERNIE-4.5-0.3B on Metal through llama.cpp. Its own
  settings page turns it on live and downloads the model (pinned SHA-256;
  `SHIXIN_AI_MODEL` overrides the path in development). On a machine too
  slow for the model, the engine stops holding results.

## Layout

| Path | What |
|---|---|
| `cmd/shixin-engine` | Persistent coordinator and engine workers; `-capture` serves menu capture |
| `cmd/shixin-builder` | Offline config/dictionary compiler |
| `cmd/shixin` | macOS front / Windows candidate host: IMK, candidate window, input menu, installer, supervisor |
| `cmd/shixin-settings` | Settings app |
| `cmd/shixin-ai` | 智能排序 scorer |
| `internal/{rime,engine,policy,ipc,client,keymap,settings,generation,lifecycle,perf,logx,i18n,rerank,ai,llama,models,netclient,update}` | Shared packages (DESIGN §5.1) |
| `internal/ui/{brand,candview,fonts,modeview,skin}` | MyGo views shared by the front and the settings app |
| `frontends/windows-tsf` | Thin C++ TSF COM server, policy/codec core and tests; CMake for Windows x64/ARM64 |
| `plugins/librime-shixin` | Native C++ librime components |
| `third_party/librime`, `third_party/llama.cpp` | Submodules (our librime fork; llama.cpp, MIT): `git submodule update --init --recursive` |
| `packaging/macos` | Info.plist, localizations and icons for Shixin.app |
| `scripts` | Data fetch, native builds, bundling, packaging, CI, the test VM |
| `tools/vm-input` | Test-only native keyboard/clipboard helper for the VM; not bundled |
| `tools/menu-capture` | Offline menu capture from public codes: runs `shixin-engine -capture` on a validated warm-only generation with its own scratch user dir and socket. It never promotes or opens a live user dictionary; its output is evaluation and training data, not typing logs |
| `spikes` | Throwaway experiments; results in `docs/spikes/`. Product code must not import from it |

## Hard rule: typing never stalls

Performance is a correctness requirement here, not a nice-to-have. A change
that can make a user's app wait on Shixin is a bug, even if everything else
works. Before writing hot-path code, re-read DESIGN §3. In short:

- **App threads never wait on the engine** except for `ASK` keys, which have
  a hard budget (default 8 ms) and then pass through. Never add a
  synchronous round trip, retry loop, lock, or connect/reconnect on the key
  path in any client (macOS front, TSF DLL, fcitx5 addon).
- **librime runs only in `shixin-engine`, on its single locked OS thread.**
  Don't call librime from any other goroutine or process. Exception:
  `shixin-builder`, which compiles data and never touches the user
  dictionary.
- **Never block the engine thread** with slow work: deploys, sync, file
  I/O, network, logging that flushes, or waiting on channels with no
  timeout. Slow work goes to the builder, background goroutines, or the hot
  swap (§3.5).
- **Zero heap allocations per key** in the engine key path and the client
  cores. Reuse buffers. Keep `testing.AllocsPerRun` tests green, and add
  them for new hot-path code.
- **Never create windows, load fonts, or open files on the key path.** Warm
  them up at startup.
- **Key policy:** assign `EAT` only when librime's outcome is certain;
  otherwise use `ASK`. A wrong `EAT` on a non-printable key is a bug.
- **No Lua, anywhere.** Don't add `lua_*` components, ship librime-lua, or
  enable librime's dynamic `plugins` loader. Engine lists are generated by
  the settings compiler. Features rime-ice implements in Lua go into
  `plugins/librime-shixin` as native C++ components (DESIGN §3.8).
- **Never let two processes open the same Rime user dictionary.** Only the
  engine holding Shixin's engine lock may create sessions with a user
  dictionary. A standby generation validates a default list containing only
  warm schemas, with user dictionaries disabled in every translator
  namespace, before creating any session. It changes cached schema IDs to
  real IDs only after acquiring the lock; deployed files stay warm-only.
  Our librime also rejects recovery on lock/I/O errors and holds LevelDB's
  lock during repair and archiving, but single ownership remains required
  (DESIGN §3.5, spike 4).
  This includes tests and tools: give them their own scratch user dir.
- **No callbacks from librime threads into Go.** The notification handler
  stays unused; poll status instead.
- **Don't let the OS throttle us.** Keep the App Nap / EcoQoS opt-outs and
  the thread priorities in place.
- **If you touch the hot path, run the latency and allocation benchmarks**
  and report the numbers. Don't trade latency for convenience.

## Architecture boundaries

- `cmd/shixin-engine`, `cmd/shixin-builder`, `cmd/shixin-ai`,
  `internal/engine`, `internal/rime`, `internal/ipc`, `internal/policy`,
  `internal/client`, `internal/settings`, `internal/generation`,
  `internal/lifecycle`, `internal/rerank`, `internal/ai`, `internal/llama`,
  `internal/models`, `internal/netclient`, `internal/update`,
  `internal/dynlib` and `internal/process` **must not import MyGo** (`scripts/ci.sh go`
  checks). MyGo is only for UI processes (`cmd/shixin`,
  `cmd/shixin-settings`, `internal/ui/...`).
- Bind librime with `purego`. **No cgo** in Go code (`CGO_ENABLED=0`
  builds). If you think cgo is unavoidable, stop and ask.
- **Language split:** C++ only for code that must run inside librime's
  candidate pipeline (`plugins/librime-shixin`) and for the OS input
  plugins. Everything else is Go. Don't add a third language.
- `plugins/librime-shixin` components are prefixed `shixin_`. They meet
  the engine latency budget, do no I/O or allocation after initialization
  on the common path, and come with C++ unit tests plus engine-level tests.
  They are built and tested with AddressSanitizer/UBSan in CI, and their
  parsers are fuzzed.
- The C++ clients (`frontends/windows-tsf`, `frontends/fcitx5`) stay thin:
  key mapping, policy lookup, IPC, applying results. No input logic, no
  Rime, no UI.
- **IPC protocol changes are append-only and versioned.** Old TSF DLLs stay
  loaded in running apps after an update. Update the golden vectors in
  `internal/ipc/testdata` and both the Go and C++ codecs together.
- **The settings app defines the supported configuration.** Features are
  configured through `settings.json` → the Rime patch compiler. New options
  need app controls and validation. Arbitrary Rime schemas and YAML patches,
  including `shixin.user.yaml`, are unsupported. Preserve existing external
  files without applying them.

## Tech stack

- Go (version pinned in `go.mod`; MyGo requires ≥ 1.27.3), MyGo, purego.
- librime ≥ 1.10, built by us with `librime-shixin` statically linked and
  **without Lua**. Default data: rime-ice (self-contained), with
  our own engine lists.
- C++17 with CMake for `plugins/librime-shixin`, the TSF DLL (MSVC) and the
  fcitx5 addon.

## Commands

Most engine tests need our librime:
`export SHIXIN_LIBRIME=$PWD/build/librime/out/lib/librime.1.dylib`. Tests
and tools that touch Rime use scratch user dirs, never the live one.

### Build and run

| Task | Command |
|---|---|
| Fetch Rime data (rime-ice) into `build/rime-data` | `scripts/fetch-rime-data.sh` or Windows `./scripts/fetch-rime-data.ps1` (`-URL` overrides the download) |
| Build (no cgo) | `CGO_ENABLED=0 go build ./...` |
| Windows development build (PowerShell 7 + Visual Studio C++; no IME registration) | `./scripts/build-windows.ps1 -Arch amd64` or `-Arch arm64`; add `-Check` on the matching native Windows host for C++ tests, vet and short Go tests; `-SkipNative` builds only Go binaries |
| Build our librime + librime-shixin (C++ tests with `--test`, sanitizers with `--asan`) | `scripts/build-librime.sh` → `build/librime/out/lib/librime.1.dylib` |
| Build Windows librime + librime-shixin (MSVC, static dependencies, no Lua; C++ tests with `-Test`; `-Generator` / `-Jobs` override discovered defaults) | `./scripts/build-librime.ps1 -Arch arm64 -Test` → `build/windows-arm64/rime.dll` and `build/librime-windows-arm64/out/share/opencc` |
| Build llama.cpp for shixin-ai (Metal, embedded shaders, no tools or network) | `scripts/build-llama.sh` → `build/llama/lib/libllama.dylib` |
| Make the 8-bit model from ERNIE-4.5-0.3B-Base (Apache-2.0; weights downloaded into their own dir under `build/`) | `PYTHONPATH=third_party/llama.cpp/gguf-py python third_party/llama.cpp/convert_hf_to_gguf.py --outtype q8_0 --outfile build/models/ernie-4.5-0.3b-base-q8_0.gguf <hf snapshot dir>` |
| Run the engine locally (dev: Homebrew librime, `build/rime-data`; without `-user`, `-settings` and `-socket` it uses the real Shixin user dir and refuses to start while an installed Shixin runs) | `go build -o build/bin/ ./cmd/shixin-engine ./cmd/shixin-builder && build/bin/shixin-engine -log -` |
| Run the engine with 智能排序 (scratch user dir; shixin-ai next to shixin-engine) | `go build -o build/bin/ ./cmd/shixin-engine ./cmd/shixin-builder ./cmd/shixin-ai && SHIXIN_AI_MODEL=$PWD/build/models/ernie-4.5-0.3b-base-q8_0.gguf build/bin/shixin-engine -lib … -user /tmp/x/user -settings /tmp/x/settings.json -socket /tmp/x/run/engine.sock`, with `"rerank": "standard"` in that settings.json |
| Build an isolated generation (output must not exist; no live user dir argument) | `build/bin/shixin-builder -lib "$SHIXIN_LIBRIME" -data build/rime-data -settings /path/to/snapshot.json -output /tmp/shixin-generation` (`-all` for packaging) |
| Capture offline menus from public codes (warm-only scratch engine, no learning) | `go run ./tools/menu-capture -engine build/bin/shixin-engine -generation /tmp/shixin-generation -data build/rime-data -lib "$SHIXIN_LIBRIME" -profile SHA256 -input codes.jsonl -output menus.jsonl -limit 50` |
| Portable TSF core tests (Go golden vectors, timeout/focus safety, queues, allocation and parser stress) | `cmake -S frontends/windows-tsf -B build/tsf-tests && cmake --build build/tsf-tests && ctest --test-dir build/tsf-tests --output-on-failure` |
| librime latency spike (needs rime data) | `go run ./spikes/m0-rime` (`-lua` for the Lua comparison) |
| User dictionary handoff spike | `go run ./spikes/m0-handoff` (scratch copies under `build/spike/m0-handoff`) |

### General checks

| Task | Command |
|---|---|
| Test / vet / format | `go test ./...` · `go vet ./...` · `gofmt -l .` |
| All CI gates locally | `scripts/ci.sh go\|macos\|asan\|asan-full\|fuzz` (`SHIXIN_PERF_TOLERANCE` scales latency budgets; `off` disables them, as CI and the ASan runs do) |
| librime and the engine under ASan/UBSan: per push without the long rime-ice runs, or every engine test | `scripts/ci.sh asan` · `scripts/ci.sh asan-full` |
| Engine + client tests incl. real rime-ice deploy (slow) | `go test ./internal/...` (`-short` skips the rime-ice deploy and replay) |
| Tests against our librime (以词定字 etc.) | `SHIXIN_LIBRIME=… go test ./internal/engine` |
| C++ tests of librime-shixin only (after a build) | `build/librime/out/plugins/shixin/shixin_test` |
| Fuzz librime-shixin's parsers | `scripts/ci.sh fuzz` (libFuzzer where available; with Apple clang, `test/fuzz_driver.cc` feeds random and grammar-built inputs) |
| Punctuation policies, numeric history across toggles, defaults and latency/allocation benchmarks for both styles | `go test -v ./internal/engine -run 'Test(EnglishPunctuationPolicies\|PunctuationToggleAfterDigit\|KeyPathDoesNotAllocate\|RimeIce)' -bench '^BenchmarkKey' -benchmem` |

### Engine lifecycle and data

| Task | Command |
|---|---|
| Handoff safety, failure/recovery and first-run timing (scratch dirs only) | `SHIXIN_LIBRIME=… go test -v ./cmd/shixin-engine` (`-short` skips real rime-ice builds) |
| librime lock-conflict regression: two processes and learned-word preservation | `go test -v ./internal/rime -run TestUserDictionaryLockConflict` |
| Abandoned generation cleanup and path preservation (no librime) | `go test ./cmd/shixin-engine -run TestGenerationCleanup` |
| Prebuilt prism reuse for page size, other options and fuzzy spelling | `go test -v ./internal/generation -run TestPrebuiltReuse` |
| Saved-generation compiler compatibility / rebuild after new defaults | `go test ./internal/generation -run TestMatchesSettings` · `go test ./cmd/shixin-engine -run TestCoordinatorRebuildsOldCompiler` |
| Offline capture protocol tests / actual scratch-generation replay | `go test ./tools/menu-capture ./cmd/shixin-engine -run 'Test(Capture\|ReadCodes\|ChangingCandidate)'` · `SHIXIN_CAPTURE_INTEGRATION=1 go test ./tools/menu-capture -run TestCaptureRealGeneration` |

### Input features (against our librime)

| Task | Command |
|---|---|
| Verify shuangpin charts against real spelling rules (scratch dictionaries) | `go test -v ./internal/engine -run TestShuangpinCharts` |
| Full-pinyin fallback, all layouts, mixed/fuzzy/ü spellings, ambiguous codes and zero Go allocations | `go test -v ./internal/engine -run 'Test(MixedPinyinSpellings\|RimeIceMixedPinyin\|ShuangpinCharts)'` |
| Candidate shortcuts across all layouts, page/highlight behavior, native translators, idle policy and zero Go allocations | `go test -v ./internal/engine -run '^TestRimeIce'` |
| Forgetting learned words/ranking, both shortcuts and off, idle policy, zero Go allocations and persistence across fresh processes (scratch dictionaries) | `go test -v ./internal/engine -run '^TestForgetCandidatePersistence$'` |

### 智能排序

| Task | Command |
|---|---|
| With a fake scorer: held results reordered, timeouts, superseded results, never-held keys, per-app context, zero allocations | `go test -v ./internal/engine -run TestRerank` · `go test ./internal/rerank` (`SHIXIN_TEST_RERANK=off` runs the engine tests and `BenchmarkKey` without it) |
| Engine cost on rime-ice: export, hold and applying orders (简拼 p99 gate, in the replay corpus run) | `go test -v ./internal/engine -run TestReplay` |
| shixin-ai: binding, scorer, reference agreement, accuracy and latency gates (run alone; the gate skips with `-short`), protocol and Remote | `go test -v ./internal/llama ./internal/ai ./internal/rerank` (the reference-agreement test needs `build/laya/abbr_ref.jsonl` from the 智能排序 spike's code, which lives outside this repository, and skips without it) |
| Model download from the published URLs (network; off by default) | `SHIXIN_TEST_NETWORK=1 go test ./internal/models` |
| End to end: rime-ice menus, Remote, server and model in one child process | `go test -v ./internal/engine -run TestRerankEndToEnd` |

### macOS front and settings app (headless)

| Task | Command |
|---|---|
| Secure-field decisions/client tests / native flag, cached and resolve costs | `go test ./internal/client -run Secure` · `go test ./cmd/shixin -run Secure -bench BenchmarkSecure -benchmem` |
| Local preedit finalization and late-result races | `go test ./internal/client -run 'Test(FocusOut\|Finish\|KeyAfterLocalCommit)'` |
| macOS shortcut modifier routing/reset cost (no event posting) | `go test ./cmd/shixin -run TestShortcutModifier -bench BenchmarkModifierSpacingReset -benchmem` |
| Candidate placement and panel measurement | `go test ./cmd/shixin -run TestCandidate` · `go test ./internal/ui/candview -run TestMeasure` |
| Candidate handoff/cache/frame allocation and latency gates | `go test ./cmd/shixin -run TestCandidate -bench BenchmarkCandidateUpdate -benchmem` · `go test ./internal/ui/candview -run 'Test(State\|String\|Selection\|Candidate)' -bench BenchmarkCandidateFrame -benchmem` |
| Candidate font picker, fallback, installed-font measurement and warm-up | `go test ./cmd/shixin-settings -run 'Test(Font\|UnavailableFont)'` · `go test ./internal/ui/fonts ./internal/ui/candview` (`TestMeasure` checks every installed family on macOS) |
| Mode bubble lifecycle/allocation tests, tracking cost and all skin/language snapshots | `go test ./cmd/shixin -run TestMode -bench BenchmarkModeIndicator -benchmem` · `go test ./internal/ui/modeview -snapshot DIR` |
| Render every settings page and shuangpin chart (Chinese/English, light/dark) / every skin to PNGs | `go test ./cmd/shixin-settings -snapshot DIR` · `go test ./internal/ui/candview -snapshot DIR` (English settings PNGs add `-en`; charts also include minimum-window scrolled views) |
| macOS IMK connection-name agreement with the installed OS (scratch bundle, no registration) | `go test ./cmd/shixin -run TestConnectionName` |
| Stale input-source cache choice and cleanup after app exit (scratch dir) | `go test ./cmd/shixin -run TestRemoveStaleCaches` |
| In-app update: release parsing, version order, download checks (httptest, no network) / the front's verification decisions with fake `codesign`, `spctl` and `plutil` / the 关于 update states | `go test ./internal/update ./internal/netclient` · `go test ./cmd/shixin -run 'Test(Team\|VerifyUpdate)'` · `go test ./cmd/shixin-settings -run 'Test(CheckUpdate\|FinishCheck)'` (`-snapshot DIR` renders every state) |
| Cache-marker refresh preserves clipboard types and change count (**VM only**, seeds the clipboard) | `SHIXIN_TEST_CACHE_REFRESH=1 ./shixin-front.test -test.run TestCacheRefreshPreservesClipboard` (`go test -c ./cmd/shixin`) |

### macOS app, packaging and install

| Task | Command |
|---|---|
| Build Shixin.app | `scripts/bundle-macos.sh` → `build/macos/Shixin.app` (`LIBRIME=` to bundle our librime; compiles all dictionaries into `rime-prebuilt`; `SIGN_IDENTITY=` signs with a Developer ID and the hardened runtime, else ad hoc) |
| Package the release disk image (opening the app inside installs or upgrades) | `SIGN_IDENTITY="Developer ID Application: …" [NOTARY_PROFILE=…] VERSION=x.y.z scripts/package-macos.sh` → `build/dist/Shixin.dmg` (notarized and stapled with a `xcrun notarytool store-credentials` profile) |
| Render app/input-source icons only | `scripts/build-macos-icons.sh [DIR]` → `build/macos/icons` by default (ICNS and a 16-point TIFF with 1x/2x representations) |
| Installer diagnostics (in the installed bundle) | `Shixin -status [BUNDLE]` · `-added` · `-refresh` (System Settings' stale input source list) · `-repair` (stale menu and app caches, once added; refreshes running apps; waits for quit only as fallback) · `-cache-status` (running regular apps with stale caches, as JSON) · `-uninstall` |
| Verify and apply a downloaded release (the settings app runs it; installs for real, **VM only**) | `Shixin -apply-update DMG -update-version V` (in the installed bundle; logs to `shixin.log`) |

### Windows QEMU VM

See [Windows development](docs/windows.md#windows-test-vm-on-macos).
Set `SHIXIN_VMROOT` (default `~/shixin-vm`) to an existing directory.
`scripts/windows-qemu.sh` controls QEMU/HVF; `scripts/windows-vm.sh` adds
SSH, transfers and paced input. Windows state stays separate from Tart VMs.
Both helpers source `scripts/windows-env.sh`. `SHIXIN_WINDOWS_VM` selects the name
and state directories (default `shixin-win11`). Resource, port, ISO, firmware
and VNC-client overrides are listed in the [variables table](docs/windows.md#windows-test-vm-on-macos).
Windows host/engine logs default to `%LOCALAPPDATA%\Shixin\Logs\{shixin,engine}.log`.

| Task | Command |
|---|---|
| Initialize SSH key and test state directories | `scripts/windows-vm.sh init-state` |
| Create/start QEMU / request shutdown / inspect the selected Windows VM | `scripts/windows-qemu.sh start\|stop\|status` |
| Start / request shutdown / inspect through the SSH helper | `scripts/windows-vm.sh boot\|stop\|status` |
| Set RAM / CPU / new disk size at boot | `SHIXIN_WINDOWS_MEM=6144 SHIXIN_WINDOWS_CPUS=4 SHIXIN_WINDOWS_DISK=64G scripts/windows-vm.sh boot` |
| Run literal PowerShell in the guest / transfer a file | `scripts/windows-vm.sh exec 'COMMAND'` · `put LOCAL GUEST` · `get GUEST LOCAL` |
| Guest screenshot / VNC input | `scripts/windows-vm.sh shot NAME` · `keys KEY...` · `type TEXT` · `click X Y` |

### The Tart VM

| Task | Command |
|---|---|
| Shixin.app in the Tart VM | `scripts/macos-vm.sh boot\|vncenv\|build\|upgrade\|logs\|burst\|input ARGS\|shot NAME\|vnc ARGS\|exec CMD` |
| Upgrade the installed IME without re-registering | `scripts/macos-vm.sh upgrade` (in-place `Contents` swap, see DESIGN §5.5) |
| Native VM keys, shortcuts and exact clipboard assertion (U.S. source for literal ASCII) | `scripts/macos-vm.sh input key cmd+n wait 300ms type '>:' key cmd+a key cmd+c expect '>:'` |
| Native VM input parser, modifiers and assertion tests (no event posting on the host) | `go test ./tools/vm-input` |
| Menu list clear and rebuild (clears the real menu list: **VM only**) | in the VM: `SHIXIN_TEST_MENU=1 ./shixin-front.test -test.run TestMenuListRebuild` (`go test -c ./cmd/shixin`) |

Add commands here as they are created.

## GUI testing

Test the front in a macOS VM, never on your own Mac: installing an input
method needs consent dialogs and changes your input sources.
`scripts/macos-vm.sh` drives a [Tart](https://tart.run) VM named
`shixin-test`. Set `SHIXIN_VMROOT` (default `~/shixin-vm`) to a directory
holding Tart's data in `tart/` (optionally Tart itself in `bin/tart.app`)
and the VM's working files in `shixin/shixin-test/`: a vncdotool venv in
`venv/`, `vnc.env` (written by `vncenv`) and `shots/`. The repository is
shared into the guest read-only.

- `burst` types into a fresh TextEdit document and asserts the exact
  copied text with native Cmd+N/A/C. Select Shixin in Chinese mode first.
- `input` builds and runs `shixin-vm-input` in the guest: `key CHORD`,
  `type ASCII`, `wait DURATION`, `expect TEXT`; `-delay` (10 ms) and
  `-timeout` (2 s) precede the commands. Keys use the US physical layout
  and normal input-method routing; `type` accepts printable ASCII, tab and
  newline, while `expect` accepts UTF-8. Use `key shift` to tap Shift, and
  `key cmd+plus` for Cmd+Plus. First use may need Accessibility enabled
  in the VM's System Settings; the host needs no permissions.
- `vnc` sends clicks and `shot NAME` captures the screen; `exec` runs a
  guest command.
- Logs: `~/Library/Logs/Shixin/{shixin,engine,ai}.log`. Touch
  `~/Library/Application Support/Shixin/debug` for key-code diagnostics.
- Feature checks to repeat by hand are in
  [`docs/manual-tests.md`](docs/manual-tests.md).

## Troubleshooting

- **Shixin added but missing from the input menu, or an app keeps
  switching back to ABC:** see
  [`docs/macos-input-menu-recovery.md`](docs/macos-input-menu-recovery.md).
  The input menu has its own cached list
  (`AppleInputSourcesInUIOrderPasteboard`), and apps keep their own input
  source caches; `Shixin -repair` fixes both. Successful TIS enable/select
  calls don't prove the menu works.
- **C++ builds must never see Homebrew or `/usr/local` headers:**
  `build-librime.sh` ignores those prefixes and fails if they appear.
  Once, gflags from Homebrew put `/opt/homebrew/include` first, librime
  compiled against OpenCC 1.4 headers while linking the bundled 1.1.9, and
  crashed on the first key.
- **Homebrew's librime** (`/opt/homebrew/opt/librime`) is fine for tests
  that don't need librime-shixin (they skip). It ships librime-lua in its
  `rime-plugins` dir, so never enable the `plugins` module against it.
  Product builds use our own librime.

## Conventions

- **Code reads like its surroundings:** standard Go style (`gofmt`,
  `go vet`), small packages, doc comments on exported identifiers, and
  errors wrapped with context. No panics outside `main` and init-time
  programmer errors.
- **Platform code** goes in `_darwin.go` / `_windows.go` / `_linux.go`
  (or build-tagged) files. Keep the shared logic platform-free and
  testable: the macOS front keeps its decisions in plain Go files with
  headless tests (`candidate_placement.go`, `mode_indicator.go`, `secure.go`).
- **Tests:** new behavior comes with tests. Engine tests run against real
  librime with a small test schema. Settings compiler changes need golden
  YAML that librime actually deploys. IPC changes need golden vectors.
- **User-facing text** is Chinese first, with English available. Don't
  hard-code strings in views; use the i18n catalog (settings app strings
  are in `internal/i18n/settings.go`).
- **Brand artwork** is drawn in `internal/ui/brand`, shared by the settings
  app, temporary tray anchor and macOS packaging. ICNS/TIFF/PNG files are
  generated under `build/`; keep the Go artwork source, not generated assets.
- **Settings UI:** follow macOS System Settings. Pages are built from the
  helpers in `cmd/shixin-settings/widgets.go` (`group`, `row`, `switchRow`,
  `memberSwitchRow`, `segmented`, `selectID`) so they keep one look: every
  on/off option is a row with a switch, a choice is a pop-up or a
  segmented control, and a long set of options goes in a details sheet
  (`ui.Modal`, like 模糊音's). Color marks only what needs attention
  (warnings, errors, progress); settled states are muted. macOS and
  Windows share every page; what differs (the front's path, the guide's
  steps, updates) is in `platform_darwin.go` / `platform_windows.go`, and
  `-snapshot` renders the Windows guide too. A new setting needs: the
  field in `settings.Settings` (with its default and validation), the
  compiler output if it reaches Rime (golden YAML plus a check in
  `TestRimeIceOptions`), and a control. Look at a change with `-snapshot`
  before calling it done.
- **Downloads are untrusted data.** Put them in their own directory under
  `build/` and never run them. Rime data is loaded by librime; that's
  expected. Don't commit `build/`.
- **Licensing:** the project is GPL-3.0-or-later. Only copy code from
  compatible licenses (Squirrel and Weasel are GPL-3.0; librime is BSD-3),
  keep attribution, and say where the code came from in the commit message.
- **Privacy:** no telemetry. Nothing leaves the machine unless the user
  turned it on (the 智能排序 model download; later cloud input and sync).
  Never log typed text or candidates, except in explicit debug builds.
- **Docs:** user-visible behavior and architecture go in `docs/DESIGN.md`;
  open problems in `docs/KNOWN-ISSUES.md`; GUI checks in
  `docs/manual-tests.md`. Write for outside readers: no local paths,
  machine names or personal anecdotes.

## Git

- Repository: `github.com/shixin-labs/shixin-input`. CI
  (`.github/workflows/ci.yml`) runs `scripts/ci.sh` on every push and pull
  request: `go` and `fuzz` on ubuntu-24.04, and `macos` and `asan` as two
  parallel macos-15 jobs. Native Windows backend builds and short tests run
  on Windows x64 and ARM64. `asan-full` runs weekly and on manual dispatch. librime comes from the
  `github.com/shixin-labs/librime` fork; patches to librime go on a branch
  there, and the submodule pins it.
- Small, focused commits with imperative subjects, prefixed with the area
  (`engine: pool sessions at warm-up`): `engine`, `ipc`, `client`,
  `macos`, `settings`, `ui`, `rerank`, `ai`, `librime-shixin`, `tsf`,
  `fcitx5`, `docs`, `build`.
- Don't commit generated files, `build/`, downloaded data or secrets.
- Before sending a change: `gofmt`, `go vet`, `go test ./...` (or
  `scripts/ci.sh go`), plus the hot-path benchmarks if you touched the
  engine or a client. CI doesn't run the latency gates
  (`SHIXIN_PERF_TOLERANCE=off`): for engine, rerank or AI changes, run
  `go test -v ./internal/engine -run TestReplay` alone on a Mac and report
  the numbers.
