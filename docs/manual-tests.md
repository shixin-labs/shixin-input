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

1. Settings should list the app on every page (Chinese and English), asking
   to switch input methods. Its two cache files must not be deleted while
   it runs. The helper invalidates the shared modification-date marker and
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
3. For an app that has not refreshed, close Settings and quit that app.
   Only its two `com.apple.IntlDataCache.le*` files should be removed;
   unrelated cache files must remain. Repeated repair requests must not
   leave multiple watcher processes running.
4. Reopen Settings: the app must no longer be listed. With Settings kept
   open during repair, it should show “Input-source lists refreshed”.
5. Reopen the app, select Shixin, type and commit Chinese, then switch to
   ABC and back. Check that no new crash report appears.

`SHIXIN_TEST_CACHE_REFRESH=1 ./shixin-front.test -test.run TestCacheRefreshPreservesClipboard`
checks general clipboard change count and multiple data flavors across
repeated marker resets and enabled-state invalidations. It seeds the
clipboard with test data: VM only.
