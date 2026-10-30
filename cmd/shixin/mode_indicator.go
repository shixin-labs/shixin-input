package main

import "time"

const modeBubbleDuration = 900 * time.Millisecond

// modeIndicator tracks only confirmed results for the current text-field
// proxy. It does no UI work: even Drain called from a key barrier can update
// it without querying the app, allocating, or showing a window.
type modeIndicator struct {
	client  uintptr
	known   bool
	ascii   bool
	pending bool
	until   time.Time
}

func (m *modeIndicator) focus(client uintptr) bool {
	m.client, m.known = client, false
	return m.dismiss()
}

func (m *modeIndicator) observe(ascii bool) bool {
	if m.client == 0 || m.known && m.ascii == ascii {
		return false
	}
	m.known, m.ascii, m.pending = true, ascii, true
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
