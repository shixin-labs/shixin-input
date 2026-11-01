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
Registration and unregistration must happen only in the VM. See
[Windows VM setup](windows.md#windows-test-vm-on-macos) for host control and
[Windows status](windows.md#status-2026-11-01) for verified coverage.

1. From an ordinary shell, register with `Shixin.exe -register`. Verify UAC
   appears; cancelling must return failure without changing any keyboard or
   COM/TIP registration. Approve a second run and verify machine registration
   plus enablement for the original user. Start `Shixin.exe`, and select
   拾新 in Windows's language/input settings. Confirm registration appears
   for the intended architecture; test native ARM64 and x64 emulated apps
   separately before making architecture support claims.
2. In Notepad, type `nihao` and Space; copy and assert exactly `你好`.
   Exercise digits, candidate clicks, arrows, Backspace, Escape, Enter,
   paging, Shift tap, Shift shortcuts and Ctrl shortcuts during composition.
   A key tested by TSF and then delivered must be sent once, including
   Explorer rename where the delivered repeat count may be zero. With 自然码,
   type `nihk` once, Space and Enter; assert the exact filename `你好.txt`.
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
   Put a password field below a textarea, compose in the textarea, then click
   below the visible card into the password field. The click must reach it,
   hide the menu, and leave subsequent literal keys out of the engine.
8. Repeat in Word/Office, Chrome/Electron, Explorer and Start search.
   Confirm exact `你好` in Start search, with the DLL loaded in the
   low-integrity SearchHost. Also run the capability-free transport test
   (`go test -v ./internal/ipc -run TestInputPipeAppContainer`) on Windows.
   Close test apps and, from an ordinary shell, unregister with
   `Shixin.exe -unregister`; confirm no unrelated input sources or user
   dictionaries changed. Also cancel an uninstall and verify the previous
   keyboard list is unchanged. Record the integrity level and named TSF API
   when registration fails. A failed uninstall must
   preserve the machine COM class and user keyboard list; a successful uninstall
   must remove the machine/user profile, COM class, service
   and categories without a subsequent spurious category-removal failure.
   Repeat with an older HKCU COM entry and an already-disabled profile.
   In the disposable VM, deny deletion of only Shixin's machine COM key, then
   uninstall: TIP removal plus COM cleanup failure must leave the keyboard
   disabled. Restore that key's permissions and retry; cleanup must succeed
   without re-enabling the removed profile.

### Windows app matrix

After registration, run `Shixin.exe -added` in the invoking user's account:
it must exit 0, leave the input source unchanged and start no host/engine.
The 开始使用 guide must show enablement. Check the user's **Startup Apps**
entry and its quoted executable command with `-startup`; execute that
command in the test session with the scratch settings environment, then
verify normal typing. After unregistration, `-added` must exit 1,
`-startup` must exit quietly, and this copy's startup entry must be gone.
UAC cancellation must preserve both the keyboard and startup entry.

While composing, inspect the input underline and active segment. Committing,
cancelling and switching away must leave no composition underline on the
final text. Tap Shift while idle: the confirmed 中/英 hint must appear beside
the caret for about 900 ms without taking focus. Candidates take priority;
switching fields/apps, moving the caret and entering secure fields dismiss
the hint. A repeated result must not restart its timer. Test per-app default
modes and no-spacing rules using a desktop app chosen from the app picker,
including an app without an explicit AppUserModelID.

With **中英文、数字之间自动加空格** enabled, verify `你好 iPhone 你好`
by committing `nihao`, tapping Shift, typing `iPhone`, tapping Shift and
committing `nihao` again. In Chinese mode, committing `nihao`, typing `3`
and committing `nihao` must produce `你好 3 你好`. In English mode after
`你好 a`, two Backspaces followed by `b` must produce `你好b`: deleting the
inserted space suppresses it for the next character. Repeat after clicking
the caret, replacing a selection, pasting and switching fields. Disable the
setting live and verify the same boundaries have no inserted spaces; enable
it again and test an app in **不自动加空格的应用**. Password/PIN input must
stay literal. Restore the original settings after the checks. A text store
that defers/refuses the optional passed-key edit may omit the space; it must
never delay, duplicate or insert the key into a later selection.

For each run record the source commit, Windows build, guest architecture,
application version and architecture, DPI, selected keyboard, and settings.
Preserve the [default Windows logs](windows.md#collecting-development-logs)
for each run. Record each check as **pass**, **fail**, **blocked**, or
**not run**, with enough diagnostics to reproduce failures.
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

Leave an uncommitted composition, press Ctrl+L in the browser and return
with Escape; confirm the original field finalizes text exactly once.
Repeat by switching apps and returning; check that text is finalized in
the original field only and no stale panel follows the new focus. Repeat with selected text already present, then with an ordinary field
followed by a password/PIN field. Screenshot candidate placement at the
screen edges and each tested DPI; list mixed monitors as not run on a
single-display VM. Confirm clicking the panel preserves keyboard focus and
clicking a field below the visible panel moves focus to that field.

Open Settings from the tray, inspect its Windows 开始使用 guide, and change
layout, punctuation and appearance. Confirm the saved setting, live engine
behavior and host appearance independently. Stop and restart the engine,
then change a setting requiring a generation rebuild during a typing burst;
record literal fallback keys and any missing or duplicated commits.

Suspend the active worker (not the coordinator) while an ordinary field
has focus. Type `nihao ` and confirm literal text appears before resuming
the worker. Resume it and verify the text stays unchanged, then clear the
field and verify a fresh `nihao` + Space commits `你好`. Repeat with an
existing inline composition. Change focus or select other text while a
recovery edit is delayed; late replies must never replace that selection.

At 2048 MiB, cycle through full pinyin and several double-pinyin layouts
while idle. Record build stage on failure and peak builder private commit;
the old engine must remain usable if a build fails. Change the Windows time
zone with a composition/session already initialized, request the native
time candidate again, and compare with the OS clock without restarting the
engine. Restore the original zone and verify once more. Trigger codes are
schema-dependent (`sj` or `time`).

Set ten candidates per page and font size 28 on a small screen. In both
horizontal and vertical layouts, highlight the last candidate, scroll to
earlier ones and click one. The highlight must be visible after keyboard
movement; all candidates must remain reachable and selectable. Click a field
outside the visible card to verify that the transparent canvas passes clicks.

Before and after typing, capture process IDs, CPU time and working sets for
`Shixin`, `shixin-engine`, `shixin-settings` and the test application hosting
the TSF DLL. Compare CPU-time deltas over a stated idle interval and during
a stated burst; a single cumulative CPU reading is not an idle-usage result.
Preserve the native build/check log and note every skipped dependency,
application or security scenario. Put reproducible product failures in
[Known issues](KNOWN-ISSUES.md#windows-validation-and-release-support).
