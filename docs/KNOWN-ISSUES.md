# Known issues and deferred work

Open items. Each says what's wrong, why it matters, and the intended fix.
Delete an entry when it's fixed, and mention it in the commit.

## Engine lifecycle

### Cold-cache first key exceeds the engine budget in the VM
- **Reproduced (2026-11-01):** a scratch rime-ice engine with warmed
  sessions, guest-local libraries/data and no reranking took 13.100 ms
  for its first key after `purge`, and 12.166 ms after 31 seconds idle
  plus `purge`, against the 8 ms single-key budget. Idle alone took
  5.017 ms. Keys still composed correctly.
- The first key's page-fault and warm-up costs remain unisolated. Warm
  replay latency does not establish the cold-cache requirement.

### Hot-swap handoff over budget
- **What:** spike 4 measured 28–32 ms against a 20 ms target; product
  release-to-ready measured 23–43 ms locally (30 ms isolated) and 51 ms in
  the VM. A Windows VM rebuild also took 72–93 ms release-to-ready;
  a burst preserved all 150 input groups but passed four through as literal
  `ni ` instead of converting them to `你`.
- **Decision:** accepted for M2, without patching librime. Builds
  and standby warm-up leave the old engine serving; the brief disconnected
  handoff uses the existing PASS fallback and may clear a composition.
- **Later:** lazy user dictionary attach / component caching in librime.

### Hot-swap lifecycle follow-ups
- **Scope:** the persistent coordinator starts workers from its own binary.
  Automatically adopting an updated engine executable needs an upgrade
  request/coordinator rollover; today the macOS `-after-upgrade` path
  stops the coordinator and worker and starts the new helper (verified in
  the VM).
- **State:** composition, current switches and in-memory remembered app
  modes reset to settings on takeover. Only one context is prepared at
  activation; extra contexts use the cold session fallback rather than a
  fully attached standby pool.

## Front and UI

### Intermittent duplicate literal keys (user report)
- Reported `,` producing `,，`, and `xinghui` committing `x幸会`.
- The client now applies queued mode replies before key classification.
  Passed printable notifications cancel modifier tracking through release
  events rather than introducing another key-down to Rime. The engine also
  prevents notification digits from selecting a candidate in a stale
  composing context. These address duplicate-output paths, but the reported
  app/timing and desktop reproduction remain unconfirmed. Focused real-client
  and real-librime regression tests fail with the old code and pass with the
  patch, including on the MBP with its installed librime and scratch data.
  The assertions now check exact output, and the all-package Go race gate
  covers mode changes, passed-key ownership and concurrent result-ring reuse.

### Windows release gaps
- Version 0.2.0 distributes unsigned x64/ARM64 previews with manual updates.
  Their integrity checks do not authenticate the publisher, and Windows
  security policies may block them. Signed installer and in-app upgrade
  support are implemented separately.
- Current packages do not support 32-bit applications. ARM64 releases include
  an ARM64X forwarder and ARM64/x64 backends.
- See [Windows development](windows.md) for the implemented registration,
  AppContainer transport, auto spacing, startup and composition feedback.

### Windows Notepad save-dialog filename falls back to Latin
- **Reproduced (2026-11-01):** in Notepad 11.2607.14.0 on Windows 11
  ARM64 build 26300.9711, the Save as filename field passed `nihao `
  literally without a candidate panel. Selecting 拾新输入法 explicitly
  through the native keyboard picker and retrying still passed `nihao`
  literally. The matching development DLL was loaded in Notepad; ordinary
  document composition and second-page candidate selection had passed in
  the same process.
- This observation used source `0f4eb40`, before the later Windows runtime
  updates. The filename control's scope, callback routing and edit-session behavior
  have not been isolated. This is a compatibility failure, not evidence
  that password guards should be relaxed. Saving via a pasted literal path
  worked; it does not establish IME composition in this field.

### Windows keyboard-compartment recovery needs a focus change
- **Reproduced in the native VM fixture (2026-11-01):** ordinary 自然码
  composition produced exact `你好`. Setting
  `GUID_COMPARTMENT_KEYBOARD_DISABLED` to 1 then passed `nihk` literally,
  with no engine keys, as required. Clearing it succeeded, and reading the
  same focused Edit control's compartment returned `VT_I4` value 0, but
  subsequent `nihk ` still passed literally with no engine-key increase.
- Moving focus to another field and back restored exact `你好` without
  changing profiles, settings or processes. The initial fixture and a
  second run with explicit compartment readback showed the same behavior.
- This observation used source `0f4eb40`, before the later Windows runtime
  updates. The missing state/focus notification has not been isolated. Clearing the
  compartment does not immediately restore the input service in that context.

### Settings status before the first poll
- On opening Settings, 开始使用 can briefly show that Shixin is not added,
  and 性能 can show that the engine is not running, before the first
  background probe completes. In the VM both later changed to the correct
  live state without installing or restarting anything.
- Show a loading/unknown state until the first probe completes, rather
  than presenting a failed state during that interval.

### Update download/apply errors lose their failure state
- **Reproduced in the VM (2026-11-01):** with signed 0.1.2 offering 0.1.3,
  reject the download connection through a process-scoped proxy. Settings
  returns to 下载并安装 without an error or Retry control. Repeated native
  button activation reproduced it; the proxy logged the rejected GitHub
  connection, the installed version remained 0.1.2 and no download files
  remained.
- **Cause in the current source:** `installUpdate` calls the cleanup
  `cancel()` before checking `ctx.Err()`. That makes the context look
  canceled even when the download or apply helper failed independently, so
  the state returns to the available offer instead of showing the failure
  and Retry control.
- Capture cancellation state before cleanup so an independent download or
  apply error keeps its failure state.

### Settings app gaps
- **Not built yet:** skin editor and the 同步/云输入 pages (M5).
- Dictionary migration/backup/clear, candidate pins and native filter controls
  are implemented. Their Go unit tests pass in the race gate; native filter
  builds/integration and GUI snapshots remain unvalidated.
- **性能 page:** shows the engine's own numbers only. The front's
  `handleEvent` and key→candidates histograms (and ASK timeouts, the stall
  counter) stay in its log; the front could report them to the engine for
  the page to show.

## Auto spacing

### Secure fields that aren't detected
- **What:** a field counts as secure when the focused app holds Secure
  Event Input. So Terminal's own Secure Keyboard Entry makes Terminal
  English while it's on, and a custom password control that neither
  enables secure input nor restricts input sources isn't detected.

### macOS doesn't switch back after a password field
- **What:** in a password field macOS switches to a Roman source (U.S. or
  ABC) itself, and Shixin never sees the keys. Leaving the field, macOS 26
  in the VM keeps the Roman source instead of returning to 拾新. Apple's
  own Pinyin behaves the same way there (Safari, 2026-10-26), so it isn't
  caused by Shixin, but it hasn't been checked on a physical Mac.
- **Fix, if real Macs do it too:** remember that the switch came from a
  secure field and reselect 拾新 when an ordinary field of the same app
  gets focus.

### Linux
- **What:** the fcitx5 client still needs the spacer and must pass
  `internal/client/testdata/spacing.txt`, using surrounding text where
  available. The Go and Windows TSF clients implement auto spacing.

## Tests

### Replay latency gates under parallel test runs
- **What:** `TestReplay`'s latency gates pass when the engine package runs
  alone, but fail now and then when other packages' tests run alongside on
  a Mac. Seen: reranking adding 672 µs to the 简拼 p99 (gate 500 µs) in a
  full `go test ./...` (2026-10-28), and a cold first key of 8.37 ms (gate
  8 ms) in the `rerank` replay with the five other packages of
  `scripts/ci.sh macos` (2026-10-30).
- **Why:** the other tests compete for the CPU. CI no longer runs the
  timing gates (`SHIXIN_PERF_TOLERANCE=off`): on GitHub's runners they
  failed the same way even at twice the budgets.
- **Mitigation:** `scripts/ci.sh` and the documented full-suite commands
  now serialize test packages (`-p 1`). Direct multi-package test runs also
  need that flag when timing gates are enabled.
- **Remaining:** an isolated replay on 2026-11-01 still hit a 12.46 ms
  first-pass key in mixed/fuzzy pinyin (8 ms gate); the immediate isolated
  recheck passed every gate. Package serialization removes test-package
  competition but does not explain that first-key spike. Keep investigating
  warm-up versus OS scheduling without raising or disabling the local gates.
