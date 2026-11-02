package main

import "time"

const modeBubbleDuration = 900 * time.Millisecond

// modeIndicator tracks only confirmed results for the current text-field
// proxy. It does no UI work: even Drain called from a key barrier can update
// it without querying the app, allocating, or showing a window.
//
// The bubble shows when the confirmed mode differs from the last one, in
// any field: a Shift toggle, or a per-app mode that switches on focus. It
// also shows once after 拾新 is selected from another input source: macOS
// shows only the 拾 icon then, not the mode. Focusing a field in the same
// mode shows nothing.
type modeIndicator struct {
	client   uintptr
	known    bool
	ascii    bool
	fresh    bool // a mode was confirmed since the last focus change
	announce bool // 拾新 was selected; show the next confirmed mode
	beside   bool // the shown mode answers a selection: clear macOS's badge
	pending  bool
	until    time.Time
}

func (m *modeIndicator) focus(client uintptr) bool {
	m.client, m.fresh = client, false
	if client == 0 {
		m.announce = false
	}
	return m.dismiss()
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
	if m.announce {
		m.announce, m.pending, m.beside = false, true, true
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
	if m.pending {
		m.pending, m.until = false, now.Add(modeBubbleDuration)
	}
	if m.client == 0 || !now.Before(m.until) {
		m.until = time.Time{}
		return false
	}
	return true
}
