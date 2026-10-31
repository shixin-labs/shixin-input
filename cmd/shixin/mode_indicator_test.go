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
		t.Fatal("the first confirmed mode was shown; macOS shows its own indicator")
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
