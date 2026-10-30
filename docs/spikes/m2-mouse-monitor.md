# M2 spike: global mouse monitor without permission

Date: 2026-10-27 · Tart VM `shixin-test`, macOS 26.6.2 · Go,
`CGO_ENABLED=0`, purego objc

Code: `spikes/m2-mouse-monitor` (`bundle.sh` builds `MouseMonitor.app`,
an `LSUIElement` agent like an IME; it logs to
`~/Library/Logs/mouse-monitor.log`).

Question: can a background app see mouse presses in other apps without
Accessibility or Input Monitoring permission? Auto spacing's reset on
clicks depends on it (DESIGN §7.1).

## Result: yes, with no prompt

- `AXIsProcessTrusted` was false, and no permission dialog appeared.
- `NSEvent addGlobalMonitorForEventsMatchingMask:handler:` for left,
  right and other mouse-down delivered all three presses made over VNC
  (event types 1, 3 and 25).
- A key-down global monitor in the same process received nothing, as
  expected: key events need Accessibility. Shixin doesn't need them, since
  an input method sees the keys typed into the focused field anyway.

## How to repeat

```
spikes/m2-mouse-monitor/bundle.sh
scripts/macos-vm.sh boot &   # then: scripts/macos-vm.sh vncenv
scripts/macos-vm.sh exec 'cp -R "/Volumes/My Shared Files/shixin/build/spike/m2-mouse-monitor/MouseMonitor.app" /tmp/ && open /tmp/MouseMonitor.app --args -t 60s'
scripts/macos-vm.sh vnc move 500 400 click 1 pause 0.5 move 700 300 click 3 pause 0.5 type abc
scripts/macos-vm.sh exec 'cat ~/Library/Logs/mouse-monitor.log'
```

Re-run on each new macOS release.
