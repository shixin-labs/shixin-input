# M0 spikes 2 + 3: MyGo app as the IMK input method, async apply, candidate flyout

Date: 2026-10-26 · Tart VM `shixin-test` (macOS 26.6.2, 4 vCPU, 8 GB) on an
Apple Silicon host · librime 1.17.0 (Homebrew in the guest) · rime-ice
without Lua · MyGo v0.6.4 · purego v0.11.2 · Go 1.27.3, `CGO_ENABLED=0`

The spike's app (`spikes/m0-imk`) was removed once the product superseded
it: `cmd/shixin` is its successor, and `scripts/macos-vm.sh` drives the VM.
The engine runs in-process on a locked OS thread here; the product moves it
to `shixin-engine` (DESIGN §3.2).

## Result: works

| Question | Answer |
|---|---|
| Can a MyGo app be the IMK input method? | **Yes.** `IMKInputController` subclass registered with `purego/objc` in `App.WhenReady`, `IMKServer` created there; macOS launches the app and calls `activateServer:` / `handleEvent:client:` |
| Does returning from `handleEvent:` at once and applying text later work? | **Yes**, in TextEdit: inline marked text, commits, candidate selection by digit, Backspace and Esc while composing, Shift tap to English and back, a 300-key burst, all with correct text order and no lost keys |
| Can a MyGo flyout be the candidate window? | **Yes.** A tray-anchored, non-focusable flyout moved with `SetPosition` to the caret rect stays where it's put, follows the caret, and never takes focus: TextEdit's title bar and menu bar stay active |

Typed in TextEdit: `nihao␣` `zhongguo2` `jintiantianqizhenhao␣`
`shurufaa⌫␣` `cuowu⎋` `⇧hello␣⇧` `women␣` → **你好🇨🇳今天天气真好输入法hello 我们**,
then 14 × `jintiantianqizhenhao␣` as a burst → 14 × 今天天气真好.

## Latency (final build, 300-key burst at ~16 keys/s over VNC)

| Path | p50 | p99 | max | Budget (§3.7) |
|---|---|---|---|---|
| `handleEvent` (the app waits on us) | 15 µs | 181 µs | 212 µs | ASK cap 8 ms ✅ |
| key → candidate window updated | 1.16 ms | 3.36 ms | 3.9 ms | p99 ≤ 4 ms ✅ |
| key → text updated in the app | 4.1 ms | 8.8 ms | 16.8 ms | see finding 1 |
| ↳ engine (librime) | 0.1–0.6 ms | ~2 ms | | |
| ↳ wait for main thread (`RunOnMain`) | 57 µs | 270 µs | | |
| ↳ `insertText`/`setMarkedText` into the app | 2.8 ms | 7.0 ms | | app-bound |
| ↳ MyGo flyout update/size/position | 0.53 ms | 1.5 ms | | |

ASK timeouts: 0.

## Findings

1. **IMK client calls are synchronous round trips into the app.**
   `setMarkedText`/`insertText` cost 2.8 ms p50 in TextEdit, because the app
   lays out its text before replying. This is the same for every IMK input
   method and is not a stall caused by us. The budget therefore splits:
   **key → candidate window** (ours, gated) and **key → app text**
   (app-bound, measured but not gated).
2. **Three techniques got key → candidates from p99 47 ms to 3.4 ms:**
   - Update the candidate window *before* calling the app.
   - Query the caret rect once per composition (the marked text start
     doesn't move); querying right after `setMarkedText` waited for the busy
     app, with a 9 ms p99.
   - Coalesce during bursts: apply every commit in order, but only the last
     marked text and candidates. It didn't trigger at 16 keys/s, but it
     protects against slow apps.
3. **macOS 26 asks for consent to enable a third-party input source.** One
   dialog per source ("Allow 拾新 to enable …? The developer will be able to
   access anything you type…"). `TISEnableInputSource` alone doesn't add it
   to the user's input sources, and `TISSelectInputSource` from another
   process returns `-50`. The first-run guide must walk the user through
   System Settings → Keyboard → Input Sources → + (Chinese, Simplified).
   Calling enable again shows the dialog again, so updates must not register
   or enable anything (finding 4).
4. **Upgrade in place, without registering** (`vm.sh upgrade`,
   `-upgrade-into`).
   - **Don't delete or replace the `.app` directory.** `rm -rf` followed by
     `cp` made LaunchServices treat 拾新 as uninstalled. While it stayed
     selected but unregistered, **keystrokes were dropped**, and getting it
     back needed registration plus consent again.
   - **What works:**
     1. Stage the new bundle outside `Input Methods`.
     2. Atomically swap its `Contents` into the installed `Shixin.app` with
        `renamex_np(RENAME_SWAP)`.
     3. Stop the old instance with SIGTERM, wait for it to exit, then
        `open -g Shixin.app --args -after-upgrade`.

     No `lsregister`, no `TISRegisterInputSource`/`TISEnableInputSource`,
     and no consent dialog. The source stayed enabled and selected, even
     though every build has a new ad-hoc signature. The new build's log
     line confirmed it was the one running.
   - **First key after the restart:** the focused app still held its
     connection to the old instance and passed its next key through as
     plain text while reconnecting. `zhongguo` gave `z红果`. With
     `-after-upgrade`, the new instance re-selects its own input mode once
     its IMK server is up. IMK then reconnects the focused app, and
     `zhongguo` gave 中国.
   - **Keys typed during the restart pass through as plain text, never
     lost.** A killed IME process also makes macOS pass keys through (typed
     `ni` appeared as plain text), which supports the design's crash path.
5. The input source shows its raw ID (`im.shixin.inputmethod.Shixin.Hans`)
   in System Settings and the input menu. The bundle needs localized
   `InfoPlist.strings` (mode names) and icons.
6. Native-UI flyouts don't size to their content (MyGo `FitContent` is for
   pages only). The spike's width estimate is too small, so candidates wrap.
   The product must measure text. Cosmetic.
7. Tart + VNC is a working GUI test harness with no TCC permissions on the
   host: vncdotool sends keys and clicks and captures screenshots, and
   `tart exec` runs commands in the guest. The repo is shared read-only via
   `--dir`; replaced directories show up stale in the guest, so each build
   is copied to a fresh path. vncdotool calls ⌘ `super`, not `cmd`.

## Not covered yet

- Allocation counts per key in the IMK front (purego callbacks and
  `objc_msgSend`): only cumulative `Mallocs` were logged. Measure per key
  in M1.
- Other apps (Safari, VS Code, Terminal, Electron), secure input fields, and
  multiple displays / Retina scale.
- The engine in a separate process with IPC (M1).
- Spike 4 (user dictionary handoff between engine processes) and spike 5
  (Windows TSF async edit sessions).
