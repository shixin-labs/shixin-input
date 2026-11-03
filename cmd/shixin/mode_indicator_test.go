package main

import (
	"testing"
	"time"
)

func TestModeIndicatorConfirmedChanges(t *testing.T) {
	var m modeIndicator
	now := time.Unix(1, 0)
	if m.observe(false) || m.visible(now) {
		t.Fatal("a result without a focused client showed a bubble")
	}
	m.focus(1)
	if m.visible(now) {
		t.Fatal("focus guessed a mode before its result")
	}
	if m.observe(false) || m.visible(now) {
		t.Fatal("the first confirmed mode was shown without an input source switch")
	}
	if !m.observe(true) || !m.visible(now) || !m.ascii {
		t.Fatal("switch to English was not shown")
	}
	until := m.until
	if m.observe(true) || m.until != until {
		t.Fatal("ordinary typing restarted the notification")
	}
	if !m.visible(until.Add(-time.Nanosecond)) || m.visible(until) {
		t.Fatal("notification did not expire at its deadline")
	}
	if m.observe(true) {
		t.Fatal("unchanged mode reappeared after expiration")
	}
	if !m.observe(false) || !m.visible(until.Add(time.Millisecond)) || m.ascii {
		t.Fatal("switch back to Chinese was not shown")
	}
}

func TestModeIndicatorLatestDeadline(t *testing.T) {
	var m modeIndicator
	m.focus(1)
	now := time.Unix(1, 0)
	m.observe(false)
	m.observe(true)
	m.visible(now)
	oldDeadline := m.until
	m.observe(false)
	// An already queued old timer callback consumes the pending new mode.
	if !m.visible(oldDeadline) || m.until != oldDeadline.Add(modeBubbleDuration) {
		t.Fatal("old expiration hid the newer mode")
	}
	deadline := m.until
	if !m.visible(oldDeadline.Add(time.Millisecond)) || m.until != deadline {
		t.Fatal("a duplicate callback extended the deadline")
	}
}

func TestModeIndicatorFocusAndDismissal(t *testing.T) {
	var m modeIndicator
	now := time.Unix(1, 0)
	m.focus(1)
	m.observe(false)
	m.observe(true)
	m.visible(now)
	if !m.focus(2) || m.visible(now) {
		t.Fatal("the old field's bubble survived a focus change")
	}
	if m.observe(true) || m.visible(now) {
		t.Fatal("a new field in the same mode showed a bubble")
	}
	// A per-app mode switching on focus is a change.
	m.focus(3)
	if !m.observe(false) || !m.visible(now) {
		t.Fatal("a new field's different mode was not shown")
	}
	if !m.dismiss() || m.visible(now) || m.dismiss() {
		t.Fatal("dismissal did not clear pending/visible state exactly once")
	}
	if m.observe(false) {
		t.Fatal("dismissal forgot the mode and flashed on ordinary typing")
	}
	m.focus(0)
	if m.pending || m.visible(now) || m.observe(true) {
		t.Fatal("a late result resurrected a deactivated field's bubble")
	}
}

func TestModeIndicatorInputSourceSelection(t *testing.T) {
	var m modeIndicator
	now := time.Unix(1, 0)
	m.focus(1)
	m.observe(false)
	m.focus(0) // switched to another input source

	// Selected from the menu: the notification precedes the field's result.
	if m.selected() || m.visible(now) {
		t.Fatal("selection without a focused field showed a bubble")
	}
	m.focus(2)
	if !m.observe(false) || !m.visible(now) || m.ascii || !m.beside {
		t.Fatal("the unchanged mode was not shown beside the 拾 badge")
	}
	m.dismiss()
	if m.observe(false) {
		t.Fatal("selection announced more than one result")
	}

	// Selected by shortcut: the result precedes the notification.
	m.focus(0)
	m.focus(3)
	m.observe(true)
	if !m.selected() || !m.visible(now) || !m.ascii || !m.beside {
		t.Fatal("the confirmed mode was not shown beside the 拾 badge")
	}
	if m.observe(true) {
		t.Fatal("the shown selection was announced again")
	}
	if !m.observe(false) || m.beside {
		t.Fatal("a Shift toggle, with no system badge, was placed beside one")
	}

	// A selection that never reached a text field is forgotten on deactivation.
	m.focus(0)
	m.selected()
	m.focus(0)
	m.focus(4)
	if m.observe(false) {
		t.Fatal("a stale selection announced a later field's mode")
	}
}

func TestModeIndicatorDoesNotAllocate(t *testing.T) {
	var m modeIndicator
	now := time.Unix(1, 0)
	if n := testing.AllocsPerRun(1000, func() {
		m.focus(1)
		m.observe(false)
		m.visible(now)
		m.observe(false)
		m.observe(true)
		m.visible(now)
		m.dismiss()
		m.selected()
		m.focus(0)
	}); n != 0 {
		t.Fatalf("mode tracking allocates %v times", n)
	}
}

var modeBenchmarkState modeIndicator

func BenchmarkModeIndicator(b *testing.B) {
	for _, toggle := range []bool{false, true} {
		name := "unchanged"
		if toggle {
			name = "toggle"
		}
		b.Run(name, func(b *testing.B) {
			m := &modeBenchmarkState // keep state observable to the compiler
			*m = modeIndicator{}
			m.focus(1)
			m.observe(false)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m.observe(toggle && i%2 == 0)
			}
		})
	}
}

// shownIn returns an indicator showing a mode switch in a field of app.
func shownIn(t *testing.T, app string, now time.Time) *modeIndicator {
	t.Helper()
	m := &modeIndicator{}
	m.focusApp(1, app, now)
	m.observe(false)
	if !m.observe(true) || !m.visible(now) {
		t.Fatal("setup: the switch was not shown")
	}
	return m
}

// Switching apps, macOS often deactivates and reactivates the input method
// within milliseconds, with a new proxy for the same app. The bubble must
// neither flash (hidden a few ms after showing) nor be dropped.
func TestModeIndicatorRefocusBurst(t *testing.T) {
	now := time.Unix(1, 0)
	m := shownIn(t, "com.tencent.WeWorkMac", now)
	until := m.until
	m.left(now.Add(2 * time.Millisecond))
	if !m.visible(now.Add(3 * time.Millisecond)) {
		t.Fatal("the bubble hid while its app might come back")
	}
	if !m.focusApp(2, "com.tencent.WeWorkMac", now.Add(5*time.Millisecond)) {
		t.Fatal("coming back did not ask to update the bubble")
	}
	if !m.visible(now.Add(6*time.Millisecond)) || m.until != until {
		t.Fatal("the bubble flashed: hidden or restarted after a deactivate/activate burst")
	}
	if m.focusApp(2, "com.tencent.WeWorkMac", now.Add(7*time.Millisecond)) || !m.visible(now.Add(8*time.Millisecond)) {
		t.Fatal("activating the same field again dismissed the bubble")
	}
}

// A mode that changed on focus, but wasn't shown before the burst, still
// shows after it.
func TestModeIndicatorPendingSurvivesBurst(t *testing.T) {
	now := time.Unix(1, 0)
	var m modeIndicator
	m.focusApp(1, "com.openai.chat", now)
	m.observe(false)
	if !m.observe(true) {
		t.Fatal("setup: no pending switch")
	}
	m.left(now.Add(time.Millisecond))
	m.focusApp(2, "com.openai.chat", now.Add(4*time.Millisecond))
	if !m.visible(now.Add(5*time.Millisecond)) || !m.ascii {
		t.Fatal("the burst dropped the pending mode switch")
	}
}

// After 拾新 is selected, the mode shows once even when a burst comes
// between the selection and the mode's result.
func TestModeIndicatorAnnounceSurvivesBurst(t *testing.T) {
	now := time.Unix(1, 0)
	var m modeIndicator
	m.focusApp(1, "com.apple.TextEdit", now)
	m.selected() // before the field's result
	m.left(now.Add(time.Millisecond))
	m.focusApp(2, "com.apple.TextEdit", now.Add(3*time.Millisecond))
	if !m.observe(false) || !m.visible(now.Add(4*time.Millisecond)) || !m.beside {
		t.Fatal("the burst dropped the announcement of 拾新's mode")
	}
}

// Another app, or the same one after the grace, is a real focus change;
// a field that left and never came back hides the bubble after the grace.
func TestModeIndicatorGraceEnds(t *testing.T) {
	now := time.Unix(1, 0)
	m := shownIn(t, "com.apple.Notes", now)
	m.left(now)
	if !m.focusApp(2, "com.apple.Safari", now.Add(time.Millisecond)) || m.visible(now.Add(2*time.Millisecond)) {
		t.Fatal("another app kept the bubble")
	}

	m = shownIn(t, "com.apple.Notes", now)
	m.left(now)
	m.focusApp(2, "com.apple.Notes", m.until)
	if m.visible(m.until.Add(time.Millisecond)) {
		t.Fatal("the app came back after its bubble ended and kept it")
	}

	// A shown bubble whose field left stays until it expires, then
	// forgets the field.
	m = shownIn(t, "com.apple.Notes", now)
	until := m.until
	m.left(now)
	if got := m.deadline(); !got.Equal(until) {
		t.Fatalf("deadline %v, want the bubble's expiry %v", got, until)
	}
	if !m.visible(until.Add(-time.Millisecond)) || m.visible(until) {
		t.Fatal("the bubble didn't last exactly until its expiry")
	}
	if m.announce || m.leftApp != "" {
		t.Fatal("the bubble's end left state behind")
	}

	// A notification not yet shown waits only the grace.
	var p modeIndicator
	p.focusApp(1, "com.apple.Notes", now)
	p.observe(false)
	p.observe(true)
	p.left(now)
	if got := p.deadline(); !got.Equal(now.Add(modeRefocusGrace)) {
		t.Fatalf("deadline %v, want the end of the grace", got)
	}
	p.pending = false // as if consumed while no field could show it
	if p.visible(now.Add(modeRefocusGrace)) || p.leftApp != "" {
		t.Fatal("a field gone past the grace kept its notification")
	}
}

// Slack deactivates the input method right after activating it and
// reactivates its field a second or more later: the bubble must stay its
// full duration, and Slack's return is not a new switch.
func TestModeIndicatorSlackDeactivates(t *testing.T) {
	now := time.Unix(1, 0)
	m := shownIn(t, "com.tinyspeck.slackmacgap", now)
	until := m.until
	m.left(now.Add(2 * time.Millisecond))
	if !m.visible(now.Add(500 * time.Millisecond)) {
		t.Fatal("the bubble hid while Slack had deactivated the input method")
	}
	m.focusApp(2, "com.tinyspeck.slackmacgap", now.Add(800*time.Millisecond))
	if m.switched || !m.visible(now.Add(850*time.Millisecond)) || m.until != until {
		t.Fatal("Slack's return restarted or ended the bubble")
	}
	if m.visible(until) {
		t.Fatal("the bubble outlived its duration")
	}
	// Returning after the bubble ended is Slack again, not a switch.
	m.left(until.Add(time.Second))
	m.focusApp(3, "com.tinyspeck.slackmacgap", until.Add(2*time.Second))
	if m.observe(true) { // shownIn ends in English
		t.Fatal("returning to the same app announced the unchanged mode")
	}
}

// Without an app (Windows), leaving is focus(0), as before.
func TestModeIndicatorLeftWithoutApp(t *testing.T) {
	now := time.Unix(1, 0)
	var m modeIndicator
	m.focus(1)
	m.observe(false)
	m.observe(true)
	m.visible(now)
	if !m.left(now) || m.visible(now) {
		t.Fatal("leaving without an app kept the bubble")
	}
}

// Switching to another app shows its mode once, changed or not, as
// macOS delivers it: the old app's field leaves, the new app's comes.
// Another field of the same app shows only a change.
func TestModeIndicatorAppSwitch(t *testing.T) {
	now := time.Unix(1, 0)
	var m modeIndicator
	m.focusApp(1, "com.tencent.WeWorkMac", now)
	if m.observe(false) || m.visible(now) {
		t.Fatal("the first mode ever confirmed was shown")
	}
	for i, tc := range []struct {
		app   string
		ascii bool
		show  bool
	}{
		{"com.openai.chat", false, true},        // same mode, another app
		{"com.openai.chat", false, false},       // another field of that app
		{"com.apple.Terminal", true, true},      // another app, another mode
		{"com.tencent.WeWorkMac", false, true},  // back again
		{"com.tencent.WeWorkMac", false, false}, // same app, same mode
	} {
		at := now.Add(time.Duration(i+1) * time.Second)
		m.left(at)
		m.focusApp(uintptr(i+2), tc.app, at.Add(5*time.Millisecond))
		got := m.observe(tc.ascii) && m.visible(at.Add(6*time.Millisecond))
		if got != tc.show || got && (m.ascii != tc.ascii || m.beside) {
			t.Fatalf("switch %d to %s: shown %v (ascii %v, beside %v), want %v", i, tc.app, got, m.ascii, m.beside, tc.show)
		}
		if m.observe(tc.ascii) {
			t.Fatalf("switch %d: the same mode was announced twice", i)
		}
		m.dismiss()
	}
}

// A quick deactivation and reactivation of the same app is not a switch.
func TestModeIndicatorBurstIsNotSwitch(t *testing.T) {
	now := time.Unix(1, 0)
	var m modeIndicator
	m.focusApp(1, "com.apple.Notes", now)
	m.observe(false)
	m.left(now.Add(time.Millisecond))
	m.focusApp(2, "com.apple.Notes", now.Add(3*time.Millisecond))
	if m.observe(false) || m.visible(now.Add(4*time.Millisecond)) {
		t.Fatal("a deactivate/activate burst announced the unchanged mode")
	}
}

// A shown bubble's duration counts from when macOS reports it visible,
// not from when it was shown: switching apps, painting can lag by a good
// part of the duration, and the bubble then showed only briefly.
func TestPaintWait(t *testing.T) {
	now := time.Unix(1, 0)
	shown := func() *modeIndicator {
		m := shownIn(t, "com.stablyai.orca", now)
		return m
	}

	// Visible at once: nothing changes.
	m, p := shown(), paintWait{}
	until := m.until
	p.start()
	if p.check(m, true, now) || m.until != until {
		t.Fatal("a bubble visible at once had its duration changed")
	}

	// Visible after 1.25 s of late checks (a busy main thread during an
	// app switch): never expired meanwhile, and the full duration from then.
	m, p = shown(), paintWait{}
	p.start()
	at := now
	for i := 0; i < 25; i++ {
		at = at.Add(50 * time.Millisecond)
		if !p.check(m, false, at) {
			t.Fatalf("stopped waiting after %d checks", i+1)
		}
		if !m.visible(at) {
			t.Fatal("the bubble expired while not yet on screen")
		}
	}
	seen := at.Add(50 * time.Millisecond)
	if p.check(m, true, seen) || !m.until.Equal(seen.Add(modeBubbleDuration)) {
		t.Fatalf("visible at %v: until %v, want the full duration from then", seen, m.until)
	}

	// Never reported visible: give up after paintWaitChecks.
	m, p = shown(), paintWait{}
	p.start()
	checks := 0
	for p.check(m, false, now.Add(time.Duration(checks)*16*time.Millisecond)) {
		if checks++; checks > 100 {
			t.Fatal("never stopped waiting")
		}
	}
	if checks != paintWaitChecks || m.until.IsZero() {
		t.Fatalf("gave up after %d checks, until %v", checks, m.until)
	}

	// Dismissed while waiting: stop, and don't revive it.
	m, p = shown(), paintWait{}
	p.start()
	p.check(m, false, now)
	m.dismiss()
	if p.check(m, false, now.Add(16*time.Millisecond)) || !m.until.IsZero() {
		t.Fatal("waiting revived a dismissed bubble")
	}
}

// A mode notification not yet shown when its field leaves waits only the
// grace, unshown: it shows if the app comes back in time, and is dropped
// otherwise, never kept alive by a display time it never had.
func TestModeIndicatorPendingGrace(t *testing.T) {
	now := time.Unix(1, 0)
	pending := func() *modeIndicator {
		m := &modeIndicator{}
		m.focusApp(1, "com.tinyspeck.slackmacgap", now)
		m.observe(false)
		if !m.observe(true) {
			t.Fatal("setup: no pending switch")
		}
		m.left(now.Add(time.Millisecond))
		return m
	}
	m := pending()
	if m.visible(now.Add(50 * time.Millisecond)) {
		t.Fatal("shown with no field")
	}
	if !m.pending || !m.until.IsZero() {
		t.Fatal("the unshown notification was given a display time")
	}
	m.focusApp(2, "com.tinyspeck.slackmacgap", now.Add(60*time.Millisecond))
	if !m.visible(now.Add(61 * time.Millisecond)) {
		t.Fatal("the app came back in time and its notification didn't show")
	}

	m = pending()
	m.visible(now.Add(50 * time.Millisecond)) // the queued UI callback
	m.focusApp(2, "com.tinyspeck.slackmacgap", now.Add(200*time.Millisecond))
	if m.visible(now.Add(201 * time.Millisecond)) {
		t.Fatal("a notification outlived its grace")
	}
}
