# Known issues and deferred work

Open items. Each says what's wrong, why it matters, and the intended fix.
Delete an entry when it's fixed, and mention it in the commit.

## Engine lifecycle

### Hot-swap handoff over budget
- **What:** spike 4 measured 28–32 ms against a 20 ms target; product
  release-to-ready measured 23–43 ms locally (30 ms isolated) and 51 ms in
  the VM.
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
  modes reset to settings on takeover. Only the macOS front's single
  context is prepared at activation; extra contexts use the cold session
  fallback rather than a fully attached standby pool.

### One input context per front
- **What:** the macOS front uses one engine session for all apps, so
  switching apps clears the composition. Per-app default and remembered
  modes now work on top of it (the engine applies them on `FocusIn`).

## Front and UI

### Windows validation and release support
- **Implemented, partly VM-verified:** TSF COM server, queued edit sessions,
  policy speculation and bounded waits, password/PIN guards, candidate/tray
  host, registration exports, and backend/settings builds for
  x64/ARM64. Native CI jobs include the COM factory smoke test; portable
  client tests run on Linux and under sanitizers locally.
- **ARM64 VM run (2026-10-31):** QEMU/HVF, Windows 11 Pro, native builds,
  librime without Lua, registration, and Edge typing were exercised.
  Application versions: Notepad 11.2607.14.0, Edge 156.0.4314.58,
  Explorer 10.0.26100.8117 and SearchHost 2608.25008.200.0 on Windows
  build 26300.9457. Settings and its Windows guide render; layout, theme
  and punctuation changes can apply live. The remaining validation and
  packaging work below prevent treating the port as ready. See the
  [Windows status](windows.md#status-2026-11-01).
- **Missing:** native llama build automation, a signed installer and
  in-place updates (关于 links to the release page meanwhile).
- **Setup and input:** the guide queries per-user enablement with `-added`;
  successful registration adds logon startup, and unregistration removes
  this copy's startup command. TSF supplies composition display attributes;
  the host shows confirmed mode changes beside the caret. Saved desktop app
  IDs also match executable paths, with ambiguous aliases left unresolved.
- **Security and validation:** Authenticode checks are required before release.
  AppContainer transport has capability-free sandbox coverage; Start search
  typing also passes in the low-integrity ARM64 SearchHost. Real text-store
  behavior,
  focus/security transitions, migration, DPI placement and mouse selection
  need broader application coverage. ARM64
  packaging must also address x64 emulated applications, which need a
  matching DLL. See [Windows development](windows.md).

### Oversized candidate panels on macOS
- **What:** normal horizontal and vertical panels are measured and clamped
  to the caret screen's work area. A panel larger than that work area is
  pinned to its origin; it cannot fit in full. The pre-created transparent
  flyout also has a fixed 1400×560-point content area.
- **Impact:** unusually long candidates/comments, ten vertical rows at
  a large font on a small screen, or a font with unusually tall metrics
  can still be clipped.
- **Fix:** constrain the layout and add scrolling or another overflow UI.
- **Verification:** placement has unit tests and works in the VM (the
  panel flips above the caret at the bottom of the screen); real
  multiple displays and Intel Macs are untested.

### Candidate panel measurement on Linux
- **What:** `candview.Measure` matches the laid-out panel on macOS, but
  on Linux (CI, ubuntu-24.04) rows come out about 1.5 px short at small
  font sizes, and much further off with no system fonts: MyGo's text
  measurement and layout disagree under font fallback. `TestMeasure`
  runs on macOS only.
- **Fix (M4, before the fcitx5 front uses it):** make it match there, or
  place the window from the previous frame's `Element.Bounds()`.

### Other apps not yet tested
- **Tested so far:** TextEdit, Terminal, and form fields in Safari
  (including a password field). Safari's address bar passed composition,
  candidate selection, Backspace, Escape and English URL checks in the VM,
  including URL entry after clearing Chinese text
  ([manual tests](manual-tests.md#safaris-address-bar)).
- **Still to test:**
  - Chrome
  - VS Code / Electron apps
  - Word, Pages, Notes, Slack
  - native password prompts (System Settings, sudo dialogs)

### Settings app gaps
- **Not built yet:** skin editor, .scel/text dictionary import, user
  dictionary export/clear, and the 同步/云输入 pages (M5).
- **性能 page:** shows the engine's own numbers only. The front's
  `handleEvent` and key→candidates histograms (and ASK timeouts, the stall
  counter) stay in its log; the front could report them to the engine for
  the page to show.
- **Window tested** at the VM's 1024×768 only. Native English pages are
  checked in light/dark at 880×640, with scrolling verified on long pages.
  All pages also have headless Chinese/English light/dark snapshots at
  880×720.
- **Still to test:** a real Retina display. The VM reports
  `NSScreen.backingScaleFactor == 1`, so these checks cover 1× only.

## Auto spacing

### App compatibility checks incomplete
- **What:** auto spacing (DESIGN §7.1) is unit-tested against the shared
  vectors and the client core. Safari's address bar has initial deletion,
  English URL and Chinese/English boundary checks; the full VM app matrix
  (TextEdit, Notes, Safari, Chrome, VS Code, Slack, Terminal) remains
  incomplete.
- **Unknowns:** whether each app answers `selectedRange`,
  `markedRange` and `attributedSubstringFromRange:` correctly and quickly
  (Electron and terminals may not; the spacer then falls back to tracking
  alone), and whether every caret move by mouse is caught by the global
  monitor (trackpad tap-to-click, Universal Control).
- **Fix:** run the matrix, measure the read times, and add apps that
  misbehave to the default `no_auto_space` list.

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

## Native translators

### Time zone changes on Linux
- **What:** `shixin_date_translator` loads the time zone once (`tzset` at
  construction). macOS's libc notices zone changes itself; glibc doesn't,
  so after travelling `sj` shows the old zone's time
  until the engine restarts.
- **Fix (M3/M4):** re-run `tzset` on the OS's time-zone-changed
  notification, off the key path (on glibc it may read files).

### Emoji among number and weekday candidates
- **What:** rime-ice's `simplifier@emoji` has no `tags`, so it adds 🔟 /
  1️⃣ between the `R` readings and `Mon.` / `Monday` between the weekdays.
  rime-ice does the same. Limiting it to `abc` would also drop emoji for
  other recognized input; decide when the filters are reworked.

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
- **Until fixed:** rerun `go test ./internal/engine -run TestReplay` alone
  before treating a failure as a regression. A fix would run the latency
  gates serially (`-p 1`) or skip them when other packages run alongside.
