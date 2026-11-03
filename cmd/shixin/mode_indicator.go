package main

import "time"

const (
	modeBubbleDuration = 900 * time.Millisecond
	// modeRefocusGrace is how long a notification not yet shown outlives
	// its text field (left) in case the same app comes back: switching
	// apps, macOS often deactivates and reactivates the input method
	// within milliseconds. A bubble already shown outlives it until it
	// expires: Slack deactivates the input method right after activating
	// it and reactivates its field only a second or more later.
	modeRefocusGrace = 100 * time.Millisecond
)

// modeIndicator tracks only confirmed results for the current text-field
// proxy. It does no UI work: even Drain called from a key barrier can update
// it without querying the app, allocating, or showing a window.
//
// The bubble shows when the confirmed mode differs from the last one, in
// any field: a Shift toggle, or a per-app mode that switches on focus. It
// also shows once after 拾新 is selected from another input source: macOS
// shows only the 拾 icon then, not the mode. And it shows the mode once
// after switching to another app (focusApp), changed or not, so that the
// mode is always known when typing starts there. Focusing another field
// of the same app in the same mode shows nothing.
type modeIndicator struct {
	client   uintptr
	app      string    // the focused field's app, when known (focusApp)
	leftApp  string    // the app whose field left (left), within the grace
	leftAt   time.Time // when it left
	known    bool
	ascii    bool
	fresh    bool // a mode was confirmed since the last focus change
	announce bool // 拾新 was selected; show the next confirmed mode
	switched bool // another app was focused; show its next confirmed mode
	beside   bool // the shown mode answers a selection: clear macOS's badge
	pending  bool
	until    time.Time
}

func (m *modeIndicator) focus(client uintptr) bool {
	m.client, m.fresh, m.app, m.leftApp, m.switched = client, false, "", "", false
	if client == 0 {
		m.announce = false
	}
	return m.dismiss()
}

// focusApp is focus for a field of app. The same field again changes
// nothing, and a field of the app that just left (left) comes back to
// its bubble and pending notification within modeRefocusGrace: a quick
// deactivation and reactivation neither flashes nor drops the bubble. It
// reports whether the bubble needs an update.
func (m *modeIndicator) focusApp(client uintptr, app string, now time.Time) bool {
	switch {
	case client != 0 && client == m.client:
		return false
	case client != 0 && app != "" && app == m.leftApp && m.waiting(now):
		m.client, m.app, m.leftApp = client, app, ""
		return m.pending || !m.until.IsZero()
	}
	prev := m.app
	if m.client == 0 && m.leftApp != "" {
		prev = m.leftApp
	}
	changed := m.focus(client)
	m.app = app
	m.switched = client != 0 && app != "" && prev != "" && app != prev
	return changed
}

// left records that the focused field went away (macOS's
// deactivateServer:). With its app known, a shown bubble stays until it
// expires and a pending notification waits modeRefocusGrace, for the app
// to come back (focusApp); another app's field or a dismissal ends them.
// Without its app, it is focus(0).
func (m *modeIndicator) left(now time.Time) bool {
	if m.client == 0 {
		return false
	}
	if m.app == "" {
		return m.focus(0)
	}
	m.leftApp, m.leftAt, m.client = m.app, now, 0
	return m.pending || !m.until.IsZero()
}

// waiting reports whether the field that left may still come back to
// its bubble: within the grace, or while its bubble shows.
func (m *modeIndicator) waiting(now time.Time) bool {
	return m.leftApp != "" && (now.Before(m.leftAt.Add(modeRefocusGrace)) || !m.until.IsZero() && now.Before(m.until))
}

// deadline is when the bubble must be looked at again: its expiry, or,
// after its field left with nothing shown yet, the end of the grace.
func (m *modeIndicator) deadline() time.Time {
	if m.client == 0 && m.leftApp != "" && m.until.IsZero() {
		return m.leftAt.Add(modeRefocusGrace)
	}
	return m.until
}

// selected records that 拾新 became the input source. The focused field's
// mode shows now if its result has arrived, otherwise with that result.
func (m *modeIndicator) selected() bool {
	if m.client != 0 && m.fresh {
		m.announce, m.pending, m.beside = false, true, true
		return true
	}
	m.announce = true
	return false
}

func (m *modeIndicator) observe(ascii bool) bool {
	if m.client == 0 {
		return false
	}
	m.fresh = true
	if m.announce || m.switched {
		// After a selection, macOS's 拾 badge is under the caret: clear it.
		m.pending, m.beside = true, m.announce
		m.announce, m.switched = false, false
		m.known, m.ascii = true, ascii
		return true
	}
	if m.known && m.ascii == ascii {
		return false
	}
	first := !m.known
	m.known, m.ascii = true, ascii
	if first {
		return false
	}
	m.pending, m.beside = true, false
	return true
}

func (m *modeIndicator) dismiss() bool {
	changed := m.pending || !m.until.IsZero()
	m.pending, m.until = false, time.Time{}
	return changed
}

// visible starts a pending notification on the UI callback, not on the key
// path. Expiration callbacks always consult the latest deadline, so an old
// timer cannot hide a newer toggle.
func (m *modeIndicator) visible(now time.Time) bool {
	if m.client == 0 {
		if m.leftApp != "" && !m.waiting(now) {
			// The field that left didn't come back: as focus(0) then.
			m.leftApp, m.announce = "", false
		}
		if m.leftApp == "" {
			// No field (an announcement of 拾新 waits for one).
			m.pending, m.until = false, time.Time{}
			return false
		}
		// The field may come back: a shown bubble stays until it expires;
		// a notification not yet shown waits, unshown, for the grace.
		return !m.pending && !m.until.IsZero() && now.Before(m.until)
	}
	if m.pending {
		m.pending, m.until = false, now.Add(modeBubbleDuration)
	}
	if !now.Before(m.until) {
		m.until = time.Time{}
		return false
	}
	return true
}

// paintWait holds a shown bubble's time on screen until macOS reports it
// visible. A window just shown reports itself occluded, and MyGo paints
// nothing until that changes, which can take a good part of the bubble's
// duration while an app switch animates: the bubble then showed only
// briefly. The front checks once a frame, at most paintWaitChecks times.
type paintWait struct {
	waiting bool
	checks  int
}

const paintWaitChecks = 30 // a frame apart: about half a second

// start begins waiting after the bubble shows.
func (p *paintWait) start() { p.waiting, p.checks = true, 0 }

// check records whether the bubble is visible now. While it isn't, its
// duration restarts from now; it reports whether to check again.
func (p *paintWait) check(m *modeIndicator, visible bool, now time.Time) bool {
	if !p.waiting || m.until.IsZero() {
		p.waiting = false
		return false
	}
	if !visible && p.checks < paintWaitChecks {
		p.checks++
		m.until = now.Add(modeBubbleDuration)
		return true
	}
	p.waiting = false
	if !visible || p.checks > 0 {
		m.until = now.Add(modeBubbleDuration) // the full duration from first sight
	}
	return false
}
