# Sogou registration and stale input-source caches

Inspected and tested 2026-10-31. Sogou's installer refreshed PowerPoint's
input-source cache and enabled Chinese input without restarting PowerPoint.
A subsequent Shixin experiment isolated a named cache modification-date
pasteboard that can refresh the disk cache without restarting the clipboard
service. A second, in-memory enabled-state cache explained the remaining
Shixin failure; the combined repair below restored typing without quitting
PowerPoint.

## Resolved cause and verified repair

The disk cache is only one layer. In an affected, already-running PowerPoint
process, debugger inspection of `TISCreateInputSourceList` and
`TISGetInputSourceProperty` showed:

| Source | Enabled inside PowerPoint | Enabled in a fresh helper process |
|---|---|---|
| Shixin parent input method | false | true |
| Shixin Hans mode | true | true |

PowerPoint could see the bundle and both source records, but kept a stale
disabled flag for the parent. Its IMK client map contained other input
methods and no Shixin client. Rebuilding the disk cache or restarting
`pboard` did not correct that flag. Disabling and re-enabling Shixin did,
and the original front then received keys without restarting PowerPoint.

The public `kTISNotifyEnabledKeyboardInputSourcesChanged` notification did
not invalidate the stale flag. The installed HIToolbox implementation uses
a separate distributed notification,
`AppleEnabledInputSourcesChangedNotification`, to invalidate enabled state.
Posting that notification changed PowerPoint's parent flag to true without
changing the enabled-source preferences, asking for consent again, or
restarting the app. Native switching then showed Shixin's own candidate
panel, logged its five `nihao` key-down events, and passed an exact `你好`
clipboard assertion. PowerPoint remained PID 8661 throughout this test.

The production helper now clears the file-cache modification-date marker
and posts that enabled-state invalidation. It was tested from another
missing-Shixin baseline with PowerPoint PID 8823 and the original front:
`-repair` ran once, native switching selected Shixin, its own panel and key
events were observed, and exact `你好` passed with the same PowerPoint PID.
No debugger was attached in this production-helper run, and neither
`pboard` nor PowerPoint was restarted. Explicit repair also invalidates
enabled state when the disk caches have already been refreshed.

The combined repair passed the opt-in clipboard test with text, HTML,
file-URL and binary flavors unchanged. The marker and notification names
are undocumented, so targeted closed-app cleanup remains a fallback.

**Test-method correction:** neither a successful external TIS selection
nor Chinese text alone proves Shixin handled a key. PowerPoint can retain
its previous input session or fall back to Apple Pinyin. One apparent
success was rejected after its screenshot and current-source check showed
Apple Pinyin and no Shixin key events. Select text before changing sources,
switch through the native menu/shortcut, and verify the candidate panel,
current source and front log as well as the exact commit. The earlier
negative typing tests below therefore did not isolate every proposed
metadata or startup-timing explanation.

Sogou also worked with the minimal TIS helper and narrow marker refresh,
including after ad hoc re-signing. This establishes that its complete
installer and developer signature are not required in these runs; it does
not establish that Sogou itself posts the internal invalidation above.
The proven Shixin defect was incomplete cache recovery, not a general
requirement for applications to quit.

## Evidence

The inspected release is Sogou 6.25.3.12004, already installed in the macOS
26.6.2 test VM from the [official distribution](../sogou-comparison.md#reproducing).
The initial investigation used the installer archive, Info.plist,
exported/imported symbols, and ARM64 disassembly. The subsequent PowerPoint
experiment below ran the official installer in the VM. No proprietary
implementation was copied into Shixin.

Artifact SHA-256 values:

- Embedded `SogouInput.zip`:
  `35f0300a9af20379369d854be9078b98d9448adc4e5e50d3f0b4931a57ef9363`.
- Installed `SogouHelper`:
  `c50ed0b5c31354f6c5586677fa2ee0a5040ef2cf8e5971a769b7c6162c3fc250`.

The installer scripts were also read directly from the embedded ZIP, rather
than relying solely on an extracted working copy.

## What Sogou does

| Observed behavior | Evidence |
|---|---|
| Installs into the system-wide input-method directory | `installOS13.sh` and `install.sh` use `/Library/Input Methods/SogouInput.app`. Shixin installs per user. |
| Registers, enables, and selects input sources with TIS | `SogouHelper` imports and calls `TISRegisterInputSource`, `TISEnableInputSource`, and `TISSelectInputSource`. Shixin uses these APIs too. |
| Restarts the system menu UI after installation | The installer scripts end with a `SystemUIServer` kill command. |
| Restarts the input-menu agent and pasteboard service in selection/recovery paths | `SGTools.selectSogouAndShowIconWithAuth` and `SGTools.selectSogouInputMethodNow` contain calls to `system` for `killall -9 TextInputMenuAgent` and `killall -9 pboard`. These are actual calls in disassembly, not just unused strings. |
| Has a version-specific pasteboard restart | `SGTools.enableSogouInputMethodForOS12` checks for macOS 13 or later, then restarts `pboard` before enabling Sogou. |
| Tries to preserve clipboard text across that restart | The helper's `getCurrentPasteboardString` and `recoverPasteboardString:` read and restore the string type. This is not a general backup of images, files, and other clipboard formats. |
| Waits for system services and input-source events | The helper observes `kTISNotifyEnabledKeyboardInputSourcesChanged`; its authorized selection path starts a timer and invokes `SogouInstallerSelector` after the relevant processes are present. |

The installer and service binaries also contain invocations of the helper's
`enableOS12` and `selectNow` commands. Static inspection identifies available
paths, not which path every installation takes on every macOS release.

No `IntlDataCache` deletion was found in the inspected installer scripts or
literal-string searches of the installer, helper, service, and task manager.
That negative search is not proof that no other code can invalidate a cache.
Static inspection alone did not establish a refresh mechanism for arbitrary
already-running clients. The subsequent experiment demonstrated that the
complete installer flow can refresh PowerPoint without restarting it.

## Why the reported WeCom comparison is not equivalent

The original WeCom `.kbdx` cache from the crash incident was preserved before
repair. Read-only inspection found:

- Sogou's input-method identifier was already present.
- Shixin's input-method identifier was absent.
- The cache predated Shixin's installation.

WeCom therefore did not need to discover Sogou from that stale cache. Its
working with Sogou does not demonstrate that Sogou could repair the same
missing-source condition without restarting WeCom.

The two crash reports show `CFRelease(NULL)` in Apple's
`TUINSCursorUIController._selectCurrentInputSource`. Rebuilding WeCom's two
input-source cache files while it was closed resolved the reported crash.
This supports the targeted recovery, but does not prove a universal
requirement to quit apps during input-method installation.

## PowerPoint experiment

Environment: macOS 26.6.2 (25G83), Microsoft PowerPoint 16.114.3, Sogou
6.25.3. A VM backup was made first. The existing Sogou source was disabled,
its launch agents stopped, and its bundle unregistered and moved aside.
PowerPoint was closed during preparation and its two old input-source cache
files were moved aside. This was not a pristine OS or Sogou preferences
profile; the required missing-source baseline was checked explicitly.

1. Open a local test `.pptx`, add a title slide and enter an ASCII title.
   Confirm a fresh TIS query returns no Sogou sources and PowerPoint's newly
   created `.kbdx` does not contain `com.sogou.inputmethod`.
2. Leave PowerPoint running (PID 4088, started 11:21:10 UTC).
3. Run the official graphical Sogou installer and authorize installation.
   At the post-copy activation guide, PowerPoint's cache still has its
   original hash and the process is unchanged.
4. Complete macOS's input-source consent, keep the default skin, and skip
   account login. Do not quit PowerPoint or remove its cache files.
5. Return to PowerPoint, type `nihao`, observe Sogou's candidate list, and
   commit with Space. Select/copy the title and assert exact clipboard text
   `你好` using the VM input helper. Save the presentation.

Observed results:

| Observation | Before | After |
|---|---|---|
| PowerPoint PID/start time | 4088 / 11:21:10 UTC | Same |
| Sogou in PowerPoint `.kbdx` | Absent | Present |
| `.kbdx` SHA-256 | `ead21cdab2aeedca08510b316e0fe3c583dcda82aaffffe412feb620486eee54` | `69e72696fbfd9a527da4ecbdbb569f3b3f7caf003ec8cd6f98447d0ba439df56` |
| `pboard` PID | 347 | 4302 |
| `TextInputMenuAgent` PID | 719 | 4304 |
| Selected source during the typing test | — | `com.sogou.inputmethod.sogou.pinyin` |
| Committed title | ASCII baseline | `你好`, exact clipboard assertion passed |

No new PowerPoint crash report appeared. Screenshots captured the open deck,
installer, composition candidates, and committed Chinese text.

**Conclusion:** quitting the client app is not an inherent requirement.
Sogou's complete install/enable flow refreshed PowerPoint while it remained
open. The service restarts and cache refresh occurred in the same flow;
this test does not isolate whether `pboard`, the input-menu agent, TIS
enablement, or a combination caused the refresh. It also does not establish
the same result for WeCom on macOS 26.7.1.

## Implications for Shixin

Shixin already has a narrower input-menu recovery: clear the named
`AppleInputSourcesInUIOrderPasteboard` and restart `TextInputMenuAgent` only
when the menu list lacks Shixin. Sogou's whole-pasteboard-service restart
may also affect this menu list, but equivalence of all effects is untested.
The PowerPoint experiment makes service restart a concrete hypothesis,
but does not prove that killing `pboard` alone fixes WeCom's separate disk or
in-memory cache. Do not adopt that broad restart on this evidence alone.

Keep the app-restart notice as a fallback for detected stale app caches,
not a claim that all apps must restart. The system-wide install location,
bundle metadata, and ordering of registration and menu refresh are further
differences worth testing, not established causes.

The isolation experiment below supplies a narrower cache-refresh operation.
A reinstall of an already-known source is not an equivalent test.

## Isolating the refresh with Shixin

The Shixin bundle was temporarily unregistered and moved aside, with its
engine data preserved. PowerPoint was opened with a newly built cache
verified to lack Shixin. The bundle was restored and registered, and the
macOS consent dialog completed. No app cache files were removed while
PowerPoint was running.

In the first run, PowerPoint remained PID 4825 throughout:

| Step | PowerPoint's `.kbdx` | Slide typing |
|---|---|---|
| Register and enable Shixin, select it, type | Still lacks Shixin | Literal ASCII |
| Clear the menu-list pasteboard and restart only `TextInputMenuAgent`, select and type again | Still lacks Shixin | Literal ASCII |
| Restart only `pboard`, select and type again | Rebuilt with Shixin | Still literal ASCII |

The first two states shared SHA-256
`bde4eced0ec2aa9427fec09b13c7bd9f1009580c19aa8367fe4c1b4954694d06`;
the rebuilt cache had
`d69c7cf7e8fad4f09562c5d91459d3d7539080cea43f67c2e11e708a82c6662e`.
An immediate check before text-input activity can still see the old cache:
the invalidation is not an eager rewrite of every client's file.

Inspection of the installed CarbonCore framework's string section found
`AppleIntlFileCacheModificationDatePasteboard` next to the international
file-cache implementation. A second missing-Shixin baseline was prepared
and the test repeated with **only this named pasteboard cleared** through
the Carbon Pasteboard Manager. PowerPoint's cache rebuilt with Shixin;
PowerPoint stayed PID 5033 and `pboard` stayed PID 4899. Neither process was
restarted, and the app's cache files were not manually removed.

The production helper was then tested against another stale PowerPoint
cache. `-cache-status` initially listed PowerPoint; `-repair` launched the
watcher and logged the marker invalidation. After text activity, status was
empty and the cache included Shixin. PowerPoint remained PID 5107 and
`pboard` remained PID 4899. The helper exited after the pending list emptied.

`TestCacheRefreshPreservesClipboard`, run explicitly in the VM, seeded the
general clipboard with plain text, HTML, a file URL, and arbitrary binary
data. Two marker resets preserved both its change count and all bytes.
This test deliberately changes the VM's clipboard to create its fixtures;
the repair operation itself does not read or write the general clipboard.

Shixin now tries the marker invalidation once per watcher run when it finds
stale caches belonging to running apps. Settings asks users to switch input
methods again; quitting is the fallback. The marker name is undocumented,
so failures remain logged and targeted cache removal after app exit remains
available. No `pboard` restart is added to the product.

**Limit observed at this stage (resolved above):** a fresh disk cache is not the whole input path.
Shixin still produced literal ASCII in the open PowerPoint slide after
these refreshes, although it composed Chinese in TextEdit. Restarting the
launcher or Shixin front, reselecting, and refocusing did not resolve the
PowerPoint result in this run. No crash was observed. This needs a separate
input-event/IMK compatibility investigation; the cache-refresh change must
not be reported as complete PowerPoint typing support.

## Connection and service-order follow-up

Reopening just the presentation did not restore Shixin input. A diagnostic
front logged neither activation nor key callbacks in the affected PowerPoint
process. After restarting PowerPoint, restoring the original front, and
reselecting Shixin, its existing `handleEvent:client:` implementation received
keys and an exact clipboard assertion confirmed `你好`. Thus this run does
not establish a general incompatibility with PowerPoint's key-event API.
The diagnostic front was removed; no alternate key handler was added.

Sogou's disassembly establishes ordering as well as the service names:

- `enableSogouInputMethodForOS12` deselects Sogou, and on macOS 13 or later
  saves clipboard text, kills `pboard`, restores text, then enables Sogou.
- `selectSogouInputMethodNow` saves clipboard text, kills `pboard` and
  `TextInputMenuAgent`, restores text, then enumerates and selects sources.
- The authorized-selection path enables the source, observes source changes,
  restarts both services, and uses a timer before invoking its selector.

A new missing-Shixin baseline was created with PowerPoint PID 5482. Both
services were restarted before enablement, macOS consent was completed,
and both were restarted again before selecting Shixin. PowerPoint remained
PID 5482, but the Chinese clipboard assertion still failed and the front
received no activation/key callbacks. This tested approximation of Sogou's
ordering is therefore insufficient for Shixin; it does not reproduce every
detail of Sogou's authorized selection path.

A system-directory comparison was inconclusive: Shixin interpreted launch
from that location as an installation request and copied itself back to its
per-user destination. The temporary system copy was moved out and the
per-user registration restored. Installation location and the differing
input-mode metadata remain unisolated variables.

## Additional isolation tests

The selector executable was inspected separately. Its selection method
enumerates TIS sources, checks availability, calls `TISSelectInputSource`,
and on success posts Sogou's `keySGSelectSogouInputMethodSucceed`
notification. No additional cache-repair operation appears in that method.

The system-directory experiment was repeated with an explicit argument to
bypass Shixin's self-installer. The running front's executable path was
verified under `/Library/Input Methods`; PowerPoint remained PID 5482.
Registration, consent, cache-marker invalidation and the two service
restarts still did not restore Chinese typing. This rules out relocation
alone as recovery for that already-affected process, not every possible
first-install sequence at that location.

A fresh missing-source baseline was then prepared (PowerPoint PID 6280,
the same baseline cache hash recorded above). Before registration, the
test bundle used Sogou's `smSimpChinese` script designation and top-level
`tsInputMethodCharacterRepertoireKey` values `Latn`, `Hans`, and `Hant`.
After consent, both Shixin sources were verified enabled and selection
returned success. The exact Chinese typing assertion still failed.
Restarting `SystemUIServer` in addition to `pboard` and `TextInputMenuAgent`
also failed to restore typing. Other metadata differences remain untested.

An earlier metadata attempt had incomplete parent-source enablement and
returned `OSStatus -50` on selection; that attempt is inconclusive and is
not counted as a typing test. The later baseline explicitly checked both
sources before testing.

All experimental metadata and installation-location changes were reverted.
After a verified PowerPoint exit and relaunch, the original per-user Shixin
again passed the exact `你好` clipboard assertion. These experiments narrow
the failure to live source discovery/connection but do not yet establish a
safe operation that reconnects the affected PowerPoint process.

## Minimal registration comparison

Sogou was removed again with its services stopped. PowerPoint PID 7117
started with the previously recorded missing-Sogou cache hash. Restoring
the signed bundle, registering/enabling it through the same diagnostic TIS
helper used for Shixin, and clearing only the modification-date marker
allowed exact `你好` input without restarting PowerPoint. The full Sogou
installer and `pboard`/menu-agent restarts were not used in this run.
Thus the full installer is not necessary for the observed Sogou success.

The experiment was repeated with a backed-up Sogou bundle re-signed ad hoc
in the VM. After input-source consent, both sources were enabled, selection
succeeded, and PowerPoint PID 7214 passed the same exact-text assertion.
The original signed bundle was restored afterward. Developer signing alone
does not explain Shixin's failure in these tests.

Shixin still failed against another missing-source baseline (PowerPoint
PID 7331) with a broader Sogou-style metadata variant: background-only,
server delegate class, root repertoire, Simplified Chinese script, shortcut
metadata, and removal of Shixin's explicit root source/language keys and
mode repertoire. Original metadata was restored afterward. This is a
negative test of that combination, not proof that all metadata is irrelevant.

A fresh-identifier Shixin probe was also staged, but its parent source was
not enabled when selection was attempted (`OSStatus -50`). That probe is
inconclusive and was disabled and removed from the input-method directory.
Investigation was interrupted to repair a live Mac's recurring crashes.
