# Retrospective: the missing candidate window and doubled commits (0.2.2)

November 2026. Status: fixed in development builds; the window fix's
mechanism is being confirmed (see "Open questions").

## Summary

After 0.2.2, a user reported that the candidate window never appeared in
企业微信 (WeCom). It turned out to be missing in several apps (WeCom,
ChatGPT and others) and present in others. Investigating it uncovered three
separate problems:

1. **The candidate window was on screen but never drawn** over some apps.
   macOS reported the freshly shown window as occluded, and MyGo, which
   puts no frames on screen for a window out of sight, never painted it.
   Asking macOS for the window's occlusion state on each show and frame
   makes it appear.
2. **Commits were sometimes typed twice** (all 0.2.2 users). Inserting
   text is a synchronous call into the app; macOS can run Shixin's queued
   `Drain` inside it, and the nested `Drain` applied the results still
   being applied. It needed a slow app call to show, which debug logging
   made certain.
3. **The caret rectangle from the app was trusted blindly.** A NaN, zero
   or off-screen rectangle placed the window off every screen, and the
   caret was read once per composition. This was not the cause of the
   report, but it was a real gap, found while chasing it.

## Impact

- (1) Every composition in the affected apps had no visible candidates.
  Typing still worked: pinyin was marked and Space or a digit committed.
- (2) Any app, intermittently: a phrase typed twice, more likely on a busy
  machine. In a test VM with debug logging, 0.2.2 typed 16 to 24 phrases
  for 14.
- (3) No user-visible case confirmed.

## Timeline

1. **Report:** "the candidate box is sometimes missing" on a machine with two
   displays. The logs had nothing about the window: placement was never
   logged.
2. **First hypothesis, wrong:** WeCom (Qt) answers the caret query before
   it lays out the marked text, with a zero rectangle. The code was
   plausible, a simulator reproduced it, and a fix (caret validation and
   retries) was installed. The report: not fixed. The debug log showed
   WeCom's carets were all valid.
3. **Duplicate commits found** while testing that build in the VM with
   debug logging on: 19 and 20 phrases for 14. 0.2.2 reproduced it, so it
   was not the new code. A Go test with a `Drain` nested in the app's
   calls overflowed the stack: the cause, fixed with a re-entrancy guard.
4. **Placement and visibility logging** (debug mode, numbers only): WeCom's
   caret was valid, the window was placed under it, shown, and reported
   visible. A window-server recording confirmed a window at the right
   place, above WeCom, fully opaque.
5. **Frame logging:** the panel was built with candidates on every key,
   in failing and working apps alike. Shixin's side was identical either
   way.
6. **Window-state logging** (occlusion, space, level) made the window
   appear: a build meant only to observe changed the behavior. Its log
   showed every newly shown window reported as occluded, turning visible
   a frame or two later.
7. **Deliberate fix:** query the occlusion state on each show and drawn
   frame, with debug logging off, as an A/B test against the builds that
   failed.

## What went wrong

- **A fix went to a user's machine on a hypothesis.** The caret theory had
  a mechanism and a simulator test, but no evidence from the failing
  machine. The first message said so, but the fix still cost a round trip.
  Measure first when a log can be added cheaply.
- **The logs couldn't answer the question.** Nothing recorded where the
  window went or whether it showed, so every early step was inference.
- **The question to the reporter came late.** "Which apps fail?" turned a
  WeCom-specific theory into a rendering problem; asking it first would
  have skipped the caret detour.
- **Test tooling misled twice:** a shell expansion (`$want今天` reads as a
  variable named `want今天`) made correct output look wrong, and a
  window recorder reported a stale frontmost app because it never ran an
  event loop.
- **Debug mode changed behavior,** in both directions: its logging made
  doubled commits certain, and its window queries hid the missing window.
  Diagnostics that call into AppKit or the app are not passive.

## What went well

- Instrumentation by layer (caret, placement, visibility, frames, window
  state), each step ruling one layer out with numbers rather than guesses.
- Reproducing the doubled commit in a plain Go test once its trigger was
  understood, then verifying every fix by disabling it and watching its
  test fail.
- Keeping privacy: every log added records positions, sizes and states,
  never text or images of it.

## Changes

- `internal/client`: `Drain` ignores a nested call and has the outer one
  look again. Tests: `TestNestedDrain` and the re-entrant variants of
  `TestTypingInSimulatedApps`.
- `cmd/shixin`: the caret tracker validates the app's caret, keeps the last
  usable one and asks again a frame later; placement never converts NaN.
  Tests: `TestUsableCaret`, `TestCaretInSimulatedApps`,
  `TestCaretRetriesAreBounded`, `TestCandidatePositionNaN`.
- `cmd/shixin`: the candidate window asks for its occlusion state on each
  show and drawn frame. Test: `TestCandidateFramesRefreshOcclusion`.
- `internal/appsim`: simulated app text fields for apps the VM can't run
  (they need an account): Cocoa, Qt's late layout, WeChat 4 and Chromium
  typing eaten keys, broken carets, and app calls that run Shixin's queued
  work.
- Debug mode logs caret answers, placement, visibility, drawn frames and
  window state.

## Open questions

- **The occlusion mechanism isn't proven.** The deliberate query is under
  an A/B test with debug logging off. If it holds, the fix belongs in
  MyGo, which should not leave a window drawn while AppKit's cached state
  calls it occluded; report it upstream.
- Why only some apps: the affected ones may keep the main thread busy in
  synchronous input-method calls, delaying AppKit's occlusion
  notification. Not verified.

## Action items

- Confirm the occlusion fix, then report the behavior to MyGo.
- Release the `Drain` fix (it affects every 0.2.2 user).
- Log app-compatibility facts by default, not only in debug mode, as
  numbers: an unusable caret, a window shown but reported occluded.
- Add a simulator profile whenever an app-specific bug is found, and run
  the typing smoke test with debug logging on as well as off, since
  slowness changes behavior.
- Ask reporters which apps are affected before forming a theory.
