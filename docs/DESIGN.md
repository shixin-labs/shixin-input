# 拾新输入法 Shixin Input — Design

Status: a living document, updated with the code (milestones in §10) ·
License: GPL-3.0-or-later

拾新 (Shixin) is a Chinese input method for macOS, Windows and Linux, built on
[librime](https://github.com/rime/librime). It aims to be as easy to use as
Sogou Pinyin (搜狗输入法): install it, pick a few options in a real settings
app, and type. Nobody should have to edit YAML. **Typing must never stall.**

**Contents:** [1. Goals](#1-goals-and-non-goals) ·
[2. Technology](#2-technology-choices) ·
[3. No-stall architecture](#3-no-stall-architecture) (rules, processes,
client model, engine, hot swap, UI, budgets, native extensions) ·
[4. Key flow](#4-key-flow-per-platform) · [5. Components](#5-components) ·
[6. Settings app](#6-settings-app) ·
[7. Features](#7-features-sogou-parity-map) (auto spacing, 智能排序) ·
[8. Engine behavior](#8-engine-behavior) ·
[9. Testing](#9-testing-and-performance-gates) ·
[10. Milestones](#10-milestones) · [11. Risks](#11-risks) ·
[12. Decisions](#12-decisions) · [13. Open questions](#13-open-questions)

## 1. Goals and non-goals

**Goals**

1. **No stalls.** An app the user is typing in never freezes because of
   Shixin, whatever the engine is doing: loading, deploying, syncing,
   crashing, or restarting. This constraint overrides every other one
   (see §3).
2. One product on all three platforms: the same candidate window, settings
   app, skins and features.
3. Sogou-level usability: everything is configured in a GUI with a first-run
   guide, and good defaults work out of the box.
4. A good dictionary out of the box: rime-ice (雾凇拼音) is the default schema
   set.
5. Private by default: no telemetry, and anything that talks to the network
   is opt-in.
6. The settings app defines the supported configuration. Every supported
   option has an app control and validation; new customization needs to be
   added to the app.

**Non-goals (v1):** handwriting, voice input, mobile, our own account/cloud
server, arbitrary user-supplied Rime schemas or YAML patches, forking Rime's
configuration model.

## 2. Technology choices

| Concern | Choice | Reason |
|---|---|---|
| Language | Go | Simple, fast to build, good stdlib for IPC, HTTP and file watching |
| App framework | [MyGo](https://github.com/egoist/mygo) for UI processes only | Native GPU-drawn UI, tray, deep links, installers, signing, auto-update |
| Engine process | Plain Go, no MyGo, no UI | Small heap, no UI work, nothing competing with librime |
| Engine | librime ≥ 1.10, built and bundled by us, **no Lua** | Same version and performance everywhere; Lua is never loaded (R9) |
| Engine extensions | `librime-shixin`, a native C++ librime plugin | Replaces rime-ice's Lua features with compiled code (§3.8) |
| librime binding | `purego` with native library loading, no cgo | Cross-compiles anywhere; every librime function takes only ints and pointers |
| Windows IME hook | C++ TSF DLL | TSF only loads in-process COM DLLs; Go must not be loaded into other apps |
| Linux IME hook | C++ fcitx5 addon | Most widely used framework; required for Wayland |
| Default data | rime-ice (self-contained: dictionaries, default.yaml, symbols), with our own engine lists | Closest to Sogou; rime-ice's Lua components are replaced |
| Context reranking | ERNIE-4.5-0.3B-Base on llama.cpp (Metal/Vulkan), purego, in its own `shixin-ai` process; opt-in | Scores candidates against committed text on the GPU/NPU; the app never waits, and a key's result waits ≤ 16 ms for its order (§7.2) |

## 3. No-stall architecture

### 3.1 Rules

| # | Rule |
|---|---|
| R1 | **An app thread never waits on the engine for keys it can classify locally.** The client decides from a local *key policy* and sends the key asynchronously. The result is applied when it arrives, typically in < 1 ms. |
| R2 | **The rare synchronous wait has a hard budget** (8 ms default). If the budget runs out, the key passes through unchanged and the client goes into degraded mode. Nothing ever waits indefinitely. |
| R3 | **librime runs only in `shixin-engine`**, on one dedicated OS thread. It never shares a process with UI, IMK or app code. |
| R4 | **Slow work never runs on the engine thread while it serves keys.** Data compilation (deploy) runs in a separate builder process. New data goes live through a hot swap to a pre-warmed engine (§3.5). |
| R5 | **No disk I/O, network, allocation or lock contention on the hot path**, in the client or the engine, beyond librime's own work. |
| R6 | **Everything slow is warmed before it is needed:** dictionaries are pre-faulted, schemas pre-loaded, sessions pooled, windows pre-created, and font glyphs pre-cached. |
| R7 | **The OS must not throttle us.** App Nap is disabled on macOS, Efficiency Mode (EcoQoS) is opted out of on Windows, and the engine thread gets interactive priority. |
| R8 | **Latency is measured on every key and gated by the tests** (§9). A regression fails them on a real Mac; CI's shared runners skip the timing gates but keep the allocation and mispredict gates. |
| R9 | **No Lua, anywhere.** librime-lua is not shipped or loaded, and no `lua_*` component may appear in a deployed schema. Features that rime-ice implements in Lua are reimplemented natively in `librime-shixin` (§3.8), or dropped. |

### 3.2 Processes

```
 clients (never block on the engine)
 ┌──────────────────────────────────────────────────────────────────────┐
 │ macOS   Shixin.app: IMK + candidate window + input menu (Go + MyGo)  │
 │ Windows shixin_tsf.dll inside each app (C++)                         │
 │ Linux   libshixin-fcitx5.so inside fcitx5 (C++)                      │
 └───────────────▲──────────────────────────────────────────────────────┘
                 │ async key stream ⇄ state stream
                 │ (unix socket / named pipe, binary protocol)
 ┌───────────────┴──────────────┐        ┌──────────────────────────────┐
 │ shixin-engine (Go, no UI)    │        │ Shixin UI (Windows only):    │
 │ librime on one locked thread │        │ candidate window + tray      │
 └───────────────▲──────────────┘        └──────────────────────────────┘
                 │ spawns / hot-swaps
 ┌───────────────┴──────────────┐        ┌──────────────────────────────┐
 │ supervisor (inside Shixin.app│        │ Shixin Settings (on demand,  │
 │ / Shixin UI / systemd)       │        │ separate process, MyGo)      │
 └──────────────────────────────┘        └──────────────────────────────┘
                                          shixin-builder (on demand):
                                          compiles Rime data for a new generation
```

| Platform | Processes | What users see |
|---|---|---|
| macOS | `Shixin.app` (IMK + candidate window + input menu + supervisor), `shixin-engine` helper in the bundle, Settings on demand | One app |
| Windows | `shixin_tsf.dll` in apps, `Shixin.exe` (candidate window + tray + supervisor), `shixin-engine.exe`, Settings on demand | One installer, one tray icon, one settings app |
| Linux | fcitx5 addon, `shixin-engine` (systemd user unit), Settings on demand. fcitx5 draws the candidates | One package |

Why the engine is its own process on every platform, including macOS:
- A librime or Lua crash, hang or GC pause can't freeze the IMK process or the
  candidate UI.
- It enables zero-downtime hot swaps for deploys and updates (§3.5).
- All platforms share one engine and one protocol.

The extra hop is ~20–50 µs, and R1 keeps it off the app's critical path.

With 智能排序 on (§7.2), the engine's coordinator also runs
**`shixin-ai`**, which holds the reranking model on the GPU/NPU. Only the
engine talks to it, asynchronously; if it is missing or slow, candidates
keep rime-ice's order.

### 3.3 The client model: classify locally, apply asynchronously

Each client (IMK front, TSF DLL, fcitx5 addon) keeps a **key policy** for the
focused session. The engine sends a fresh one with every state update. For a
key event the client looks up its disposition in the policy, in < 50 µs with
no syscalls except a non-blocking write:

| Disposition | Client action | Typical keys |
|---|---|---|
| `PASS` | Let the app have it. No IPC | English mode letters; arrows/Enter/F-keys when not composing; Ctrl/Cmd shortcuts when not composing |
| `PASS_NOTIFY` | Let the app have it, and send it to the engine asynchronously. A character waits for the ordering barrier, like `PASS` | Shift / Ctrl press and release (Rime's ascii_composer needs them to detect a tap-to-switch); digits in Chinese mode when not composing (the punctuator's digit separators read them from the commit history, so `3.5` doesn't become `3。5`; a separator such as `.` then predicts an unknown next state) |
| `EAT` | Return "handled" at once and send asynchronously. Apply commit/preedit/candidates when the result arrives | Letters in Chinese mode; while composing: letters, digits/space (select), Backspace, Esc, Enter, arrows, page keys; Chinese punctuation |
| `ASK` | Send synchronously and wait up to the budget (R2) | Anything the engine can't classify for certain, e.g. a modifier combination while composing, or a custom key binding |

**How the policy is built.** The engine builds it from the current Rime
state and the active schema's config:
- composing or not, ASCII mode, `speller/alphabet` and `initials`
- `punctuator` symbols, `key_binder` bindings, `recognizer` patterns,
  `ascii_composer/switch_key`

It is a compact table: bitmaps for printable ASCII with and without Shift, a
small list of special keys, and a default for modifier combinations. Clients
only do table lookups, so the C++ and Go implementations are trivial.
`EAT` is only assigned when the outcome is certain. Anything else is `ASK`.

**Built by probing librime, not by re-implementing it** (`internal/engine`,
M1). At warm-up the engine feeds every printable key and the editing keys to
a scratch session from each state (idle Chinese, idle ASCII, composing, per
schema and punctuation style) and records what librime does. Both punctuation
styles have their own cached idle and speculative composing policies;
English punctuation does not force ordinary typing through `ASK`. Digits
in Chinese input still send asynchronous history notifications, including
when English punctuation is active, so toggling styles after a digit keeps
numeric separators correct. Probes never select a candidate, so
they never write the user dictionary. Keys whose outcome depends on more than
that state are not probed and become `ASK`: switcher hotkeys and key_binder
bindings (read from config), and navigation keys while composing (Left at the
start of the input, Up on the first candidate are declined by librime). In
the idle policies only `when: always` bindings count: librime's other
conditions (`composing`, `has_menu`, `paging`) need a composition, and the
composing policy asks for every non-printable key anyway. So paging keys such
as `-`/`=` don't make every idle `-` wait for the engine. A binding on a
printable key with only Shift (Sogou's Shift+Space for full width) would
make that key `ASK` in every state, so the settings don't offer one. Each
action also predicts the next state (`Same`, `Composing`, `Unknown`) for the
speculative state below, and each policy names its composing policy. Policies
are sent once per connection; results refer to them by ID.

**Correctness rules:**
- **Speculative state.** After eating a letter in Chinese mode, the client
  assumes composition has started until the result confirms or corrects
  that. So a Space typed 2 ms later is treated as "select", not passed
  through.
- **Ordering barrier.** Before passing a key through (`PASS`) while results
  are still pending, the client waits for them within the budget. This
  keeps text order correct, e.g. `nihao` + Cmd-V.
- **Mispredict repair.** If the engine reports "not handled" for a printable
  key that was eaten, the client commits that character itself, which has
  the same effect as passing it through. Mispredicts on non-printable keys
  must never happen by construction. Each one is counted as a bug metric.
- **Disconnected or engine unavailable:** every key is `PASS` (English), the
  status icon shows a degraded state, and the client reconnects in the
  background. It never connects on the key path.
- **Initial state:** a client sends `FocusIn` as soon as it connects. After a
  front restart the focused app may never activate the input method again,
  and without a state the client would pass every key through (found in the
  VM, M1).
- **Secure input:** the front supplies the secure bit on `FocusIn` and
  rechecks before keys and asynchronous results (§5.5). The core guards
  locally before any engine reply: every key passes without IPC or a wait,
  and queued commits, repair text, preedit and candidates are discarded.
  Password keys never reach Rime; stale policies can't consume them.
- **Shift held:** Rime's `ascii_composer` treats Shift-down/Shift-up with no
  key in between as a 中/英 tap. Keys the client passes to the app while Shift
  is held (Cmd+Shift+4) are sent as notifications so a shortcut never
  switches modes.

**How results are applied, per platform:**
- **macOS:** `handleEvent:` returns at once. Results arrive on a reader
  thread and are applied on the main thread through the client proxy
  (`setMarkedText` / `insertText`), as Squirrel does for mouse selection.
  These are synchronous round trips into the app (spike 2), so:
  - Update the candidate window **before** calling the app.
  - Query the caret rect **once per composition**.
  - When several results are queued, apply every commit in order, but only
    the last marked text and candidates.
- **Windows:** results arrive on a DLL I/O thread (overlapped pipe), are
  posted to the app's TSF thread through a hidden message window, and are
  applied with `RequestEditSession(TF_ES_ASYNCDONTCARE | TF_ES_READWRITE)`.
  The caret rectangle is read inside that edit session and sent to the
  presentation host, never on the key path. Before passing an unhandled
  Control/Alt/Windows shortcut, the client finalizes its existing marked
  range with a pooled, nonwaiting edit request and invalidates old replies.
  External composition termination also invalidates the engine context, so
  a late reply cannot reinsert already-finalized text. Empty unsupported
  input-scope values (including the shell/Notepad `E_FAIL` with `VT_EMPTY`) are ordinary
  fields; explicit secure scopes, native password edits, disabled-keyboard
  compartments, read-only contexts and TSF lock/disconnect errors still
  guard input.
  A background reply watchdog disconnects after 100 ms without progress on
  accepted keys; it adds no wait to the app thread or the 8 ms ASK budget.
  Received edits drain first, existing marked text is finalized, and eaten
  printable keys without replies are inserted as literal text. Later plain
  printable keys join that bounded local tail until its edit runs. Focus,
  security, external selection/text changes, navigation and tail exhaustion
  invalidate delayed writes, so recovery cannot replace a later selection.
  Reconnecting never replays old requests.
- **Linux:** the socket fd is registered in fcitx5's event loop
  (`addIOEvent`). Results update the `InputPanel` on fcitx5's thread.

### 3.4 Engine: the hot path

- **Threading.** One goroutine owns librime, locked to its OS thread
  (`runtime.LockOSThread`) at interactive priority:
  `QOS_CLASS_USER_INTERACTIVE` on macOS, `THREAD_PRIORITY_ABOVE_NORMAL` on
  Windows, `nice -5` on Linux where permitted. IPC reader/writer goroutines
  hand it work through preallocated ring buffers.
- **Zero allocation per key.**
  - Results are copied from librime's C strings into reusable per-session
    buffers.
  - The protocol is encoded into reusable frames.
  - `testing.AllocsPerRun == 0` on the key path is a CI gate.
  - The GC is tuned for a small, stable heap (`GOMEMLIMIT`, high `GOGC`).
- **No callbacks from librime threads.** The notification handler is not
  used. Status is read after each key, and maintenance state is polled off
  the hot path. This avoids Go callbacks on foreign threads entirely.
- **Logging.** librime's `min_log_level = 2` (errors only) in release builds.
  Our logs go to a bounded in-memory queue, flushed by a background goroutine.
  On Windows the host and its supervisor-started engine append to
  `%LOCALAPPDATA%\Shixin\Logs\{shixin,engine}.log`, including startup
  failures and automatic restarts. Files are opened before serving input, and
  logger shutdown flushes queued diagnostics without racing background callers.
  No fsync anywhere.
- **Warm-up before ready.** The engine accepts clients only after it has:
  1. Pre-faulted the compiled dictionaries (`madvise(WILLNEED)` + touching
     the pages).
  2. Opened every enabled schema in a hidden session.
  3. Filled a pool of ready sessions, so a new app or window never waits for
     `create_session`. Each pooled session runs one composition with emoji
     and 简繁 on: librime's simplifiers load their OpenCC dictionaries
     lazily, per session, and the first key of a fresh session took 26 ms
     for 简繁 (9.5 ms for emoji) before this. Engine restart with deployed
     data stays under 200 ms in the VM.
  4. Run a short synthetic typing script through each schema.
- **Staying warm.** On focus-in, the next session's dictionaries are
  re-advised. A low-priority background thread periodically touches
  dictionary pages so the OS doesn't evict them after long idle periods.
- **Module list.** librime is initialized with an explicit `modules` list
  (`default`, `shixin`, plus `predict`/`octagram` only if adopted). The
  dynamic `plugins` loader is never enabled, so no plugin found on disk,
  such as a Homebrew librime-lua, can be loaded.

### 3.5 Deploys, updates and crashes without downtime

**Settings change or dictionary update (M2 protocol):**

The front's supervisor starts one persistent `shixin-engine` coordinator.
It owns a separate supervisor lock, runs no librime, and survives front
restarts. Engine generations are children of it. A private, same-uid Unix
control socket (`engine.sock.control`) accepts versioned JSON `apply`
requests from Settings and replies only after activation or failure. This
cold control protocol is separate from the unchanged binary key protocol.
Settings retains its existing applying/error status, and serializes edits.
The coordinator also watches settings.json, so edits outside Settings work.

1. The coordinator snapshots settings into a new private generation under
   `Rime/generations`, and invokes the sibling `shixin-builder`. The
   builder has no live-user-dir argument: its own scratch input/build dirs
   contain the generated patches and read-only references to shared data.
   It calls only deployer APIs, never `create_session`, maintenance, sync,
   or user-dictionary APIs. Failed generations are discarded.
   For a live rebuild it privately copies static `.bin` resources from the
   validated warm-only active generation. The compiler checks checksums and
   rebuilds changed resources; files are never hard-linked to an engine's
   mapped dictionaries. This avoids rebuilding unchanged dictionary tables
   and their large temporary memory footprint. Failures retain a bounded,
   allowlisted stage name and exit/timeout status; native diagnostics may
   contain private configuration text and are not copied into product logs.
2. The builder emits real compiled schemas plus warm variants. Every
   script/table translator namespace in a warm variant explicitly disables
   its user dictionary; `default.yaml` lists **only** warm variants. No
   remembered real schema can therefore be selected by `create_session`.
   Custom phrase tables belong to the generation, not the live user dir.
3. A child `shixin-engine -worker -generation …` initializes with the
   generation's staging dir. Before creating *any* session it validates the
   warm-only default list and every translator's user-dictionary setting.
   It probes policies and warms sessions/dictionaries without the engine
   lock; these sessions cannot open the live dictionary. Its stdin/stdout
   carry versioned lifecycle commands/replies, never typed text.
4. The coordinator asks the old worker to migrate only when no session is
   composing. It retries off the key path; continuous composition leaves
   the old generation active and eventually reports an apply error.
   `Migrate{endpoint}` already exists in binary protocol v1. On its main
   thread the client stops posting to the old connection, drains its
   already queued writes, and half-closes it. The old worker processes
   those writes and flushes results before closing. This is a sequence
   boundary: accepted keys are answered before the dictionary is released.
   A stuck client is subject to the existing bounded disconnect rule.
5. After clients drain, the old worker destroys all sessions and finalizes
   librime, then releases Shixin's exclusive engine lock. The standby
   acquires that lock, checks LevelDB lock files, and selects the real
   schema on its retained warm session. While holding the lock it changes
   the cached default schema list to real IDs (without saving it), so the
   switcher and future sessions never choose warm schemas after activation.
   One session is ready at activation, matching the macOS front's single
   context. Additional contexts use the existing cold session fallback;
   preparing a larger real-schema pool is deferred. No deploy or file
   copying occurs on a serving engine thread. Only the lock holder may select a real schema.
6. Each worker has a private endpoint. The public `engine.sock` is an
   atomically replaced symlink to the active endpoint; clients reconnect
   to this stable discovery path in their background connection loop.
   The coordinator persists the active generation for crash recovery and
   then retires the old worker. It keeps the previous generation for
   rollback. On build or warm-up failure the old engine never migrates.
   If promotion fails after release, stop/wait the failed standby before
   restarting the previous generation, still under the single-owner rule.

Before reusing a persisted generation at startup, the coordinator compares
its saved generated input files with the current settings compiler's output.
Loading `settings.json` alone is insufficient: newly added default bindings
can make old and new snapshots compare equal while the deployed schema is
still old. A missing or mismatched input triggers a fresh offline build
before starting a worker. Matching generations keep the existing fast
recovery path; no comparison or compilation runs on a serving engine thread.

Once the recovered worker is serving, the coordinator snapshots abandoned
`gen-<digits>` directories and sibling `.json` settings snapshots for
background removal. The persisted active generation is protected; after
successful startup other generations left by an earlier coordinator are
no longer needed for recovery. During normal swaps, active and previous
generations remain retained as above. Cleanup never rescans, so builds
started by later settings applies cannot enter its deletion list. It
leaves unrelated names and symlinks alone, confines deletion to an open
generation directory, logs failures without interrupting service, and
stops before the supervisor lock is released on shutdown.

Composition and transient per-session switches/remembered app modes are not
transferred; the new generation applies the current settings. A composition
started while migration is draining may be cleared; completed commits are flushed first.
During the short disconnected handoff, keys follow the existing §3.3
`PASS` fallback; they are not buffered and replayed under a changed policy.
`EAT` remains asynchronous and `ASK` retains its 8 ms cap. This corrects the
original design's unimplemented promise of client-side handoff buffering.

The 20 ms target is deferred: accept spike 4's **28–32 ms** for now, without
patching librime. Local product checks on Apple M4, macOS 26, our librime
1.17.0 (2026-10-26) measured release **0.8–5.5 ms**, release→ready **23–43 ms**
across concurrent local runs.
An isolated learned-word preservation run
measured **30.10 ms** to ready and **31.04 ms** through the first query.
These include lifecycle pipe round trips, process exit, lock acquisition,
real-schema attach/filter warm-up, and publishing the endpoint. There is
no strict timing assertion on the handoff while other worktrees run. The
coordinator logs only timings and lifecycle errors, never input or candidates.

**Prebuilt data:** bundle creation runs `shixin-builder` in a scratch dir
with all supported schemas, copies the compiled tables, reverse indexes
and prisms into `Contents/Resources/rime-prebuilt` (72 MB), and drops the
dictionary sources from `rime-data` (46 MB): librime reuses a prebuilt
table when its `.dict.yaml` is missing. Runtime builders pass the
directory as `prebuilt_data_dir`. No bundle tool opens a real user
dictionary. The bundle is 103 MB (44 MB zipped).

Our librime fork keys prism reuse on the dictionary checksum and a
canonical serialization of the compiled schema's `speller` section.
Unrelated settings (page size, emoji, 简繁, mixed English), build timestamps
and YAML formatting no longer rebuild prisms. Spelling changes still
rebuild the affected prism, including fuzzy pinyin in full and double
pinyin. The existing prism layout is unchanged; legacy whole-schema
checksums cause a one-time rebuild on deployment. No compilation happens
on a serving engine thread.

The builder still copies shared configuration YAML into its private input
dir and gives inputs one fixed mtime (`generation.stableInputs`) for
reproducible compiled configuration. `TestPrebuiltReuse` packages every
schema, removes dictionary sources as the bundle does, then checks reuse
with default and non-default settings and selective rebuilds for fuzzy
pinyin. `TestFirstRunTiming` compares empty user dirs with and without
prebuilt data. On Apple M4, the page-size case in `TestPrebuiltReuse` fell
from 2.08 s to 53 ms for the isolated generation-build call.

**Verification:** `TestStandbyPreservesLearning` runs independent engine
processes on a scratch user dir. It commits a lower-ranked word until it
becomes the first candidate, checks LevelDB ownership and byte-for-byte
unchanged dictionary files during standby warm-up, rejects early promotion,
continues learning, drains a queued commit, swaps, and checks the learned
rank again. It also covers the schema switcher, custom phrase generations,
a swap to 小鹤双拼, builder failure, and warm-up failure. `TestCoordinatorControlAndRecovery`
covers the Settings control socket, exclusive coordinator ownership and
worker crash recovery, plus startup reuse of a generation while abandoned
build data is collected. `TestGenerationCleanup` cases cover protected and
new generations, partial builds, snapshots, cancellation and symlinks.
`TestStopForUpgrade` verifies coordinator-first
shutdown releases both locks. `BenchmarkKey` (the IPC round trip on the
small test schema) was **20.65 µs/key before / 21.55 µs/key after**, with
**0 B/op and 0 allocs/op**; engine/client allocation assertions pass.
The existing binary IPC v1 and its golden vectors are unchanged: this uses
its existing `Migrate` message.

In the VM (macOS 26, TextEdit): changing the page size while typing
swapped generations in 2.5 s with the old engine serving throughout;
the engine was unavailable for 51 ms (release to ready), and the next
composition showed the new page size. Upgrading the bundle replaced the
coordinator and reused the active generation without a rebuild.

**App updates** are intended to use the same path with a new worker
executable. The current coordinator starts its own executable for every
generation; automatic engine binary replacement still needs an upgrade
control operation and coordinator rollover. The existing macOS
`-after-upgrade` path instead stops the coordinator first, waits for its
worker to release the dictionary, then starts the new helper. It also
handles a legacy engine that holds only the live user lock.
On macOS, a front (`Shixin.app` executable) update is swapped in place
without re-registering the input source (§5.5).

**Crash:** clients get a disconnect, every key becomes `PASS`, and the
supervisor restarts the engine from the warm-up step. Typing continues in
English until it's back, typically within 300 ms. The app never freezes.

**Sync (M5):** librime's `sync_user_data` disables input while it runs. It is
scheduled only in idle windows: screen locked, or no input for 5 minutes. If
the user starts typing mid-sync, clients degrade to `PASS` with a "同步中"
indicator instead of waiting. A later improvement could be an incremental
sync that avoids librime maintenance mode.

**Single owner of the user dictionary (spike 4):**
- Only one engine process per user dir may have sessions with a user
  dictionary. It holds Shixin's exclusive engine lock. Standbys may have
  only validated warm sessions without a user dictionary.
- The supervisor may warm a replacement before release, but it never
  promotes it while the old engine might still hold the user dictionary.
  A hung engine is killed and waited for.
- Our librime schedules automatic user-dictionary recovery only when
  LevelDB reports corruption. Lock conflicts, permission failures and other
  I/O errors leave the database alone. Recovery acquires LevelDB's own
  exclusive `LOCK` before repair and holds it through archiving a damaged
  directory; a task queued before another process opens the database must
  fail safely too. If archiving fails, it preserves the database rather
  than deleting it. A replacement database takes its own lock on open.
  The engine lock and warm-only schemas remain required for single
  ownership; this patch adds protection inside librime.
- `TestUserDictionaryLockConflict` deliberately bypasses Shixin's lock in
  scratch subprocesses: an owner learns a lower-ranked word, a second
  process creates a session on the same user dictionary, and the owner
  continues learning. A fresh process verifies the learned rank after
  shutdown. C++ tests also cover real corruption recovery, queued-task
  lock races, snapshot fallback and failed archiving.

Results: `docs/spikes/m0-4-handoff.md`.

### 3.6 UI paths

- **Candidate window:**
  - Created at startup, then only shown or hidden: a non-focusable,
    non-activating window; never created on the key path.
  - Rendering coalesces to the latest state per display frame.
  - Result application copies display data into a startup buffer sized to
    the IPC frame limit and queues a preallocated `dispatch_async_f`
    callback. Caret IPC and showing/hiding the window happen there, after
    `handleEvent:` returns. The UI frame consumes only the latest snapshot;
    it never borrows the client's reusable result buffers.
  - Candidate/preedit/comment strings are immutable owned copies, interned
    in a bounded cache (512 entries, 256 KiB). Unchanged text reuses its
    string in place; row click handlers and view buffers are retained.
    Capture and warmed typing/page/highlight/hide frames allocate nothing.
    A new string or layout may allocate on a UI-frame cache miss, outside
    the key callback. No text is logged or persisted by these caches.
  - Shaped text runs are cached per candidate string.
  - The glyph cache is pre-warmed with the ~3,500 most common hanzi and the
    UI fonts.
  - Fallback font lookup happens at startup, not on first use.
  - Candidate font families are listed once at startup. The selected family
    (or the system default if unavailable) is prepared with representative
    Latin, Chinese, symbol and emoji glyphs at both text sizes and 1×/2×
    before IMK registration. Live settings changes prepare the new font on
    the watcher goroutine before publishing it to the candidate window.
    Font-aware text/baseline measurements are retained in a bounded cache
    (512 entries, 256 KiB of key strings) so revisiting warmed pages does
    not allocate. Rare glyphs may still miss the renderer's glyph cache on
    an asynchronous UI frame, outside key handling.
- **Settings** is a separate process. Heavy pages (skin gallery, dictionary
  lists) can't affect the IMK main thread or the candidate window.
- **Mode bubble (macOS):** a separate, non-activating, click-through flyout
  created at startup. Both mode glyphs in every skin/appearance are rendered
  to 2x bitmaps before registering IMK; switching modes loads no fonts or
  images. Result application only updates fixed-size state and queues a
  preallocated `dispatch_async_f` callback. That callback queries the caret
  and shows the bubble after `handleEvent:` returns. One reusable timer
  queues expiration; stale callbacks consult the latest deadline.
- **macOS main thread** (Shixin.app) does only IMK calls, applying results,
  and candidate drawing. App Nap is disabled with
  `NSProcessInfo.beginActivity(.userInitiated | .latencyCritical)`.
- **Windows Shixin.exe:** opted out of power throttling
  (`SetProcessInformation(ProcessPowerThrottling)`). Its candidate window is
  shown with no-activate flags.

### 3.7 Latency budgets

| Path | Budget |
|---|---|
| Client classify + enqueue (app thread) | ≤ 50 µs, no blocking syscalls |
| `ASK` / ordering-barrier wait (app thread) | hard cap 8 ms (configurable 4–16 ms); expected p99 < 1 ms |
| IPC one way | p99 ≤ 100 µs |
| Engine per key, rime-ice | p50 ≤ 0.3 ms · p99 ≤ 2 ms · p99.9 ≤ 8 ms |
| Key → candidate window updated (excl. display refresh) | p99 ≤ 4 ms (measured 3.4 ms, spike 2) |
| Key → text updated in the app | measured, not gated: IMK/TSF calls are synchronous round trips into the app, which does its own layout (TextEdit: 2.8 ms p50) |
| Heap allocations per key (client + engine) | 0 |
| Engine unavailable during hot swap | 28–32 ms accepted for M2; ≤ 20 ms deferred, idle boundaries only |
| Engine crash → serving again | ≤ 300 ms (English pass-through meanwhile) |
| Cold start → first key handled | ≤ 300 ms after warm data exists (English until then) |
| Context reranking (§7.2) | The app never waits. With 智能排序 on, key → candidate window can grow by the 16 ms hold (measured p50 9.2 ms); engine work per key ≤ 0.5 ms more at p99; scorer p95 ≤ 15 ms |

"Stall" is defined as any app-thread wait attributable to Shixin longer than
the `ASK` cap. Every one is counted, logged and treated as a bug.

### 3.8 Native extensions instead of Lua (`librime-shixin`)

rime-ice puts 16 components in Lua. Lua runs on every key, through an
interpreter with its own garbage collector. rime-ice even ships a
`force_gc` translator to keep that GC in check. Shixin never loads Lua.
Instead:

- **The settings compiler writes the full engine lists**
  (`engine/processors`, `segmentors`, `translators`, `filters`) for every
  schema. It doesn't inherit rime-ice's, so no `lua_*` component reaches a
  deployed schema. CI fails if one does.
- **`librime-shixin`** is a C++17 librime module, statically linked into our
  librime build on every platform. It registers native components under
  the `shixin_` prefix. Each component:
  - meets the engine budget (§3.7)
  - does no I/O or allocation after initialization on the common path
  - has unit tests and is included in the replay corpus
- **Why C++ (not Go or MoonBit):**
  - Candidates must live inside librime's menu, so that paging, selection,
    word learning and filters such as emoji and 简繁 keep working.
  - librime's component interface is a C++ class API (`Processor`,
    `Translator`, `Filter` over `Context`/`Segment`/`Candidate`), with no C
    interface.
  - A Go implementation would need a C++ adapter calling back into Go
    several times per key, with allocating string conversions. That
    violates R1/R5.
  - MoonBit would also need a C++ adapter, and would add a third, pre-1.0
    toolchain.
  - The components are small. Memory safety is covered by
    AddressSanitizer/UBSan in CI and by fuzzing the parsers (calculator,
    Unicode).
- **The dividing line:** only code that must run *inside librime's
  candidate pipeline* goes in `librime-shixin`. Everything else stays in
  Go: cloud candidates, dictionary import, settings, sync, per-app modes,
  the key policy. Context reranking (§7.2) follows the same line: the
  `shixin_rerank` filter only exports candidates and applies a permutation,
  while scoring and the ordering policy run in Go in `shixin-ai`.

| rime-ice Lua component | Feature | Shixin |
|---|---|---|
| `select_character` | 以词定字 (`[` `]`) | `shixin_select_character` processor · M1 |
| `date_translator` | `rq` `sj` `xq` `dt` `ts`, Chinese/English dates | `shixin_date_translator` · M2 ✅ |
| `number_translator` | `R123` → 壹佰贰拾叁, amounts | `shixin_number_translator` · M2 ✅ |
| `unicode` | `U62fe` → 拾 | `shixin_unicode_translator` · M2 ✅ |
| `calc_translator` | Calculator (`cC1+2`) | `shixin_calc_translator`: bounded recursive-descent parser · M2 ✅ |
| `pin_cand_filter` | Pinned candidates (置顶) | `shixin_pin_filter`, configured from Settings · M2 |
| `long_word_filter` | Long words first | `shixin_long_word_filter` · M2 |
| `reduce_english_filter` | Demote some English words | `shixin_english_demote_filter` · M2 |
| `v_filter` | v-mode symbols first | `shixin_v_filter` · M2 |
| `autocap_filter` | English auto-capitalization | `shixin_autocap_filter` · M2 |
| `corrector` | Typo/pronunciation hints | Later; evaluate cost |
| `lunar` | 农历 | Later; precomputed table, no runtime astronomy |
| `uuid` | UUID | Later |
| `search` | Radical-code assisted search | Later |
| `force_gc` | Lua GC workaround | Dropped; not needed without Lua |
| rime-ice `processors`/`segmentors`/`translators` that are not Lua (speller, script_translator, melt_eng, cn_en, radical lookup, custom_phrase, emoji/traditionalize simplifiers, uniquifier) | | Kept as-is |

The translators read rime-ice's own configuration, so its schemas need no
changes: `date_translator/*` for the date codes, and the prefixes of the
`recognizer/patterns` that tag their segments (`unicode` `^U…`, `number`
`^R…`, `calculator` `^cC…`). The recognizer takes a key into the input
when the input plus the key still matches a pattern, ahead of the selector
and the page keys: digits and `.` after `R`, hex digits after `U`, any
printable key after `cC`. That is why `shixin_select_character` runs after
the recognizer: its keys (`-` `=` when configured) must still reach
`cC5-3`. rime-ice's full pinyin uses initials as date codes (`rq`, `sj`);
its double pinyin schemas use words (`date`, `time`), since two letters
are a syllable there. Date candidates have quality 100 and come first, as
in rime-ice. The pure parts (calculator, numerals, date formats, code
points) have no librime dependency; they are unit-tested on their own and
fuzzed (`test/parsers_fuzz.cc`).
On Windows the date clock converts the current OS instant with
`FileTimeToLocalFileTime`, so time-zone changes take effect in existing
sessions without refreshing the CRT's cached zone or reading files on the
key path. Linux's zone-change notification support remains deferred.

## 4. Key flow per platform

**macOS**

```
key → IMK → Shixin.app main thread: handleEvent
  → keymap (kVK → keysym) → policy lookup
     PASS → return NO
     EAT  → enqueue to engine, return YES
     ASK  → send, wait ≤ budget
  … engine thread: librime → result (commit, preedit, candidates, next policy)
  → reader thread → main thread: setMarkedText / insertText, candidate window
```

**Windows**

```
key → TSF → shixin_tsf.dll OnTestKeyDown/OnKeyDown → policy lookup
     EAT → enqueue on pipe, return eaten
  … engine → result → DLL I/O thread → PostMessage → app TSF thread
  → async edit session: update composition / commit; read caret rect → engine
engine → Shixin.exe: candidate state + caret rect → candidate window
```

**Linux**

```
key → fcitx5 → addon keyEvent → policy lookup → filterAndAccept + enqueue
  … engine → result → fcitx5 IO event → InputPanel / commitString
```

## 5. Components

### 5.1 Repository layout

```
go.mod                         module shixin
cmd/shixin-engine/             engine: persistent coordinator and generation workers (no MyGo)
cmd/shixin-builder/            offline compiler of private generations
cmd/shixin-ai/                 智能排序 scorer process (§7.2)
cmd/shixin/                    macOS front: IMK, candidate window, mode bubble, input menu, installer, supervisor
cmd/shixin-settings/           settings app (MyGo)
internal/rime/                 purego binding to librime
internal/engine/               engine thread, sessions, probed key policies, hot-swap handoff, 智能排序 hold
internal/generation/           builds and validates private, warm-only generations (§3.5)
internal/lifecycle/            coordinator/worker/Settings control protocol (JSON, no input text)
internal/policy/               key policy table and wire format, keysyms and modifier masks (spec for C++)
internal/keymap/               native key codes → keysyms/modifiers (macOS kVK)
internal/ipc/                  binary client protocol, zero-alloc codec, Unix socket transport
internal/client/               Go client core of the macOS front: policy, speculation, barriers, auto spacing
internal/settings/             settings model, validation, storage, Rime patch compiler, shuangpin charts
internal/rerank/               智能排序: context rings, slots, ordering policy, engine↔shixin-ai protocol
internal/ai/                   shixin-ai's scorer and server
internal/llama/                purego binding to llama.cpp
internal/models/               智能排序 model download and verification
internal/perf/                 allocation-free latency histograms
internal/logx/                 non-blocking logger and log locations
internal/i18n/                 Chinese/English string catalog
internal/ui/{brand,candview,fonts,modeview,skin}/  MyGo views shared by the front and the settings app
plugins/librime-shixin/        native C++ librime module (replaces rime-ice Lua, §3.8)
third_party/librime/           our librime fork (submodule)
third_party/llama.cpp/         inference runtime (submodule), built by scripts/build-llama.sh
packaging/macos/               Info.plist, localizations and icons for Shixin.app
scripts/                       data fetch, librime/llama builds, bundling, packaging, CI, the Tart VM
tools/vm-input/                test-only native keyboard/clipboard helper for the VM (not bundled)
tools/menu-capture/            offline menu capture from public codes (§9)
spikes/                        M0/M2 experiments; results in docs/spikes/
docs/                          this design, known issues, test records, spike reports
```

Windows development code is in `frontends/windows-tsf/` (C++ TSF DLL,
M3; VM validation pending). Planned: `frontends/fcitx5/` (C++ fcitx5 addon, M4), dictionary import (`.scel`, M2) and the Windows
candidate window and tray in `cmd/shixin`.

### 5.2 `internal/rime`: librime binding

- `purego.Dlopen`:
  - `librime.1.dylib` from the bundle's Frameworks on macOS
  - `rime.dll` next to the exe on Windows
  - `librime.so.1` on Linux
- `rime_get_api()` is read into a Go struct that mirrors `RimeApi`.
  `data_size` is checked before each optional member is used.
- Functions that return structs by value are avoided. Every call is
  `purego.SyscallN`, and nothing that calls back into Go is registered.

### 5.3 Settings → Rime config compiler

- Settings are stored in one JSON file:
  - macOS: `~/Library/Application Support/Shixin/settings.json`
  - Windows: `%APPDATA%\Shixin\settings.json`
  - Linux: `$XDG_CONFIG_HOME/shixin/settings.json`
- The compiler writes `default.custom.yaml` and one `<schema>.custom.yaml`
  per supported schema directly, and the custom phrase table
  `shixin_phrases.txt`. Each schema file carries the full Lua-free engine
  lists, no spelling-hint comments (rime-ice shows the pinyin of every
  candidate for its Lua corrector; Sogou shows none), the `switches` with their initial states from the settings, and
  the whole `key_binder` map (replacing rime-ice's, and with it its import
  of `default.yaml`'s bindings). `switcher/save_options` is empty, so the
  settings, not librime's saved options, decide each switch's initial
  state.
- Generated YAML is an internal implementation detail. Supported options
  come from the settings app and its validated settings model. Arbitrary
  user-supplied schemas and YAML patches, including `shixin.user.yaml`, are
  unsupported. Existing external files are preserved without being applied.
- The Rime user directory is our own, so Squirrel/Weasel data is untouched:
  - macOS: `~/Library/Application Support/Shixin/Rime`
  - Windows: `%APPDATA%\Shixin\Rime`
  - Linux: `$XDG_DATA_HOME/shixin/rime`
- Applying a change goes through the builder and hot swap (§3.5). The engine
  is never redeployed in place. Errors appear in Settings.

| Setting | Generated Rime config |
|---|---|
| Candidates per page = 7 | `menu/page_size: 7` |
| Fuzzy z=zh, n=l | `speller/algebra/+`: `derive/^([zcs])h/$1/`, `derive/^n/l/` |
| Shift_L commits code | `ascii_composer/switch_key/Shift_L: commit_code` |
| Scheme = 小鹤双拼 | `schema_list: [double_pinyin_flypy]` |
| Start in English, 繁体, no emoji | `switches`: `ascii_mode` reset 1, `traditionalization` reset 1, `emoji` reset 0 |
| 以词定字 on `- =`, page with `, .` | `key_binder/select_first_character: minus`, `bindings`: `{when: has_menu, accept: comma, send: Page_Up}` … |
| 简繁 on ⌃⇧4 | `bindings`: `{when: always, accept: Control+Shift+4, toggle: traditionalization}` (+ `Control+Shift+dollar`, what macOS reports) |
| No mixed English | `engine/translators` without `table_translator@melt_eng` and `@cn_en` |
| Custom phrase yx → 邮箱 | `shixin_phrases.txt`, `custom_phrase/user_dict: shixin_phrases` |

`settings.json` is user-editable and may come from a newer version: loading
resets only the invalid fields (or drops invalid and duplicate list
entries) and keeps the rest; the settings app keeps the original as
`settings.json.bad` before it next writes the file. Errors name phrases by
index, never by their text, since they reach the logs.

Fronts read the appearance settings (skin, light/dark, font family/size, layout)
straight from `settings.json`, which the macOS front watches; they change no
Rime file, so `settings.EngineChanged` reports that the engine needn't
reload. 简繁 needs OpenCC's data, which comes with our librime build and is
bundled into `rime-data/opencc`.

Appearance offers a searchable installed-font picker and the live candidate
preview. `font_family: ""` uses the system default. A family missing on the
current machine falls back without discarding the saved preference; the
settings page explains the fallback. Fonts installed while Shixin is running
appear after restarting the app.

### 5.4 IPC

- **Transport:**
  - macOS / Linux: Unix socket in a 0700 per-user directory
  - Windows: named pipe `\\.\pipe\shixin-<SessionId>-<UserSID>` with
    overlapped I/O
- **Framing:** `u32 len | u8 type | payload`. Strings are `u32 len | UTF-8`.
  Fields are append-only. Decoding is zero-allocation, and the decoder is
  fuzzed.
- **Handshake:** `Hello{protocolVersion, clientKind, pid, appId}` →
  `Welcome{protocolVersion, features, policy}`. Old TSF DLLs that stay
  loaded after an update keep working.
- **Messages** (`internal/ipc`, golden vectors in `internal/ipc/testdata`):

  | Client → engine | Engine → client |
  |---|---|
  | `Hello{version, kind, pid, app}` | `Welcome{version, features}` |
  | `Key{ctx, seq, keysym, mods, flags(sync, notify)}` | `Result{ctx, seq, flags(handled, composing, ascii, …), policyId, commit, preedit, cursor, sel, page, highlighted, candidates, labels}` |
  | `FocusIn{ctx, secure, app}`, `FocusOut{ctx}` | `Policy{id, table}`, sent once per connection before the first `Result` that refers to it |
  | `Select{ctx, seq, i}`, `ChangePage{ctx, seq, back}`, `Clear{ctx, seq}`, `SetOption{ctx, seq, name, on}` | `Unavailable{reason}`, `Migrate{endpoint}` |
  | `StatsRequest{reset}` | `Stats{keys, dropped, startup, uptime, librime, native, schemas, keyTime histogram}` |

  The settings app connects with `kind = Settings` and only asks for stats
  (its 性能 page); it opens no input context.

  智能排序 (§7.2) needs nothing from this protocol: a key's result
  simply comes with the reordered candidates.

  The engine and `shixin-ai` use their own protocol (`internal/rerank`,
  golden vectors in its `testdata`), with the same framing:
  - `Hello{version}` → `Status{version, ready}`
  - `Context{app, rev, text}` (ContextUpdate)
  - `Score{session, exportSeq, app, appRev, strength, entries(weight, flags, text)}`
  - `Rerank{session, exportSeq, permutation}`

  It's versioned the same way, but no C++ client sees it.

  `ctx` is a client-chosen input context id (one Rime session each), so one
  connection can serve several text fields or apps.
  Windows uses a separate presentation pipe (`ui.sock`). After validating
  the focused context and secure state, the TSF apartment forwards only
  display fields from `Result` and a `Caret{ctx, left, top, right, bottom}`
  frame (type 10, signed physical screen pixels) directly to `Shixin.exe`.
  It never forwards commits. The host verifies the foreground PID and context,
  coalesces displays, and sends `Select{ctx, displayedSeq, index}` back; TSF
  rejects stale clicks and assigns the engine sequence. The engine key
  stream is unchanged and receives no caret frames.
- **Security:**
  - Windows: input and candidate pipes grant the user full access and
    `ALL APPLICATION PACKAGES` only read/data-write access, with a low-integrity
    label. Control and AI pipes stay user-only. Discovery records grant packages
    read access only. All pipes reject remote clients and check peer SID/session;
    clients also check the server image path. Input servers grant packages only
    limited process query and token query rights so these checks work inside
    AppContainer; no process control, memory reads or token duplication is
    permitted. Authenticode checks remain required before release.
  - Unix: the peer's uid is checked via `SO_PEERCRED` / `getpeereid`.
- A shared-memory ring transport is a possible later optimization. It isn't
  needed while R1 holds.

### 5.5 macOS front (`Shixin.app`)

- **Bundle:**
  - id `im.shixin.inputmethod.Shixin`, `LSUIElement` (as Squirrel)
  - IMK Info.plist keys set through MyGo's `macos.infoPlist`
  - installed in `~/Library/Input Methods`
  - `shixin-engine` in `Contents/Helpers`
- **IMK classes** are registered with `purego/objc` in `App.WhenReady`. The
  controller holds only the Go client core (§3.3). Verified in spike 2.
- **Secure fields:** bind Carbon/HIToolbox's
  `IsSecureEventInputEnabled` with purego and warm it at startup. Read it
  on activation, each key (including modifier events), composition end,
  deactivation, candidate selection and before draining asynchronous
  results. IMK may reuse the same proxy and omit `activateServer:` when
  focus moves inside an app; polling only on activation, or only while
  idle, could miss a password field entered during composition. Each
  change updates the local guard and posts the existing
  `FocusIn{ctx, secure, app}` asynchronously, without another key-path
  wait. Reconnect preserves the bit. Carbon documents this API as not
  thread safe, so reads stay on the main thread. The native flag check
  measured 160–170 ns/call and zero allocations on an Apple M4; run
  `go test ./cmd/shixin -run Secure -bench BenchmarkSecure -benchmem`
  to measure it again without running or installing the input method.

  **Ownership:** the flag is global ([Apple TN2150](https://developer.apple.com/library/archive/technotes/tn2150/_index.html));
  it alone never marks a field secure. Only while it is on, read the owner
  PID from `CGSessionCopyCurrentDictionary`'s `kCGSSessionSecureInputPID`
  through CoreGraphics/CoreFoundation via purego. Require a positive
  CFNumber PID equal to the focused client's app PID. IMK's public
  `IMKTextInput` protocol exposes `bundleIdentifier`, not a PID. Use
  `NSWorkspace.frontmostApplication.processIdentifier` only after its
  bundle identifier matches the IMK client's cached bundle identifier.
  This also distinguishes multiple instances of the same bundle. No
  lookup calls the client's process on a steady-state key.

  Cache the owner/client PIDs and match result by client proxy and current
  flag state. Re-resolve on a flag off/on transition, proxy/activation
  change or workspace app activation, and every 250 ms while the flag
  stays on. An owner handoff without those notifications is noticed at
  the next refresh. Both positive and negative matches are cached.
  Flag plus cached ownership measured 192–206 ns; a re-resolution measured
  28–29 µs on the unavailable-session path, all with zero Go allocations.
  Each copied CF dictionary is released and each lookup has its own autorelease pool. The immutable
  key and workspace singleton are held for the process's lifetime; the
  cached client bundle string is retained until replaced.

  **Limits:** Terminal's own Secure Keyboard Entry makes Terminal English
  with spacing off while enabled; secure input stuck in another process
  no longer changes Shixin's mode or spacing. A missing session, key,
  valid PID, frontmost app or matching client bundle means non-secure,
  never a global fallback. The owner key is undocumented, but present on
  macOS 26 (VM, Safari password field: the owner is Safari's own process,
  not WebContent). Tests exercise the exact key and
  CFNumber decoding using native CF dictionaries. Shixin never enables
  or disables the system flag or reads a field to detect its type.
  Custom password controls that neither enable secure input nor restrict
  input sources remain outside this method.

  Browsers combine Secure Event Input with restrictions to ASCII-capable
  sources: [Chromium's implementation](https://github.com/chromium/chromium/blob/main/ui/base/cocoa/secure_password_input.mm)
  calls `EnableSecureEventInput` and sets the TSM enabled-input-sources
  property; its comments trace this to WebKit. The call site is in
  Chromium's browser process (`RenderWidgetHostViewMac::SetTextInputActive`),
  and [WebKit's `WebViewImpl::updateSecureInputState`](https://github.com/WebKit/WebKit/blob/main/Source/WebKit/UIProcess/mac/WebViewImpl.mm)
  calls Carbon in the UI process. The inspected password paths therefore
  identify the browser app, not a Chrome renderer or WebContent process.
  Do not alias shared helper bundle identifiers to a browser or walk
  arbitrary parents: that could attribute another app's secure input to
  the client. An unexpected helper owner is an unknown match rather than
  a guess. Native `NSSecureTextField`
  can likewise exclude non-Roman input methods, so Shixin may receive no
  password keys at all: in the VM, Safari's password field switches to
  U.S. before any key reaches Shixin. This OS behavior complements the guard; it is not
  assumed for every custom control. [Squirrel](https://github.com/rime/squirrel)
  has no explicit secure/password check in its input controller. [McBopomofo's controller](https://github.com/openvanilla/McBopomofo/blob/master/Source/InputMethodController.swift)
  also has no such check in activation or event handling. These examples
  do not establish that every IMK client is excluded; Shixin keeps its
  explicit guard. No reference code is copied.
- **Finishing a composition:** `deactivateServer:` and `commitComposition:`
  commit the last marked text set in the app without the spaces librime
  puts between syllables: what the user typed, as Sogou does, with any
  already-converted prefix (`ni hao` commits `nihao`, `你 hao` commits
  `你hao`), not candidate 1. Squirrel's lifecycle handling was consulted
  (`SquirrelInputController.swift` and `SquirrelApplicationDelegate.swift`),
  but no source was copied; Squirrel fetches raw Rime input at
  finalization.
  The client owns a reusable copy of the last displayed preedit, inserts
  it locally through the normal auto-spacing commit path while the old
  app proxy is still retained, and queues `Clear` asynchronously. There
  is no engine wait or drain in these callbacks. A sequence fence drops
  every queued or late result from before Clear, including commits,
  mispredict repairs, preedit and candidates. Unsequenced old focus
  replies are ignored until the clear acknowledgement; new focus replies
  then apply normally. Keys not yet reflected in the displayed marked
  text are deliberately omitted when focus leaves. A changed client
  proxy also finishes the old field before replacing it.
  On macOS 26, `kTISNotifySelectedKeyboardInputSourceChanged` checks the
  current source and uses the same finish path when Shixin is no longer
  selected, covering switches by another process (`TISSelectInputSource`)
  that skip `deactivateServer:`. Distributed notifications are observed
  through `CFNotificationCenterAddObserver` with
  `DeliverImmediately`: by default they are held while the process is
  inactive, and an input method's process always is, so they would
  arrive late or never (found in the VM). Deactivation releases the retained app proxy, and
  the local marked-text copy is emptied before insertion, so subsequent
  lifecycle callbacks cannot duplicate that commit.
- **No menu bar item of its own.** The controller's `menu` adds 拾新's
  section to the system input menu while 拾新 is selected: the current
  mode, 设置…, and the version. Restarting or redeploying the engine is
  never a user action: the supervisor restarts it after a crash, and the
  lifecycle coordinator after a change that needs it. MyGo floats a window above
  other apps only as a tray icon's flyout, so the front creates a tray
  icon, creates the candidate and mode-bubble flyouts, and destroys the
  icon. The windows keep their level, and the front places them itself.
- **Install: open Shixin.app.**

  The IMK connection name in the executable and `Info.plist` must both be
  `im.shixin.inputmethod.Shixin.Connection` (bundle identifier plus
  `.Connection`). Modern IMK's bundle resolver ignores names outside its
  legacy allowlist and returns this fallback. The server may normalize its
  published name separately, so resolver agreement alone does not prove
  that a client can connect. `TestConnectionName` checks the declaration
  against the installed framework's resolver without registering a source.

  Opened from anywhere but
  `~/Library/Input Methods` (Downloads, a disk image), it installs itself
  (`cmd/shixin/install_darwin.go`):
  1. **Not installed:** copy the bundle in with `ditto`, clear the download
     quarantine, prepare System Settings (below) and run `Shixin -install`
     (`TISRegisterInputSource`, then `TISEnableInputSource`, which asks
     for consent).
  2. **Installed:** upgrade in place (below), but only to a newer build
     (`CFBundleVersion`, a timestamp): opening the same or an older build
     changes nothing. If 拾新 isn't added yet, prepare System Settings and
     open its input source editor
     (`x-apple.systempreferences:com.apple.Keyboard-Settings.extension?InputSources`)
     instead of asking again.
  3. Start `Shixin -forget`, detached: five seconds later, once the
     opened copy and the staged upgrade have exited (LaunchServices
     registers a running app again), it unregisters every copy of 拾新 but
     the installed one. On one Mac (macOS 26.7.1), records of deleted
     copies (an upgrade's staging directory) stayed under 拾新's bundle
     id. Uninstalling forgets them all.
  4. Open the settings app's 开始使用 guide (`-welcome`). Until 拾新 is
     added, bring System Settings to the front.

  Preparing System Settings fixes two stale caches, both seen on a Mac
  with macOS 26.7.1 (2026-10-28), which otherwise needed a logout:
  - **The Keyboard pane's list.** It keeps its own copy of the input
    source list in
    `$(getconf DARWIN_USER_CACHE_DIR)/com.apple.Keyboard-Settings.extension/com.apple.IntlDataCache.le*`.
    That copy was two weeks old and lacked 拾新, so + didn't offer it while
    TIS itself knew 拾新. Quit System Settings and delete the copy; macOS
    rebuilds it when the pane opens.
  - **The input method launcher.** An older build had run as the input
    method from ~/Downloads. Afterwards selecting the installed copy did
    nothing until `imklaunchagent` was restarted (`pkill -x
    imklaunchagent`; launchd starts it again). Its `Refusing connection
    name` errors aren't evidence either way: some were Apple's PressAndHold
    ([recovery notes](macos-input-menu-recovery.md)).

  Both happen before any consent request: the dialog is in System
  Settings, and quitting it would dismiss the dialog. `Shixin -refresh`
  does the first alone, for the guide's 打开输入法设置.

  Sandboxed clients can also have their own stale input-source caches:
  `$(getconf DARWIN_USER_CACHE_DIR)/<client-bundle-id>/com.apple.IntlDataCache.le`
  and `.le.kbdx`. On that Mac, Slack's cache, three weeks old, omitted
  Shixin even though the shared cache, TIS and Keyboard Settings listed it.
  Restarting Slack, the menu service or reinstalling Shixin did not replace
  that disk cache. Quit the affected client, back up and remove only its two
  cache files, then reopen it. Slack rebuilt its cache with Shixin included.
  Do not reset the user's enabled sources to repair this cache discrepancy;
  clearing Keyboard Settings' separate cache does not refresh the client.

  The input menu also has a separate named pasteboard cache,
  `AppleInputSourcesInUIOrderPasteboard`. In the same incident, rebuilding
  Slack's disk cache did not restore the menu entry: TIS listed Shixin as
  enabled, but the menu-list API still returned only ABC and Squirrel.
  Clearing this named pasteboard and restarting `TextInputMenuAgent` restored
  Shixin to the menu list without logout. See
  [the recovery procedure](macos-input-menu-recovery.md) for exact APIs,
  verification and the limits of that finding.

  **Both are repaired automatically** (`Shixin -repair`, in
  `cmd/shixin/caches_darwin.go`), only once 拾新 is added and only where a
  cache lacks it: the guide runs it when it sees 拾新 added, and the
  post-install `-forget` after an upgrade. Each removal is logged.
  - The menu's list is read with the Carbon Pasteboard Manager
    (NSPasteboard sees its flavor but returns no data): a property list of
    input sources. If 拾新's mode isn't in it, clear the pasteboard and
    restart `TextInputMenuAgent`, which rebuilds it.
  - If a running installed app's cache lacks Shixin, invalidate
    `AppleIntlFileCacheModificationDatePasteboard`, Carbon's shared cache
    modification-date marker. Clients can then rebuild their disk caches
    on their next text-input activity without quitting. Then post HIToolbox's
    `AppleEnabledInputSourcesChangedNotification` through the distributed
    notification center. Otherwise an app can keep the parent input method
    disabled in memory even though its mode is enabled and its disk cache
    is fresh. The public TIS enabled-source notification alone does not
    invalidate this state. Explicit `-repair` also posts the invalidation
    when no disk cache is stale. These operations leave the general
    clipboard and `pboard` alone. Both names are undocumented; errors are
    logged and the fallback below remains available.
  - A detached, per-user locked helper (`-watch-caches`) scans every 5 s
    while a running regular app (one with a Dock icon) keeps a stale cache
    and gives up after 30 minutes; the front
    starts another at its next launch. It invalidates the marker once per
    run, not on every scan. Installation, repair, and front startup launch
    it; it exits when no such app remains or Shixin is disabled. All
    scanning and file removal run outside the input-method UI and key path.
  - Only regular apps count. Agents and extensions (WindowManager,
    AirPlayUIAgent, the wallpaper agent, a password manager's browser
    helper) have no text field, never rebuild their caches and never quit:
    on one Mac (2026-10-31) their October caches kept the helper
    polling for hours. Their stale caches are removed once
    they aren't running, and they are never listed.
  - The repair is silent: Settings shows no notice about it. The 开始使用
    guide says to quit and reopen an app that won't switch to Shixin. When
    the app quits, the helper removes only its two stale cache files.
    `-cache-status` reports the running regular apps with stale caches as
    JSON without changing caches, for diagnosis. Removal failures are logged; a remaining stale cache is
    never reported as refreshed. A fresh cache does not by itself prove that the
    client's input-method connection or key handling works.
  - Verified in the VM (2026-10-28): `TestMenuListRebuild` clears the list
    and sees it rebuilt with 拾新; a Safari cache rebuilt while 拾新 was
    uninstalled was removed once 拾新 was added, and Safari's next cache
    listed 拾新. `TestRemoveStaleCaches` covers the choice of files and
    later cleanup after an app quits. The detached watcher was checked in
    the VM with Calculator: it retained the running app's stale caches,
    rejected a duplicate watcher, removed only the two cache files after
    quit, and exited.
    The marker plus enabled-state invalidation was tested with PowerPoint
    16.114.3 on macOS 26.6.2: a missing-source cache rebuilt, Shixin displayed
    its own candidates and committed exact Chinese text without restarting
    PowerPoint or `pboard`. `TestCacheRefreshPreservesClipboard` checks unchanged clipboard
    change count and text, HTML, file-URL and binary flavors (VM only).

  - **Consent** (macOS 26, verified in the VM, 2026-10-28). Enabling shows
    a consent dialog inside System Settings, which may open behind other
    windows; that is how one first install ended up half
    enabled (the mode on, the input method off).
  - **Allow** adds 拾新 the first time a machine sees it. Afterwards
    (after Don't Allow, or reinstalling after an uninstall) a request shows
    the dialog again, but Allow adds nothing, so neither the installer nor
    the guide asks again.
  - **+ always works** once the pane's list is fresh: input source
    editor → + → Chinese, Simplified → 拾新 → Add, with no further dialog.
  - **The guide** polls `Shixin -added` (both sources enabled). Step 1
    explains the dialog; step 2's 打开输入法设置 runs `Shixin -refresh`
    and opens the editor.
  - `Shixin -uninstall` turns both sources off, stops the front and the
    engine, unregisters and removes the bundle; settings and learned words
    stay.
  - Selecting the source from another process fails with `-50` (spike
    2).
  - **Release: a disk image.** `scripts/package-macos.sh` builds
    `Shixin.dmg`, holding only Shixin.app (no Applications link: opening
    the app installs it, wherever it runs from) with 拾新's volume icon.
  - **Gatekeeper.** The script signs every binary and library with a
    Developer ID and the hardened runtime (no entitlements; typing, the
    settings app and shixin-ai on Metal verified in the VM), signs the disk
    image, and with `NOTARY_PROFILE` notarizes and staples the disk image,
    which notarizes the app inside. Opening the app from a downloaded,
    notarized disk image asks once ("Apple checked it for malicious
    software") and installs from the mounted volume (upgrade verified in
    the VM, 2026-10-31). A copy run translocated (a read-only copy under
    `AppTranslocation`) installs the same way. Without notarization a
    downloaded copy is blocked at its first opening until Privacy &
    Security → Open Anyway.
- **Update (no re-registration; verified in spike 2):**
  1. Stage the new bundle outside `Input Methods`.
  2. Atomically swap its `Contents` into the installed `Shixin.app` with
     `renamex_np(RENAME_SWAP)`. The `.app` directory, its path and the
     bundle id stay the same, so LaunchServices sees a modification, not an
     uninstall. No `lsregister`, no `TISRegisterInputSource` /
     `TISEnableInputSource`, so no consent dialog. The input source stays
     enabled and selected, even though the ad-hoc signature changed.
  3. At an idle boundary, stop the old front gracefully, restart the
     engine, and relaunch the front (`open -g … --args -after-upgrade`).
     The installer restarts the engine itself: while 拾新 is the selected
     input source, macOS may relaunch the front before that `open`, which
     then only activates it without the argument, and the old engine kept
     running old code (seen with the 0.1.3 in-app update). The front also
     restarts an engine that started before the installed engine binary
     changed (`staleEngine`), which covers updates done by older
     installers, and the supervisor backs off while another engine holds
     the lock instead of starting one every half second. Stop an open settings app too:
     it would keep running the old code (one model download still used
     the old downloader); the installer opens the new one.
  4. Once its IMK server is up, the new front re-selects its own input mode
     if it is the current one. Otherwise each already-focused app passes
     its first key through as plain text while it reconnects. With the
     re-select, the next key goes straight to the new instance.

  **Never delete or replace the `.app` directory.** LaunchServices treats
  that as an uninstall: the input source must be registered and consented
  to again, and while the selected input method is unregistered, macOS
  drops keystrokes. Keys typed during the restart itself pass through as
  plain text, never lost. Keeping the front's restart short (the engine is
  a separate process and isn't restarted) keeps that window small. Most
  updates only touch the engine helper and use the hot swap (§3.5), so the
  front keeps running.
- **In-app update (manual).** Settings → 关于 → 检查更新 asks GitHub's
  latest-release API for the newest published release (unauthenticated; the
  version from the tag, the release notes, and the `Shixin.dmg` asset's
  URL, size and `sha256` digest). Nothing checks automatically or in the
  background: the click is the only network access (privacy, AGENTS.md).
  `internal/update` (no MyGo) compares dotted numeric versions and
  downloads into `~/Library/Caches/Shixin/Updates/<version>/`, as
  `.partial` renamed once the size and the digest match; downloads are
  untrusted data until verified. Builds with a pre-release suffix or `dev`
  can check but don't offer to install.

  The settings app then runs the installed front, detached, as
  `Shixin -apply-update DMG -update-version V`, because the upgrade stops
  Settings. The front mounts the image read-only (`hdiutil attach
  -nobrowse -readonly`, always detached afterwards) and fails closed
  unless: `spctl` accepts the disk image as a notarized, stapled primary
  signature and `codesign --verify --strict` passes on it; the volume's
  `Shixin.app` passes `codesign --verify --deep --strict`; its
  TeamIdentifier equals the installed bundle's (read from the installed
  bundle, so forks signing with their own team work) and is a real team;
  its bundle id is ours, its `CFBundleShortVersionString` is the version
  the release announced and its `CFBundleVersion` is newer than the
  installed build. An ad hoc installed copy has no team, so it can't update
  itself. The install is the Update above (staged copy, `Contents` swap,
  front relaunch), after which the front reopens Settings on 关于 and
  deletes the download. The release notes are shown as plain text (heading
  marks, emphasis and code ticks dropped).

  `-apply-update` is a child of the settings app it has to stop, and
  macOS's `pgrep` skips its caller's ancestors unless given `-a`; without
  it the old Settings survived the upgrade. Verified in the VM
  (2026-10-31): a Developer ID 0.1.0 test build checked, downloaded,
  verified and installed v0.1.1 in place, with 拾新 still selected. The
  reopened Settings was v0.1.1, which predates `-page` and exited at once;
  releases from then on have the flag.

- **Bundle metadata:** localized `InfoPlist.strings` for the bundle and
  input mode names (otherwise the raw source ID is shown). The English
  input menu keeps the Chinese brand visible as 拾新 (Shixin); the settings
  bundle has its own Chinese/English names. `internal/ui/brand` draws an
  original, font-independent 拾 mark, shared with the settings About page
  and temporary tray anchor. `scripts/build-macos-icons.sh` renders the
  app ICNS for both bundles and a 16-point input-source TIFF with 1x/2x
  representations. `TISIconIsTemplate` lets macOS tint the input-source
  icon for light/dark and highlighted menus. Generated assets stay in
  `build/`, not source control. Verified in the VM's input menu, System
  Settings and settings-app Dock icon. macOS caches input-source metadata
  within a login; after an in-place upgrade, the VM checks restart
  `TextInputMenuAgent` / `TextInputSwitcher` to refresh those icons/names.
- **Supervisor:** the front launches and restarts the persistent helper;
  its coordinator runs the builder and worker hot swap without MyGo.
- **Candidate placement:** `candview.Measure` measures the visible card,
  including labels, comments, preedit, padding and border, with MyGo's text
  metrics. Per-state text measurements are cached; repeated measurements
  allocate nothing. Both horizontal and vertical layouts use this size,
  rather than the larger transparent flyout's frame. Cocoa supplies each
  `NSScreen.frame` and `visibleFrame` on the main thread. A platform-free
  function selects the screen containing the caret (nearest screen if the
  reported caret is in a gap), clamps the panel to its work area, and puts
  it above the caret line when there is no room below. The caret's full
  height is respected, including zero-height rectangles. Coordinates stay
  in Cocoa's global bottom-left space until the final conversion to MyGo's
  top-left space about the primary screen's height; left and higher
  secondary displays can have negative window coordinates. Screen work
  areas are refreshed on view updates, so Dock and display changes apply.
  The transparent shadow margin may extend outside the work area. If the
  panel itself exceeds the work area it is pinned to that area's origin;
  fitting or scrolling oversized panels remains deferred.
  Verified in the VM (the panel flips above the caret at the bottom of
  the screen); multiple displays and Intel Macs are untested.
- **Fallback if MyGo + IMK can't share a process:** a minimal Swift IMK front
  with the same client core logic. The engine is already separate, so
  nothing else changes.

### 5.6 Windows TSF DLL

**Implementation status:** `frontends/windows-tsf` implements the native
COM front, fixed-buffer policy/codec core, background overlapped pipes,
focus and password/PIN guards, and serialized edit sessions. `Shixin.exe`
implements the candidate/tray host and engine supervisor, sharing the
settings app and candidate views with macOS. x64/ARM64 builds and native
COM smoke tests are configured in CI; the DLL has been cross-compiled for
x64, and the portable core passes ASan/UBSan locally. Actual Windows typing
and registration have been exercised in the ARM64 VM, including Start search
at low integrity. [Windows development](windows.md) describes verified coverage
and remaining release work. Authenticode verification remains required before release.

- **Registration:** an ordinary `Shixin.exe -register` invocation requests UAC
  for an isolated machine step: HKLM COM registration,
  `ITfInputProcessorProfileMgr::RegisterProfile` (disabled by default), and
  keyboard/immersive categories. Back in the original caller, `InstallLayoutOrTip`
  enables only that user's keyboard. Unregistration disables the caller's
  keyboard while the machine profile still exists, after UAC approval and
  before the elevated child passes a user/administrator-only event gate.
  Cancellation leaves the keyboard list untouched; machine removal failure
  restores the keyboard only if it was previously enabled and its machine TIP
  still exists. Post-removal COM cleanup failures leave it disabled and support
  cleanup retries. Legacy HKCU COM cleanup happens after successful removal,
  including for disabled keyboards. Profile registration
  and enablement use the Windows APIs.
  This is a supported machine installation with user enablement, not a
  registration that bypasses Windows' administrator requirement.
- **Restricted apps:** the DLL grants packages read/execute access, and its
  worker resolves unredirected LocalAppData without verifying its private
  parent directory. Connection, discovery and identity checks stay off the
  key path. It advertises immersive support without claiming UIless search
  integration or secure-desktop support.
- A C++17 in-proc COM server: `ITfTextInputProcessorEx`, `ITfKeyEventSink`,
  `ITfThreadMgrEventSink`, `ITfCompositionSink`, display attributes and input
  mode.
- **Contains:** VK → keysym mapping, policy lookup, speculation/barrier, the
  overlapped pipe client and async edit sessions. No UI, no Rime.
- **`DllMain` does nothing heavy;** it connects lazily on a background
  thread. This matters for games and short-lived processes.
- **Test matrix:** UWP/AppContainer, elevated apps, Office, Chrome/Electron,
  Explorer, Start search, windowed games. Builds for x64 and ARM64, signed.

### 5.7 fcitx5 addon

`InputMethodEngineV2` with the same client core in C++. Non-blocking socket
in fcitx5's event loop, `InputPanel` updates, and a classicui theme exported
from the active skin.

### 5.8 Data and distribution

| | macOS | Windows | Linux |
|---|---|---|---|
| librime | Our build (universal) with `librime-shixin` linked in, no Lua, in Frameworks | Our MSVC build with `librime-shixin`, no Lua | Our build bundled in `/usr/lib/shixin` (not the distro's, which may ship Lua and a different version) |
| Shared data | `Contents/SharedSupport/rime-data` | `<install>\data` | `/usr/share/shixin/rime-data` |
| Package | Signed, notarized `.pkg` | Signed installer + DLL registration | deb/rpm/AUR |
| Updates | MyGo signed delta updates. Dictionary updates via builder + hot swap | | Distro/our repo |

## 6. Settings app

A separate process, opened from the input menu (macOS), the tray
(Windows), `shixin://settings`, or a launcher in Applications / the Start
menu.

| Page | Contents |
|---|---|
| 常用 General | Scheme (全拼 / 双拼 variants), candidates per page, layout, 中英切换 key, initial mode |
| 外观 Appearance | Skin gallery with live preview (same candidate view), font/size, light/dark, skin editor |
| 输入 Input | 模糊音, 以词定字 keys, page keys, 中英混输, emoji, 简繁, 智能排序 (off / 保守 / 标准, model download, §7.2) |
| 快捷键 Shortcuts | Toggles for 中/英, 全/半角, 简/繁, 标点, schema switch |
| 词库 Dictionary | Custom phrases, forget a highlighted word's learning, .scel/text import, user dictionary export/clear |
| 应用 Apps | Per-app default mode |
| 同步 Sync | Folder or WebDAV, idle-window scheduling |
| 性能 Performance | Live latency histograms (§3.7), stall counter, slowest recent keys, Lua component costs |
| 高级 Advanced | Cloud input (off), open Rime folder, logs, about/update |

First-run guide: enable the IME in System Settings (macOS), 全拼/双拼, fuzzy
pinyin, skin, and importing an existing Sogou/Squirrel/Weasel/fcitx5-rime
dictionary.

**Built (M2, `cmd/shixin-settings`):** a System Settings–style window (hidden
title bar, translucent sidebar with colored icons, pages of grouped cards)
with 常用, 外观, 输入, 快捷键, 词库 (custom phrases), 应用, 性能 (live engine
histogram and counters) and 关于 (folders, restore defaults,
privacy). Changes apply as they are made, 0.7 s after the last edit, with a
status pill instead of a Save button. Changes to the generated Rime
files request a builder/standby hot swap through the coordinator (§3.5). Through `settings.json`, appearance changes reach
the candidate window at once, and the engine picks up per-app rules and
auto spacing live. The skin preview and
the candidate window draw the same view (`internal/ui/candview`, skins in
`internal/ui/skin`). Views are tested headlessly with `ui.NewTester`, and
`go test ./cmd/shixin-settings -snapshot DIR` renders every page in Chinese
and English, light and dark. Chinese filenames stay unchanged; English
filenames add `-en` (for example, `general-en.png`, `general-dark-en.png`).
On Windows the candidate card is bounded by the caret monitor's work area
and the prewarmed 1400×560-DIP canvas, including shadow margins. Oversized
content scrolls in both directions; a changed highlight is revealed while
manual scrolling remains available between engine updates. The native hit
region follows the visible card. This requires no flyout resize on a key path.
For each double-pinyin layout, 常用 offers a reference keyboard chart with
initials, finals (including the semicolon key) and vowel-only syllable
examples. Its content scrolls in a small window while the close button
stays visible. The chart describes the base layout before fuzzy-pinyin
extensions and does not change settings. Chart metadata lives in
`internal/settings/shuangpin.go`; engine tests check every displayed
mapping using rime-ice's actual spelling algebra and a scratch dictionary.
The snapshot command also renders every chart in both languages and themes,
plus scrolled charts at the 720×480 minimum window size.
General also offers **同时支持全拼 / Also accept full pinyin** for shuangpin,
off by default (`full_pinyin_fallback`). It preserves full syllables beside
the selected layout's codes, so full and double pinyin can be mixed within
a phrase. The compiler applies fuzzy rules first, derives an uppercase copy
of each full spelling (excluding reserved `xx`), preserves common ü/u/v
aliases on that branch, then restores lowercase after the layout's conversion.
The layouts use circled-capital markers, so these ASCII capitals remain
separate during conversion. This adds spellings to the offline prism, with
no new translator or runtime spelling logic. Enabling it rebuilds the active
prism through the normal builder/hot-swap path, including source-less bundles.
The full-pinyin schema is unchanged; its settings control is hidden while
the stored preference is retained. Ambiguous codes may add candidates or
change ranking; both readings remain selectable.
快捷键 also offers optional second/third-candidate shortcuts, off by
default: Ctrl or Option/Alt plus `;` and `'`. They select items 2 and 3 on
the current page, regardless of the highlight. The compiler writes
`has_menu` bindings that forward `Control+2` / `Control+3` to librime's
selector. The modifier prevents the recognizer from appending digits in
number, Unicode and calculator input. Plain semicolon (including `ing`
in some shuangpin layouts) and apostrophe delimiters keep their meaning.
With no menu or in English mode, the shortcuts pass through; their idle
policy needs no engine wait. The existing composing ASK budget applies.
The candidate-shortcuts group also offers forgetting the highlighted word's
learning: Ctrl+Shift+Backspace by default, Ctrl+Delete, or none. Dictionary
shows the configured key and links to Shortcuts. The compiler writes
`editor/bindings` using librime's `delete_candidate` action, disables its
inherited Ctrl+Delete when not selected, and binds Shift+Delete to ordinary
composition deletion so Shift-as-Control fallback cannot forget words.
This changes no IPC or client key handling. Idle/English keys pass through;
the composing shortcut uses the existing bounded ASK. Forgotten learned
entries/ranking persist across restarts. Bundled words may remain in the
candidate list, and custom phrases are removed through Dictionary's editor.
常用 offers a default punctuation style (`punctuation`: `chinese` or
`english`, Chinese by default) for new sessions. It sets `ascii_punct`'s
schema reset value; the configured punctuation shortcut still toggles the
current session without changing the preference. Chinese input can thus
start with `，。` or `,.`; English input continues to pass punctuation
through normally.
Not yet: skin editor, .scel/text import, user dictionary
export/clear, the front's own latency on the 性能 page, sync and cloud input
(M5), the first-run guide.

## 7. Features (Sogou parity map)

The [Sogou Mac settings comparison](sogou-comparison.md) records the observed
controls and Shixin's current support separately from the plans below.

**From librime + rime-ice data (configured in settings):**
- 全拼, 双拼（自然码、小鹤、微软、搜狗、智能 ABC、紫光、拼音加加）, 简拼, 模糊音
- Word learning and frequency adjustment (自造词, 调频)
- 中英混输, 中/英标点, 全/半角, 简/繁
- page keys, `/` symbols, emoji
- 部件拆字 lookup, custom phrases

**From `librime-shixin` (native replacements for rime-ice Lua, §3.8):**
- 以词定字
- `rq`/`sj`/`xq` dates and times
- `R123` uppercase numbers
- `U` Unicode
- calculator
- pinned candidates
- long-word priority

**Built by Shixin:**
- Settings app and first-run guide
- Skins and skin editor
- Mode indicator bubble (macOS): a small card (26 pt, about a line of
  text) with 中 / 英 (En in the English UI) beside the caret for 900 ms
  when the confirmed mode differs from the last one: a Shift toggle, or a
  per-app mode that switches on focus. Focusing a field or selecting 拾新
  in the same mode shows nothing; macOS 26 already shows its own indicator
  for an input source switch, and a bubble on every focus was too loud. It
  uses the candidate skin's background, 中 in its highlight color, and the
  same screen/work-area clamping as the candidates. Repeated results do not
  restart it; candidates take priority. Focus loss, mouse clicks,
  input-source changes, engine disconnects and secure fields dismiss it.
  Only engine results request a mode; focus alone never guesses one.
  Lifecycle and allocation tests are headless.
- Per-app mode and secure-field handling
- Auto spacing between Chinese and English (§7.1)
- Right-click delete word
- `.scel` import, user dictionary import/export, migration from other IMEs
- Sync
- Performance page
- 智能排序: candidates reordered by a local model from what was just
  written, strongest for 简拼 (§7.2)
- Typing stats (later)

**Cloud input (opt-in, M5):**
- Fully asynchronous and never awaited. A result that arrives within ~300 ms
  of a pause is inserted as candidate 2 (☁); otherwise it's dropped.
- Never used in secure fields.
- Pluggable providers; public Sogou/Baidu/Google endpoints are unofficial, so
  the default provider must be chosen carefully.

### 7.1 Auto spacing between Chinese and English

Shixin inserts a space where Chinese meets English or digits: `用 iPhone
拍照`, `共 3 个`, not `用iPhone拍照`, `共3个`. This is the pangu.js
convention, applied while typing. Off by default; one switch in the
settings app turns it on.

**The rule: a space only when Shixin knows both sides.** A missing space
costs the user one keystroke. A wrong space is an edit they have to find and
delete, and it makes the feature feel broken. So every uncertainty resolves
to *no space*: after a click, a window switch, a paste, or anything else
Shixin didn't see, the state is *unknown* and nothing is inserted until it
is known again.

#### What counts as a boundary

Each character is in one class:

| Class | Characters |
|---|---|
| `CJK` | Han (all blocks), kana, Hangul, Bopomofo |
| `Latin` | Letters outside CJK: ASCII, Latin-1 and Latin Extended, Greek, Cyrillic |
| `Digit` | `0`–`9` |
| `Space` | Space, tab, newline, and the start of a line |
| `Other` | Punctuation (ASCII and full-width), symbols, emoji |
| `Unknown` | Shixin doesn't know |

A space goes between `CJK` and `Latin` or `Digit`, in either order: `第
3 章`, `2026 年`. Nothing is inserted next to `Space`, `Other` or
`Unknown`, or inside a commit. A commit is taken
as given: `2026年10月26日` from `rq`, or `iPhone手机` from a custom phrase.

#### Where it happens: in the client, with no round trip

The decision needs the character left of the caret. Only the client sees
everything that changes it: our commits, the English keys it passes to the
app, clicks, and focus changes. The engine sees none of the passed keys.
So the state lives in the client core (`internal/client`, `spacer`).

- **Commit:** before `Applier.Commit`, if the left class and the commit's
  first character form a boundary, the client inserts `" " + text` in one
  call, which makes one undo step in the app. The commit's last character
  becomes the new left class. This covers Chinese after English (`iPhone` →
  `很好`), mixed-English candidates after Chinese, and Shift's
  commit-the-code (`中文` + `hello` + Shift → `中文 hello`).
- **A passed key:** an English letter typed in English mode is PASS; the app
  inserts it itself. If the left class is `CJK` and the key is a letter or
  a digit, the client takes that one key and inserts `" " + ch`. Every
  other passed printable key only updates the left class.
  The PASS ordering barrier (§3.3) has already applied pending commits by
  then, so the left class is current.
- **Cost:** one class lookup and a branch per key. The engine is never
  involved, nothing allocates, and the only extra app call is the insertion
  that happens anyway. This is local classification as in R1.

The client keeps a short history of the classes it knows (16 entries), not
just the last one. Backspace pops an entry, so `中文 a` with Backspace, then
`b`, gives `中文 b`, and an empty history means `Unknown`. If Backspace
removes a space Shixin inserted, the user didn't want it: the next
character gets no space. A commit containing joining code points (emoji
sequences, combining accents) keeps only its last class, since one
Backspace deletes several code points.

#### Reset: back to `Unknown`

Anything that may move the caret or change the text, other than our own
commits and the keys we saw, clears the history.

| Event | macOS source |
|---|---|
| A mouse press anywhere: in the text, another window, a menu, the Dock | `NSEvent addGlobalMonitorForEventsMatchingMask:` for left, right and other mouse-down. Presses only, never mouse moves, which fire constantly and move no caret |
| Another app becomes active | `NSWorkspaceDidActivateApplicationNotification` |
| Another text field or window in the same app | IMK `activateServer:` and a new client object (`setClient`) |
| The app ends the composition | IMK `commitComposition:` |
| Spaces, Mission Control, screen lock, sleep, user switch | `NSWorkspaceActiveSpaceDidChangeNotification`, `…ScreensDidSleep…`, `…WillSleep…`, `…SessionDidResignActive…` |
| Shixin stops being the input source | IMK `deactivateServer:`, and `kTISNotifySelectedKeyboardInputSourceChanged` (macOS 26 doesn't always call `deactivateServer:` on a switch) |
| A key that moves the caret or edits: arrows, Home/End, Page Up/Down, Tab, Escape outside a composition, Forward Delete | `handleKey` |
| Return and Enter | `handleKey`; the left class becomes `Space` (start of a line) |
| Any key with Command, Control or Option (paste, undo, select all, word moves, Emacs bindings, Option characters), except Command-C and Command-S, which leave the text alone | `handleKey` |
| A character outside the keymap (another layout, a dead key) | `handleKey` (`xkVoidSymbol`) |
| A secure field | front's Secure Event Input check; local client guard before any reply (§5.5) |
| An app with spacing off | engine, per context (below) |
| No key for 30 s | timestamp. Covers changes Shixin can't see: dictation, autocorrect, Universal Control, scripts, an app editing its own text (a chat box clearing after Send) |

All of these are cheap. The mouse monitor and notifications are installed
once at startup on the main thread, and reset handlers set a flag the
spacer reads on the next key. On macOS, modifier flags changes that Rime
doesn't watch (including Command and Option) also reset it: Safari can
consume Cmd+A before IMK sees the key-down, leaving only Command's flags
changes. This conservatively resets even for Cmd+C and Cmd+S; readable
fields can recover the left character at the next boundary. Shift and
Control keep their existing Rime notification path.
The input-source notification also finishes
the last marked text when switching away (§5.5), through the same spacing
commit path, then clears spacing history for the next field. Global
monitors for mouse events need no
Accessibility permission, unlike key events (verified on macOS 26.6,
`docs/spikes/m2-mouse-monitor.md`; re-run on new releases).

#### Verifying with the app, where it can answer

Tracking is enough most of the time, and the resets keep it safe. Where the
app can report its own text, Shixin also checks before it acts, at the two
moments a wrong answer would matter:

- **Before inserting a space,** it reads the character left of the caret:
  `selectedRange` and `attributedSubstringFromRange:` on the IMK client. If
  that character isn't the expected class, the space is skipped and the
  history is corrected.
- **When the history is `Unknown`** (after a reset) and a commit or a
  passed letter or digit could start a boundary, it reads the character to
  learn the class, so `iPhone|`, then a click elsewhere, then a click back
  and `很好`, still gets `iPhone 很好`. While composing, it reads the
  character before the marked text.

These are synchronous calls into the app, like the caret rect. They happen
at most once per boundary, never per key, and they get a budget: a client
that doesn't answer (`NSNotFound`, nil: Terminal, some Electron apps) or
takes over 2 ms twice is marked unverifiable for the session, and tracking
alone decides. Measure in the VM's app matrix before enabling it per app.

Apps also answer wrongly, as an experimental Squirrel fork with the same
feature found in daily use.
A mismatch is a correction only when it can't be explained by one of
these:

- **Stale answers.** Slack and Google Docs apply `insertText` late and
  report the text from before Shixin's last insertion: `中文`, `ni`
  committed with Return, then `里` reads `文`. An answer matching the class
  left of the caret before the last insertion is taken as stale: the
  tracking wins and the space goes in. Two stale or slow answers make the
  app unverifiable.
- **A fake caret.** Ghostty's `selectedRange` is always `{0, 0}`, the start
  of an empty text. When the history knows characters were typed left of
  the caret since the last reset, "start of the text" can't be true, and
  the app is unverifiable at once.

Windows and Linux have better sources than tracking: TSF's
`ITfTextEditSink` reports every selection change and edit (a reset), and an
edit session reads the text before the caret. fcitx5 provides the
surrounding text and cursor. There the tracker is the fallback for apps
that report nothing. These clients implement the same small state machine,
like the policy lookup (§3.3). The rules stay in one table, and
`internal/client/testdata/spacing.txt` holds event-sequence vectors that
every client must pass (`noread; key a; click; commit 中文 => a中文`: no
space after a click when the app can't be read).

The Windows TSF client implements this state machine with fixed history and
buffers. It consumes the existing `ResultAutoSpace` flag, including live
global and per-app changes. Commits and misprediction repairs add any prefix
inside their queued write session; a UTF-16 range reads at most two code
units before the composition start or caret. Replacing selected text never
adds a prefix. Secure/read-only fields are checked again before any read or
insertion. Focus, input-source changes, external edits, navigation,
shortcuts, disconnects and idle time clear the history. Known passed edits
retain it; Chromium's separate selection notification after Backspace is
ignored only when its caret matches the saved range from that edit.

A passed letter/digit at a possible boundary requests the pooled write edit
with `TF_ES_ASYNCDONTCARE`, without an engine round trip or a new wait. If
TSF runs it immediately, the DLL can replace the key with one `SetText`
insertion containing the space and character. If the edit is deferred,
refused, or another edit is pending, the key passes unchanged and the optional
space is skipped; a deferred callback is cancelled before the key passes.
While enabled, the DLL checks passed printable keys with `ToUnicodeEx`
without changing dead-key state before tracking or taking them, so another
keyboard layout cannot turn into a US character or a guessed boundary.
Enabling spacing clears the unverified history from when it was off. The
portable C++ tests run the shared vectors and allocation/latency checks;
native TSF fixtures cover delayed edits, selection changes and secure fields.

#### Settings and per-app rules

- `settings.json`: `auto_space` (off by default) and the apps with spacing
  off, defaulting to the 应用 page's developer tools (terminals and code
  editors, where a space can change meaning).
- The engine decides per context, as for per-app modes. Spacing is off in
  secure fields and in the listed apps, and every Result carries the
  answer in a flag, `ResultAutoSpace`. This is append-only; old clients
  ignore it. Clients don't parse settings. The engine watches
  `settings.json` and applies per-app rules and this switch live, without
  a restart; a client sees a change with its next result (at the latest,
  the next focus change).
- The client's secure guard overrides `ResultAutoSpace` immediately,
  including stale queued results. While secure, no passed key or commit
  reaches the spacer or its `TextReader`. Both transitions clear spacing
  history; leaving secure waits for the engine's spacing setting again.
- Settings app: a 中英文、数字之间自动加空格 switch on the 输入 page, and a
  不自动加空格的应用 list on the 应用 page.

#### Tests

- `spacer` unit tests: boundaries per class pair, the history with
  Backspace, removing an inserted space, every reset event, the timeout,
  and that the key path doesn't allocate (`testing.AllocsPerRun`).
- The shared event-sequence vectors, run against the Go and Windows C++
  cores; the fcitx5 client must pass them when implemented.
- VM: the app matrix (TextEdit, Notes, Safari, Chrome, VS Code, Slack,
  Terminal), checking for no space after a click, a window switch, a
  paste and the timeout, plus the verification timings per app.

Not in the first version: spacing against the character *right* of the
caret (`|iPhone` then `我的`), which needs the app's text and is only
reliable where it can be read; and pangu's finer rules (units such as
`5%`, quotes, brackets), whose characters stay `Other`.

### 7.2 Context-aware reranking (智能排序)

Shixin reorders Rime's candidates using what the user has just written. A
small language model runs locally on the GPU/NPU and scores each candidate
as the continuation of the text already committed. A deterministic policy
then decides the new order: it promotes #1 and picks the rest of page 1.
librime still owns the menu, so paging, selection, learning and the other
filters keep working. The feature is opt-in and local only. The user
never sees a reorder happen: the engine holds a key's result for up to
16 ms for the order, then sends it once, reordered or, if the model was
too slow, in rime's order. The app never waits (R1, R4, R5).

#### Why, and with what model

Measured zero-shot in `docs/spikes/m2-rerank-models.md` (2026-10-27). The
test text is simplified FineWeb-2 web text. The options are same-code
words in rime-ice weight order, an approximation of librime's
context-free menu. Settings were chosen on one half and tested on the
other.

| Input | rime-ice: first word / first page | With the model (ERNIE-4.5-0.3B, stable policy) |
|---|---|---|
| Full pinyin (top 20 seen) | 83.7% / 98.3% | 96.0% / 99.9% |
| 模糊音, 7 pairs (top 50) | 79.4% / 96.7% | 95.4% / 99.8% |
| **简拼 initials, `tw` → 台湾 (top 50)** | **29.4% / 61.3%** | **79.0% / 95.5%** |

简拼 is the case that matters most. There the right word is often on page
2–10, and moving it onto page 1 is most of the value.

Model choice:

- **The model is `baidu/ERNIE-4.5-0.3B-Base` (361M, Apache-2.0)**, used
  zero-shot to begin with. It beat Qwen3-0.6B-Base (596M) on accuracy and
  speed. It also beat every model under 300M that was tried: the uer GPT-2s
  (60M/102M), whose weights have no license; Wenzhong-GPT2-110M; and
  MiniMind 26–64M, which does worse than rime.
- It scores **log P(candidate | context)**: the sum of the candidate's
  token log-probabilities after the context. This needs no prompt and no
  training, and its large vocabulary often holds a whole word as one
  token, which keeps the per-key work small.
- Rejected:
  - Decision models such as Laya: no signal zero-shot.
  - ELECTRA-small: +4.5 points only, against +12 for ERNIE.
- The scorer interface is "log-probability of a string after a context",
  so a fine-tuned or distilled model can replace this one without changing
  the engine.

#### Overview

```
 commit "我们去"                                  key "tw" (简拼)
     │                                                  │
 engine ── Context{app slot, rev, text} ──▶ shixin-ai: prefill once,
     │                                   keep its KV cache and next-token log-probs
     │                                                  ▼
     │   librime menu ─▶ shixin_rerank filter: rime order unchanged; exports the
     │                    first N same-span candidates (text, prior, flags) to its slot
     │── Score{session, export seq, app slot, rev, N candidates} ──▶ shixin-ai:
     │   (the key's Result is held)        prune by first-token bound, score the best M,
     │                                      fuse with dictionary priors, compute the order
     │◀── Rerank{session, export seq, permutation} ── (p50 9 ms)
 engine: still held, same export? → write the permutation to the slot, refresh
     the composition (private key) → the filter applies it → Result ──▶ client
 no order within 16 ms, or a newer request first? → Result in rime's order;
     the order that comes later is dropped
```

#### Context

- **Per-app context (engine, Go).** Context belongs to the app, not to the
  input context. Each app id from `FocusIn` gets a fixed ring of committed
  text, 128 runes, filled from `ReadCommit` in `reply` for whichever input
  context is in that app. Each ring has a revision counter.
  - The engine preallocates 16 rings at startup and reuses the least
    recently focused one for a new app. No allocation per key or per focus.
  - Switching apps switches rings. Returning to an app continues its own
    context: a chat keeps the conversation, an editor keeps its document.
  - Another field in the same app keeps the ring and appends a newline as a
    soft break, since the model reads a newline as a paragraph boundary.
- The model sees the ring's tail: up to 64 tokens. After every commit the
  engine sends `ContextUpdate` to `shixin-ai`, which prefills it right
  away; typing the next code takes longer than that.
  - It identifies the ring by slot number only. App identifiers never leave
    the engine.
- **Never recorded:**
  - Secure fields. Their app's ring is left untouched.
  - English typed in ASCII mode, which is `PASS` with no IPC.
- **Caret moves.** The client makes idle arrows, Home/End, Backspace and
  mouse clicks `PASS_NOTIFY` (async, as for Shift today). The engine then
  appends the same soft break: earlier text is kept, but a caret move or
  deletion means what follows may not continue it.
- **Lifetime.** Rings live in memory only. They are dropped when 智能排序 is
  turned off or the engine restarts, and evicted per app by LRU.
- **Not in the first version:**
  - Surrounding text read from the app at focus-in. Platforms differ, and
    on macOS it's a synchronous round trip off the key path.

#### Inside librime (`plugins/librime-shixin`)

- **`shixin_rerank` filter.** It is first in the filter list, on `abc`
  segments. It sees librime's own candidates in simplified text, and
  emoji, 繁体 and `uniquifier` apply after the reorder. A planned
  `shixin_pin_filter` would come after it, so pins win.
  - **Export.** When the menu of the segment being translated is first
    built, it buffers the first N candidates with the same span as #1
    (start and end). It writes their text, flags and prior into the
    session's slot. The prior is the dictionary entry's log weight
    (`Phrase::weight()`). `quality()` can't serve: librime computes it as
    `exp(weight)` plus the translator's initial quality plus a length
    term. It also writes a hash of the whole input and #1's span,
    and an export sequence number. An unchanged export (the same hash,
    texts and flags) keeps its number, so a refresh doesn't invalidate the
    order imported for it.
  - **Apply.** If the slot holds an imported permutation for the current
    export sequence, it emits the first N candidates in that order.
    Everything after N follows unchanged. Otherwise it passes the
    translation through untouched. A permutation that isn't a permutation
    of the N entries, or that moves a fixed entry, is rejected and counted.
  - N is 20, or 50 when the segment's code is initials only: letters with
    no vowel (`tw`, `zgr`). Mixed codes such as `zhongg` get 20. In
    shuangpin a two-consonant syllable also counts, which only costs a
    longer export.
  - Only `script_translator`'s Chinese `phrase`, `sentence` and
    `user_phrase` candidates move. Everything else is fixed and keeps its
    place: translator candidates (date, number, Unicode, calculator),
    English, `cn_en`, and custom phrases, which are `table` candidates.
    Learned `user_phrase` entries are flagged for the policy.
- **`shixin_rerank_processor`.** It is first in the processor list. It
  accepts one private keycode (`0x01fffffe`, outside X11 keysyms and
  Unicode), sent only by the engine. It calls
  `Context::RefreshNonConfirmedComposition()`, so the filter runs again
  with the imported order. The engine drops client keys with that code.
- **Slots.** One fixed-size slot per session (4,964 bytes), in anonymous
  memory the engine maps outside the Go heap at startup: 256 slots. A
  session created beyond that doesn't rerank. Each slot holds:
  - the export: sequence, hash, 64 entries and a 4 KiB string arena
  - the import: enabled flag, sequence and permutation
  - counters: applied and rejected

  The layout is in `rerank_slot.h` and mirrored by `rerank.Slot`, with
  offset tests on both sides. The engine registers the region once
  through an exported C function (`shixin_rerank_attach`), which checks
  the slot size. Each session learns its slot index from the context
  property `shixin_rerank_slot`, set when the session is created. Only the
  engine thread touches a slot while librime runs, so there are no locks
  and no callbacks into Go.
- **Budget.** It does no allocation after init on the common path, as for
  every `shixin_` component. With the feature off, or no permutation, it's
  a pass-through. Buffering 50 candidates makes librime translate further
  than the visible page. That cost is measured and gated (below), and N is
  lowered if it breaks the engine budget.

#### `shixin-ai` (new process)

- **A new process, in Go with no MyGo and no cgo** (`cmd/shixin-ai`; the
  scorer and server are `internal/ai`, the llama.cpp binding
  `internal/llama`).
  - It holds the model: 368 MB at 8-bit (Q8_0 GGUF).
  - A crash or stall in the inference runtime can't reach librime.
  - The engine's heap stays small (§3.2).
- **Lifecycle.**
  - The engine's coordinator starts it when 智能排序 is on, and restarts
    it with backoff. It isn't restarted when it exits with status 3 (no
    supported accelerator) or 4 (model missing). It exits when its stdin
    closes, so it never outlives the coordinator, even after a `SIGKILL`.
  - The setting applies live, without a new generation (it doesn't
    change the Rime configuration). The coordinator's settings check
    starts or stops `shixin-ai`. The active worker's settings watcher
    calls `Engine.SetRerank` and sets the strength on its `Remote`. Off,
    the filter passes candidates through, and every app's context is
    forgotten.
  - It listens on `ai.sock` in the engine's 0700 runtime directory and
    serves one engine at a time; a newer connection replaces the old one.
    The active worker connects only once promoted, so a standby
    generation never takes it over during a hot swap (§3.5). (The first
    design had `shixin-ai` connect to the engine; with generations, the
    engine side knows when it is active.)
  - The engine's side is `rerank.Remote`. It sends without blocking the
    engine thread (preallocated frames, dropped when the queue is full or
    nothing is connected), and replays each app's latest context after a
    reconnect.
  - It never talks to clients, opens no network sockets, and never sees the
    user dictionary.
  - It runs the model on one thread with user-interactive QoS.
- **Runtime: llama.cpp (MIT)**, pinned at v0.7.0 and loaded with purego
  through its C API, like librime. It's built by us
  (`scripts/build-llama.sh`), with no Homebrew headers: shared libraries
  with the Metal shaders embedded, and no tools, network, OpenMP or BLAS.
  - Backends: Metal on macOS; Vulkan on Windows and Linux; CUDA only if
    the user's system provides it, never bundled.
  - It supports ERNIE 4.5 (`convert_hf_to_gguf.py --outtype q8_0`).
  - It has shared-prefix sequences: copying the context's sequence to an
    option's shares KV cells instead of copying them (unified KV cache).
  - One runtime on all three platforms. The budgets below are enforced by
    the performance gates, not by a separate benchmark phase.
- **GPU/NPU only.**
  - llama.cpp is offered only the first GPU/accelerator device, with every
    layer: all 19 layers, the output layer and the KV cache are on it.
    Without such a device `shixin-ai` refuses to start (status 3), and
    Settings show "no supported accelerator".
  - The softmax of every output runs on the GPU too, in a backend sampler
    of ours that only applies `softmax` (purego callbacks, called
    synchronously while llama.cpp builds the graph). llama.cpp's own
    `dist` sampler also draws a token, and its cumulative sum over the
    103k-token vocabulary tripled decode times on Metal.
  - Tokenization and the policy run on the CPU and take microseconds. So
    does reading the input embedding row, which llama.cpp keeps in host
    memory by design.
- **Per app.**
  - On `ContextUpdate` it prefills that app slot's context (the last 64
    tokens), then keeps its KV cache and next-token log-probs. A context
    that continues the previous one decodes only the new tokens, up to 96,
    before it is cut back to the last 64. It keeps 4 app slots warm, LRU,
    so switching between a few apps doesn't need a new prefill.
  - Requests are taken in batches: after each arrival, everything queued
    is read first, keeping the newest `ScoreRequest` per input context and
    the newest context per app. Score requests are answered first,
    prefilling their app's context if it is the revision they name; a
    request for any other revision is dropped. Then the remaining contexts
    are prefilled.
- **Scoring with pruning.**
  1. Bound every option by log P(its first token), free from the prefill.
  2. Fully score rime's top 5 movable options and the M = 6 with the best
     bounds. Each multi-token option gets its own sequence on the shared
     context, and all their tokens go in one batched step (of ≤ 20
     outputs).
  3. The rest follow in rime's order (policy).

  M is 6, not the spike's 8. With 8, first page was 0.3 points better (one
  item of 300), but score p50 sat at the 8 ms budget. 4–6-bit K-quants
  were faster at p50, but slower at p95 and less accurate (Q4_K_M agreed
  with the reference on 45 of 50 items, against 49).
- **Measured** (`go test ./internal/ai`, M4, 8-bit, 2026-10-27):
  - Against the spike's fp32 PyTorch scores, 50 items, every option
    scored: the same best option on 49; mean |Δ log p| of probable options
    0.044.
  - 300 spike 简拼 items, standard strength: first word / first page
    31.3% / 59.7% (rime) → 78.7% / 92.7%. The spike had 79.5% / 93.8%.
  - Prefill p50 11.4 ms, p95 16.5 ms; score p50 7.2 ms, p95 9.8 ms. The
    test gates these budgets.
  - End to end (`TestRerankEndToEnd`: rime-ice in our librime, `Remote`,
    the server and the model), 8 sentences then a 简拼 code: rime's #1
    was right 4 times, the reranked #1 6 times, and page 1 had the word
    7 times. The reorder arrived 13–25 ms after the key's Result, the
    context's prefill included. One break: after 我们明天一起去, `tw` put
    天文 (as in 天文馆) ahead of 台湾.
- **Policy (Go, unit-tested, `rerank.Order`).** The fused score is
  `log P + β·(prior − prior₀)`, where the prior is the dictionary log
  weight and ₀ is rime's #1. The spike's `β·log((w + 1) / (w₀ + 1))` is
  the case prior = log(w + 1). A learned `user_phrase`'s weight comes from
  the user dictionary, on another scale. It gets the prior of rime's
  leading dictionary word instead, and drops at most one place. Fixed
  entries keep their places, and the others are ordered around them. The
  policy is "stable":
  1. The best fused option becomes #1 only if it beats rime's #1 by the
     margin.
  2. The other four page-1 slots go to the best-scored remaining options.
  3. Those four keep rime's relative order, as does everything after page 1.

  Against a full sort, the stable policy kept the same accuracy and halved
  how many page-1 slots change: 1.18 against 2.07.
- **Strength settings** (`ai.Strengths`):
  - 保守: a strict margin, for few wrong #1 changes. Provisionally β 0.5,
    margin 3.
  - 标准: the best held-out accuracy. Provisionally β 0.3, margin 0.5;
    the spike's held-out folds picked (0.5, 1) and (0.2, 0).
  - The values are refitted on real menus (phase 4) and shipped with the
    model.

#### Holding a key's result for its order

An order that came after rime's menu was on screen would move candidates
under the user, so none ever does. Instead:

- **Hold.** When a key produces a new export on page 1 with the highlight
  on #1, the engine sends the export to the scorer and parks the key's
  result. The engine thread doesn't wait: it serves other requests, and a
  timer releases the result.
- **Release** happens on the first of:
  - The order arrives for the same export. It is imported, the
    composition refreshed, and the result goes out reordered.
  - The timeout, 16 ms (one 60 Hz frame): the result goes out in rime's
    order.
  - Any newer request for that input context: the result goes out in
    rime's order first, then the request is handled.

  An order that arrives after its result has gone out is dropped. Each
  key gets exactly one result, and a menu never changes once shown, so
  the protocol needs nothing new and no stale view can exist.
- **Never held:**
  - ASK keys, which the app waits for with its 8 ms budget.
  - Notified keys, and keys that commit text.
  - Menus with fewer than two movable candidates, menus already ordered,
    and any key while the scorer isn't connected.
- **The cost** is that the candidates and the inline text appear up to
  16 ms later. End to end (`TestRerankEndToEnd`), held results waited
  p50 9.2 ms, p99 15.4 ms; 147 of 148 got their order, 1 timed out. With
  a 12 ms timeout, more than a fifth timed out. A PASS key typed within
  that time may hit a client barrier (§3.3); held results carry no
  commit, so text order is unaffected.
- **Context first.** Before a request the engine sends the app's context
  if `shixin-ai` doesn't have that revision yet, including a new app's
  empty one. `shixin-ai` drops requests for contexts it doesn't have, so
  without this every key in a new app waited out the timeout.

#### Failure and degradation

- `shixin-ai` not running, still loading, slow or crashed: rime-ice order,
  exactly today's behavior. The status shows why on the settings page.
- No accelerator: `shixin-ai` exits with status 3, and Settings show "no
  GPU". The model missing: the coordinator doesn't start `shixin-ai`, and
  Settings ask for the download.
- A download that doesn't match its size and SHA-256 is discarded; only a
  verified file is renamed into place.
- A slow order costs at most the 16 ms timeout; the result then comes in
  rime's order.
- **A machine too slow for the model** (an old GPU, a VM): after 16
  timeouts in a row, the engine stops holding results and sends them at
  once. It still holds one request in 32 as a probe, and a probe answered
  in time resumes. Settings show "GPU too slow: paused". In the macOS VM
  (paravirtualized GPU) the scorer took p50 64 ms, against 7 ms on an M4,
  with the same accuracy.

#### Privacy

- Off by default. Committed text lives only in the engine's and
  `shixin-ai`'s memory, per app. It's never persisted or logged (debug
  builds excepted, as everywhere).
- `shixin-ai` sees app slot numbers, never app identifiers.
- Nothing is recorded in secure fields or in ASCII mode.
- No network at inference time. The model download is the only network
  use, started from Settings on request: a pinned URL and SHA-256, into
  its own directory, loaded as data and never executed.

#### Settings

- **智能排序 page** (its own sidebar entry, after 输入), with 关闭 / 保守 / 标准 (default
  关闭).
  - **Model:** its size (385 MB), and 下载 with progress and cancel, or
    删除 once downloaded.
  - **While on, status:** 运行中 / 正在启动 / 需要先下载模型 / GPU 太慢，
    已暂停 / 没有可用的 GPU / 未运行. It comes from `shixin-ai`'s state file
    (written by the coordinator in the runtime dir) and the engine's stats.
  - **The note** says that what is written stays in memory and isn't
    recorded in password fields. It credits ERNIE 4.5 (Baidu, Apache-2.0)
    and llama.cpp (MIT).
  - About opens `Contents/Resources/licenses` (llama.cpp, nlohmann-json,
    librime).
- **The download** (`internal/models`) is our own conversion, hosted at
  `huggingface.co/shixin-labs/ERNIE-4.5-0.3B-Base-GGUF` with a model
  card and baidu's LICENSE. It's pinned by size and SHA-256, written to a
  `.partial` file and renamed only once verified.
  - huggingface.co is unreachable from mainland China without a proxy.
    The download follows the environment's proxy, else the one in System
    Settings → Network (`scutil --proxy`; apps opened from Finder have no
    proxy variables). If a host fails, refuses or stalls for 30 s, it
    falls back to `hf-mirror.com`; the checksum makes any host safe.
    Verified on a Mac behind a local proxy, 2026-10-28.
  - Third-party GGUFs of the same model were tried and score markedly
    worse: mradermacher's Q8_0 agreed with the reference on 37 of 50
    items (ours 49), and 简拼 fell to 65.7% / 76.7%. They were made with an
    older converter and tokenize differently.
- There's no per-app switch: the setting is global. Each app still has its
  own context (above).
- **`settings.Settings`:**
  - `rerank` (`off|cautious|standard`), with a default and validation.
  - These don't reach Rime YAML except the filter and processor entries
    in the engine lists. The compiler always lists them; the filter is a
    no-op when off. Golden YAML and a `TestRimeIceOptions` check apply.
- **性能 page** (from the engine's stats, an optional section after
  `KeyTime`):
  - the share of keys that got the model's order, and that timed out
  - how long held results waited: p50, p99
  - Not yet: prefill time, `shixin-ai` memory.

#### Budgets

| Path | Budget |
|---|---|
| Engine thread, extra per key (export check + enqueue) | ≤ 20 µs, 0 allocations |
| Export, hold and applying the order (refresh), N = 50 | p99 ≤ 0.5 ms over today's per-key time (gate) |
| Prefill per commit (64 tokens) | p95 ≤ 20 ms |
| `ScoreRequest` → permutation, M = 6 + 5 | p50 ≤ 8 ms, p95 ≤ 15 ms |
| Hold a key's result for its order | 16 ms |
| `shixin-ai` resident memory | ≤ 600 MB |

#### Tests and gates

- **C++ (`shixin_rerank`):**
  - export of the first N same-span candidates
  - apply and ignore by hash and sequence
  - translator and English candidates never move
  - pass-through when off
  - no allocation per key after warm-up
  - ASan/UBSan
- **Go:**
  - The policy (stable order, margin, guard, `user_phrase`) with fixed
    scores.
  - Engine tests with a fake scorer: the result comes reordered; a
    timeout sends rime's order and drops the late order; a newer key or a
    page flip sends the held result at once; ASK keys and an unready
    scorer are never held; invalid orders fall back to rime's.
  - Per-app context:
    - commits land in the focused app's ring
    - switching away and back resumes it
    - another field in the same app adds a soft break
    - LRU reuse of the 16 rings
    - secure fields record nothing
    - no allocation per focus change
  - `AllocsPerRun == 0` on the key path with the feature on.
- **`shixin-ai`** (`internal/llama`, `internal/ai`; skipped without
  `scripts/build-llama.sh` and the model):
  - The mirrored llama.cpp structs read the library's defaults back
    correctly.
  - Batched shared-prefix scores equal scoring each option alone.
  - Scores agree with the spike's fp32 reference on 50 items: the same
    best option on ≥ 45, mean |Δ log p| ≤ 0.25. The reference is spike
    data in `build/`.
  - Contexts: incremental prefill scores like a fresh one; 4 warm apps,
    LRU; a learned word's prior.
  - Accuracy and the latency gates on 300 items (not under `-short`).
  - `Remote` against the server with a fake model: replayed contexts, an
    answered request, a dropped stale one, a newer engine replacing the
    connection, and newest-wins batching. `Remote` allocates nothing on
    the engine thread.
  - Wire golden vectors (`internal/rerank/testdata`).
  - The end-to-end test above, in a child process: rime-ice menus, the
    held results, and how long they waited.
- **Evaluation harness.** A Go tool replays a text corpus through our real
  librime with rime-ice, in a scratch user dir, and records the real
  menus. It replaces the spike's dictionary approximation, and reports
  first word and first page for full pinyin, 简拼 and 模糊音. CI fails on a
  regression against the recorded numbers.
- **Benchmarks** for the filter and engine paths, alongside the existing
  hot-path benchmarks. `TestReplay` replays rime-ice with a scorer that
  reverses every menu at once, so the engine exports, holds, imports and
  refreshes on every key. Its `abbrev-rerank` variant gates that work:
  the 简拼 corpus's p99 engine time per key with reranking, minus without,
  must stay ≤ 0.5 ms. Most of it is the refresh: applying an order
  re-translates the composition. The menu can't just be reordered in
  place, since emoji, 繁体 and the uniquifier run after the filter.
  Measured on an M4 (2026-10-27):
  - 简拼: p99 224 → 576 µs, p50 40 → 144 µs
  - full pinyin: p50 44 → 88 µs, p99 416 → 640 µs, inside the 2 ms engine
    budget (§3.7)
  The VM checks typing in TextEdit with the feature on: reordered menus,
  and Space committing the #1 shown.

#### Phases

1. **Engine and librime with a fake scorer:**
   - per-app context rings
   - `PASS_NOTIFY` caret moves
   - filter, processor and slots
   - holding a key's result for its order
   - tests and gates
2. **`shixin-ai` on macOS:** llama.cpp on Metal, pruning, policy,
   supervision, and the scorer latency gates. Done 2026-10-27, measured
   above.
3. **Settings, model download, 性能 metrics,** and VM verification. Done
   2026-10-27; the model is published, and `TestPublished` downloads and
   verifies it (`SHIXIN_TEST_NETWORK=1`):
   - the setting, applied live
   - the download and its status
   - the 性能 counters
   - bundling, licenses
   - the slow-machine pause
   - In the VM: the upgrade, the live toggle, `shixin-ai` loading on the
     guest's Metal device, held results, and the pause. Text typed in
     TextEdit came out correct.
4. **Evaluation on real menus, then fine-tuning or distillation** on a
   corpus with a shippable license. A smaller student would keep ERNIE's
   word-level tokenizer.
5. **Windows and Linux** (Vulkan/CUDA) with M3/M4.

## 8. Engine behavior

- **Per-app default mode** (terminals and editors start in English), with the
  last mode remembered per app. Built: on `FocusIn` with a new app, the
  engine applies the app's remembered mode (with "remember each app's mode"
  on) or its rule from the settings; other apps keep the current mode.
- **Secure fields:** ASCII only, no learning, no cloud. This uses macOS
  Secure Event Input (§5.5) and Windows `IS_PASSWORD`. Built in the engine:
  `FocusIn{secure}` switches to ASCII and the next ordinary `FocusIn`
  restores the mode. The macOS front reports the bit and guards locally;
  the client tests verify restoration of both Chinese and English modes
  through a real engine, alongside the engine's per-app mode tests.
- **Mode switches and the policy:** a 中/英 switch updates the policy in the
  same `Result`, so the next key is classified correctly.

## 9. Testing and performance gates

- **Replay corpus:** recorded typing sessions (full pinyin, double pinyin,
  mixed English, punctuation, fast bursts) are replayed against the real
  engine with rime-ice. The tests fail if any budget in §3.7 regresses
  (`SHIXIN_PERF_TOLERANCE` scales the budgets). CI sets it to `off`: on
  shared runners the same commit passed and failed by a few percent, so
  the timing gates run locally on real hardware before merging, while CI
  still fails on mispredicts and allocations.
- **No-Lua gate:** every deployed schema in CI is scanned, and any `lua_*`
  component fails the build. The engine also asserts at startup that no
  `lua` module is registered.
- **Native plugin tests:** each `librime-shixin` component has unit tests
  (C++) and engine-level tests through `simulate_key_sequence`.
- **Allocation gates:** `AllocsPerRun == 0` for the engine key path and the Go
  client core. C++ clients are checked for no heap allocation per key in
  debug builds.
- **Policy correctness:** for every key in the corpus, compare the predicted
  disposition with librime's actual `handled`. Any non-printable mispredict
  fails CI.
- **Chaos tests:**
  - kill the engine mid-typing
  - hot swap mid-burst
  - delay the engine artificially by 50 ms or 1 s

  In every case, assert that clients never block beyond the `ASK` cap and
  that text order is preserved.
- **Cold/idle tests:** after dropping page caches (Linux) or purging (macOS),
  the first key must still meet the budget thanks to warm-up and keep-warm.
- **Unit tests:** engine (`simulate_key_sequence`), settings compiler (golden
  YAML, deployed by librime in CI), IPC codec (golden vectors shared with
  C++), UI views without a window, and skin screenshots.
- **GUI tests in Tart VMs:** macOS front-end tests run in a Tart VM driven
  with `scripts/macos-vm.sh`: its `input` command builds the Go/purego
  helper in `tools/vm-input` as `shixin-vm-input` and runs it in the guest
  for native CGEvents, including
  modifier presses/releases and shifted symbols. Clipboard assertions retry
  within a bounded timeout and compare exact UTF-8 text without logging it.
  `burst` uses native Cmd+N/A/C and verifies all 84 characters from 14
  repetitions in TextEdit. VNC remains for clicks and screenshots, and
  `tart exec` runs guest commands. Accessibility may be needed in the guest;
  no TCC permissions are needed on the host (spike 2).
- **Manual app matrix per platform,** as listed in §5.6, plus the macOS and
  Linux equivalents.
- **Offline menu capture** (`tools/menu-capture`): replays public corpus
  codes through `shixin-engine -capture` and records each code's visible
  menu as JSONL, for evaluating and training 智能排序 (§7.2). Capture is a
  tooling mode outside the coordinator/worker lifecycle: it validates a
  builder's warm-only generation before creating any session, uses its own
  scratch user directory and socket, never promotes to real schemas (user
  dictionaries stay disabled), takes no live user, settings or socket
  paths, and exits on stdin EOF or a signal. It reuses the engine thread
  and binary protocol unchanged. A candidate is marked as covering the
  whole input only when selecting it in a fresh session commits exactly
  it; native candidate types and weights aren't exposed. Date and time
  candidates make captures timestamped observations, not reproducible
  bytes.

## 10. Milestones

| # | Milestone | Done when |
|---|---|---|
| M0 | **Spikes** | Answered with fallbacks chosen (below) |
| M1 | **macOS MVP** ✅ | Engine process + async IMK front + candidate window + Shift 中/英 + menu bar + minimal settings. Our librime build without Lua, plus `librime-shixin` with 以词定字. Budgets met on the replay corpus; no-Lua gates in CI |
| M2 | **Settings and Sogou features** (settings app ✅, per-app mode ✅, native translators ✅, builder + hot swap ✅) | Full settings app, builder + hot swap, skins, custom phrases, per-app mode, .scel import, remaining `librime-shixin` components (dates ✅, numbers ✅, Unicode ✅, calculator ✅, filters), signed pkg |
| M3 | **Windows** | TSF DLL with async edit sessions, Shixin.exe, full test matrix passing with zero stalls |
| M4 | **Linux** | fcitx5 addon, theme export, packages |
| M5 | **Network** | Idle-window sync, dictionary updates, opt-in cloud, typing stats |
| M6 | **智能排序 (macOS first)** | §7.2 phases 1–4: engine/filter/IPC with per-app context and gates, `shixin-ai` on Metal within its latency gates, settings and download, evaluation on real menus meets the spike's 简拼 gains; Windows/Linux with M3/M4 |

**M1 results** (Tart VM, macOS 26, our librime 1.17.0 + librime-shixin):

- Replay corpus (2,595 keys, rime-ice): engine per key p50 48 µs, p99
  416 µs, p99.9 640 µs; no policy mispredicts. Clean under ASan/UBSan.
- In TextEdit at VNC typing speed: `handleEvent` p50 28 µs / p99 160 µs,
  key→candidates p99 3.84 ms, no `ASK` timeouts; 14 × 今天天气真好 typed in
  a burst lands exactly. Terminal, Shift 中/英 with `commit_code`, 以词定字
  and the settings round trip work.
- Engine restart with deployed data: 86–150 ms. First rime-ice deploy:
  6–9 s (English pass-through meanwhile); prebuilt dictionaries in the
  bundle now reduce this to 0.4 s with default settings (§3.5).
- M2 replaces the settings restart with builder + hot swap (§3.5), and
  per-app rules and remembered modes replace the global mode. Fuzzy pinyin
  works in full pinyin and all seven supported double-pinyin schemes; its
  derivations run before double-pinyin spelling conversion. Remaining:
  one input context per front.

**M0 spikes:**

1. **Go → librime via purego.** Type `nihao` through rime-ice data with all
   `lua_*` components patched out, and measure per-key latency and
   allocations. Also measure rime-ice *with* Lua once, to record the cost we
   are avoiding.
   **Done:** p50 49 µs, p99 494 µs, 0 allocations per key; Lua is 3–5×
   slower (`docs/spikes/m0-1-rime-purego.md`).
2. **MyGo + IMK.** A MyGo app registers `IMKInputController` via
   `purego/objc`, returns from `handleEvent:` at once, and applies the text
   asynchronously in TextEdit, Safari, VS Code and Terminal.
   **Done for TextEdit:** works, budgets met
   (`docs/spikes/m0-2-imk.md`). Other apps are still to test.
3. **Candidate window.** A non-focusable MyGo flyout, moved to any screen
   point via `NativeHandle`, appears over another app without activating
   Shixin. Measure show and update latency.
   **Done:** a tray-anchored flyout plus `SetPosition` works without
   `NativeHandle`. Update p99 is 1.5 ms, and the app keeps focus.
4. **User dictionary handoff** between two engine processes (§3.5). ✅
   Works with a warm standby that never opens the user dictionary first
   (28–32 ms). A standby that does open it destroys learned words.
   `docs/spikes/m0-4-handoff.md`.
5. **Async TSF edit sessions** in the hardest apps (Office, Chrome, UWP),
   during M3 preparation.

## 11. Risks

| Risk | Mitigation |
|---|---|
| Async apply misbehaves in some apps (text order, lost compositions) | Ordering barrier, speculation, chaos tests; per-app fallback to `ASK`-everything if an app proves incompatible |
| Policy mispredicts | Conservative `EAT`; mispredicts measured in CI against librime's real answers |
| librime latency spikes | No Lua; warm-up, keep-warm; spikes delay the preedit, never the app |
| Maintaining our own librime builds + C++ plugin on three platforms | Scripted CI builds from day one (Squirrel and Weasel already do this); the plugin uses only librime's stable C++ component API |
| Future rime-ice versions add Lua-dependent features | We own the engine lists; new features are ported natively or skipped deliberately |
| A second librime process opens a live user dictionary (spike 4) | Single-owner engine lock; warm-only standby schemas; supervisor kills and waits before replacing; our librime recovers confirmed corruption only, under LevelDB's exclusive lock |
| Hot-swap handoff over budget (28–32 ms vs 20 ms) | Idle boundary only and hidden by async clients; librime patch for lazy user dictionary attach |
| MyGo is young | Engine, builder and IPC don't import MyGo; MyGo only in UI processes; pin versions; contribute upstream |
| MyGo + IMK in one process fails | Swift front fallback; the engine is unaffected |
| Wayland caret positioning | fcitx5 draws the candidates, using the exported theme |
| Antivirus flags a DLL in every process | Authenticode signing, vendor submission |
| Reranking moves candidates the user was about to pick | It can't: a key's result is held until its order (≤ 16 ms) and sent once, so a menu never changes once shown; stable policy and margins limit how much page 1 changes |
| Model too slow or too large on older hardware | Pruning (M + 5 options); scorer latency gates; late results are dropped, never waited for |
| Reranking is worse on real menus or casual text than in the spike | Evaluation harness on real librime menus gates release; fine-tune or distill with a licensed corpus |

## 12. Decisions

| Topic | Decision |
|---|---|
| Name | 拾新 (Shixin); bundle id `im.shixin.inputmethod.Shixin` |
| License | GPL-3.0-or-later (matches rime-ice; allows Squirrel/Weasel code reuse) |
| Lua | Not used at all, for performance. rime-ice's Lua features are reimplemented natively in `librime-shixin` or dropped (§3.8) |
| 中/英 switch | Shift (Sogou habit) on all platforms by default: tapping Shift alone switches; a short Shift tap while composing commits the typed code as English (`commit_code`). Configurable in settings |
| Context reranking | Opt-in, local, GPU/NPU only. Base model ERNIE-4.5-0.3B (Apache-2.0) scoring log P(candidate \| context); stable order policy; never awaited (§7.2) |

## 13. Open questions

- `ASK` cap default: 8 ms proposed. Lower is safer for apps but passes more
  keys through under load.
- IBus engine for GNOME after M4?
- 智能排序 (§7.2):
  - The corpus for fine-tuning.
  - Reading surrounding text from apps.
