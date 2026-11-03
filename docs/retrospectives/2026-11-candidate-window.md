# Retrospective: the missing candidate window, doubled commits and the mode bubble (0.2.2)

November 2026. Status: fixed in development builds and confirmed by the
reporter.

## Summary

After 0.2.2, a user reported that the candidate window sometimes didn't
appear: in 企业微信 (WeCom) every time, in Slack, ChatGPT and other apps
sometimes, in others never. The investigation found four problems:

1. **The candidate window belonged to the wrong display.** The window is a
   transparent 1400×560-point canvas with the panel at its top-left corner.
   On a Mac with a second display to the right, a caret low and to the
   right on the built-in display put most of the canvas over the other
   display (or below the bottom of the screen, on no display at all). With
   "Displays have separate Spaces", macOS shows a window spanning displays
   only on the one holding most of it, so the panel, on the built-in
   display, never showed. 0.2.2's "place the window before showing it"
   made this frequent: before it, the window was ordered in where the last
   composition left it and moved afterwards, and macOS doesn't reassign a
   window that a program merely moves. The fix sizes the canvas to end at
   the caret screen's edges.
2. **Commits were sometimes typed twice** (every 0.2.2 user). Inserting text
   is a synchronous call into the app; macOS can run Shixin's queued `Drain`
   inside it, and the nested `Drain` applied the results still being
   applied. It needed a slow app call, which debug logging made certain.
3. **The 中/英 mode bubble flashed or didn't show after switching apps.**
   macOS deactivates and reactivates an input method within milliseconds
   while switching apps, and Slack deactivates it right after activating it,
   reactivating its field a second later; each deactivation dismissed the
   bubble. A window just shown also reports itself occluded, and its 900 ms
   ran while painting lagged.
4. **The app's caret rectangle was trusted blindly.** NaN, zero or
   off-screen rectangles could place the window off every screen. Not the
   cause of the report, but a real gap found on the way.

## Impact

- (1) On affected machines, whole compositions without candidates, in apps
  whose text field sits low and to the right; typing still worked.
- (2) Any app, intermittently: a phrase typed twice, more likely on a busy
  machine. In a test VM with debug logging, 0.2.2 typed 16 to 24 phrases
  for 14.
- (3) A bubble shown for 2–100 ms, or not at all, after app switches.

## Timeline

1. **Report:** "the candidate box is sometimes missing". The logs said
   nothing about the window: placement was never logged.
2. **Caret theory (wrong):** Qt apps answer the caret query before laying
   out the marked text. A simulator reproduced it and a fix went to the
   reporter's machine. Not fixed: the debug log showed valid carets.
3. **Doubled commits found** while testing in the VM with debug logging:
   0.2.2 reproduced them, a Go test with a nested `Drain` overflowed the
   stack, and a re-entrancy guard fixed them.
4. **Layer-by-layer logging** (caret, placement, visibility, frames): the
   window was placed at the caret, shown, reported visible, and built
   frames on every key. A window-server recording showed it on screen,
   above the app, opaque.
5. **Occlusion theory (wrong):** a build that logged the window's
   occlusion state made the box appear in WeCom. A deliberate occlusion
   query was shipped to the reporter as a fix. The box still went missing
   in Slack. In hindsight the WeCom result was most likely the caret, or
   the window, being elsewhere that time.
6. **Mode bubble:** logs showed the activation/deactivation bursts and
   Slack's deactivation; a grace period and keeping a shown bubble until it
   expires fixed it, confirmed by the reporter.
7. **Renderer theories (wrong, and harmful):** a MyGo trace build showed
   that failing compositions started within two seconds of the last one,
   with a partial first frame, while frames after MyGo's two-second trim
   were full and synchronized. Two MyGo patches forcing full, then
   full-and-synchronized, frames on every show made the box **never**
   appear on the reporter's machine. The reporter was put back on a working
   build each time.
8. **Hardware isolation:** a standalone MyGo flyout harness on a
   single-display Mac drew correctly in every round, including quick
   re-shows and near-bottom placement. Software rendering on the reporter's
   machine still failed: not the GPU.
9. **Root cause:** the reporter's second display. The failing window
   positions put most of the canvas over it (for one failure, 96,900
   square points on the built-in display against 102,400 on the other),
   the working ones didn't. Sizing the canvas to the caret's screen fixed
   it, confirmed by the reporter, with compositions further right than
   before.

## What went wrong

- **Fixes went to the reporter's machine on theories.** Three times
  (caret, occlusion, renderer), and the renderer patches made things
  worse. Each looked supported by evidence that turned out to be
  coincidence or correlation.
- **Nobody asked about the setup.** One question ("how many displays, and
  where is the second?") would have pointed at the answer on day one. The
  VM and the development Mac have one display each, so nothing local could
  reproduce it.
- **"Shown and visible" was trusted as "seen".** AppKit and the window
  server reported the window on screen throughout; neither knows which
  display a spanning window is shown on.
- **The logs couldn't answer the question.** Early on nothing recorded the
  window's placement, and later nothing recorded its size relative to the
  screens.
- **Test tooling misled:** a shell expansion (`$want今天` reads as a
  variable named `want今天`) made correct output look wrong; a window
  recorder reported a stale frontmost app because it never ran an event
  loop; a harness first drew identical content on every update.
- **Debug mode changed behavior:** its extra synchronous calls into the
  app made doubled commits certain.

## What went well

- Reproducing the doubled commit in a plain Go test once its trigger was
  understood, and verifying every fix by disabling it and watching its
  test fail.
- The app simulator (`internal/appsim`): it caught a real mistake in its
  own model and in a fix, and covers apps the VM can't run.
- Restoring the reporter's working build within minutes of each
  regression.
- Privacy: every log added records positions, sizes and states, never
  text.

## Changes

- `cmd/shixin`: the candidate canvas is sized to end at the caret screen's
  right and bottom edges (`candidateCanvas`; `TestCandidateCanvas` uses
  the failing geometry). The window update re-checks after its caret query
  and sets its shown flag before native calls.
- `internal/client`: `Drain` ignores a nested call and has the outer one
  look again (`TestNestedDrain`, re-entrant `TestTypingInSimulatedApps`).
- `cmd/shixin`: the mode bubble survives deactivation bursts and Slack's
  deactivation, shows the mode on every switch to another app, and starts
  its duration once visible (`TestModeIndicator*`, `TestPaintWait`).
- `cmd/shixin`: the caret tracker validates the app's caret and asks again
  (`TestUsableCaret`, `TestCaretInSimulatedApps`).
- `internal/appsim`: simulated app text fields for apps the VM can't run.
- Debug mode logs caret answers, placement, hides with their reason, drawn
  frames, nested updates and the app's callbacks during Shixin's calls.
- The occlusion query added on a wrong theory was removed.

## Action items

- Ask reporters about their setup (displays and their arrangement, scaling,
  the app and where in it they type) before forming a theory.
- Test the front with two displays: add a second virtual display to the
  test VM, and check placement near each display's edges.
- Log, by default and as numbers, which screen a shown window belongs to
  and how much of it lies on that screen.
- Prefer measurements on the affected machine to fixes; when a fix must be
  tried there, keep a working build ready to restore, at a version number
  above the experiment's (the installer refuses downgrades).
- Add a simulator profile whenever an app-specific bug is found.
