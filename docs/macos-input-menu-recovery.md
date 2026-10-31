# Recovering Shixin missing from the macOS input menu

**Symptom:** Shixin is added in System Settings → Keyboard → Input Sources,
but the input menu doesn't list it (even with TextEdit focused), or
switching to it in one app keeps falling back to ABC or another input
method.

**First, run `Shixin -repair`** (`~/Library/Input Methods/Shixin.app/Contents/MacOS/Shixin -repair`).
Shixin's settings app runs it when it sees Shixin added, and the
installer after installing or upgrading. It applies both recoveries below,
only once Shixin is added and only where a cache lacks it, and logs each
change to `~/Library/Logs/Shixin/shixin.log` (DESIGN §5.5). The manual
procedure below is for diagnosing what it didn't fix.

These causes were found on a Mac with macOS 26.7.1 (2026-10-28), where
recovery was needed without logging out.

Settings lists running apps whose input-source caches lack Shixin. Shixin
invalidates Carbon's cache modification-date marker and HIToolbox's cached
enabled-source state, allowing those apps to refresh on their next text-input
activity. Switch to another input
method and back to Shixin. This leaves the general clipboard and its service
alone. If input still fails, save work and quit the affected app; the
background helper then removes only its two stale input-source cache files.
It continues after Settings closes. Reopen Settings to check completion;
`Shixin -cache-status` prints pending app names as JSON.

A stale cache can also cause an app to crash during input-source switching:
WeCom on macOS 26.7.1 hit `CFRelease(NULL)` in
`TUINSCursorUIController._selectCurrentInputSource`. Its old cache lacked
Shixin; rebuilding only the two files below resolved the reported crash.
The preserved cache already contained Sogou, so Sogou working in that app
was not a test of the same missing-source condition. See the
[Sogou registration investigation](spikes/m2-sogou-registration.md) for
its installer recovery steps and the limits of that comparison. A later
controlled missing-source test showed Sogou refreshing PowerPoint's cache
without quitting PowerPoint. Isolating the shared modification-date marker
then reproduced the disk-cache refresh without restarting `pboard`; Shixin
now tries this first. PowerPoint also retained a disabled parent-source flag
in memory after its disk cache rebuilt. Invalidating that enabled-source
state restored Shixin's own candidates and Chinese commits without quitting
PowerPoint. Cache freshness alone is not proof of successful typing.

## The input menu's own list

The input menu keeps its list in a separate named pasteboard,
`AppleInputSourcesInUIOrderPasteboard` (flavor
`AppleInputSourcesInUIOrderFlavor`). It is neither the clipboard nor an
`IntlDataCache` file, and macOS doesn't always rebuild it when an input
method is added.

**Diagnose:** compare what TIS knows with what the menu shows.

- `TISCreateInputSourceList` includes Shixin's `…Shixin.Hans` mode as
  enabled and selectable.
- The menu's list doesn't. `Shixin -repair` reads it with the Carbon
  Pasteboard Manager (NSPasteboard sees the flavor but returns no data).
  For diagnosis only, the private `_TSMCopySelectableInputSourcesInUIOrder()`
  in HIToolbox returns the same list.

**Recover:**

1. Note the enabled sources and the menu's list first. Don't treat every
   switching problem as this one.
2. In the affected user's GUI session, clear **only that named
   pasteboard**: `[[NSPasteboard pasteboardWithName:@"AppleInputSourcesInUIOrderPasteboard"] clearContents]`.
   Over SSH, run the helper that does this with `open -g -n -W` so it runs
   in the user's session.
3. Restart the menu agent; launchd starts it again and rebuilds the list:

   ```sh
   pkill -u "$(id -u)" -x TextInputMenuAgent
   ```

4. Verify from a fresh process that the menu's list now includes Shixin,
   then select it and type in the affected app. A zero status from
   `TISEnableInputSource` or `TISSelectInputSource` proves nothing here.

The old list can't be backed up as a pasteboard item (`dataForType:`
returns no data); record its input source IDs instead.

## An app's own input-source cache

Apps can keep their own copy of the input source list:

```text
$(getconf DARWIN_USER_CACHE_DIR)/<app bundle id>/com.apple.IntlDataCache.le
$(getconf DARWIN_USER_CACHE_DIR)/<app bundle id>/com.apple.IntlDataCache.le.kbdx
```

In the incident, Slack (`com.tinyspeck.slackmacgap`) had a three-week-old
copy without Shixin, while the shared cache and TIS had it. Restarting
Slack or reinstalling Shixin didn't refresh it, and switching to Shixin in
Slack kept returning to ABC.

1. Quit the app and make sure its processes have exited.
2. Move only those two files out of its cache directory, keeping a backup.
   Don't clear the app's whole cache or change the enabled sources.
3. Reopen the app. macOS recreates the cache; check that the new `.kbdx`
   contains `im.shixin.inputmethod.Shixin`.
4. If Shixin is still missing from the menu, it's the separate menu list
   above; repeating this won't fix it.
5. In the app, select Shixin, type pinyin and commit a candidate. Switch
   to another app and back.

## What didn't help

Reinstalling Shixin, restarting only the menu agent, turning Shixin off and
on, and clearing the shared or app-specific `IntlDataCache.le*` files all
left Shixin out of the menu while the menu's own list lacked it. No logout,
reboot or user-dictionary change was needed once that list was cleared.

`imklaunchagent` logging `Refusing connection name` errors isn't evidence
against Shixin: some come from Apple's PressAndHold. Check the bundle
identity in a log line before attributing it.
