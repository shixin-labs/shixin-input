package keymap

import (
	"testing"

	"shixin/internal/policy"
)

func TestMacKeyDown(t *testing.T) {
	for _, c := range []struct {
		code uint16
		ch   byte
		want uint32
		ok   bool
	}{
		{0, 'a', 'a', true},
		{18, '!', '!', true},
		{36, '\r', 0xff0d, true},
		{49, ' ', ' ', true},
		{51, 0x7f, 0xff08, true},
		{123, 0, 0xff51, true},
		{118, 0, 0xffc1, true},
		{0, 0xe6, 0, false}, // non-ASCII from another layout
	} {
		got, ok := MacKeyDown(c.code, c.ch)
		if got != c.want || ok != c.ok {
			t.Errorf("MacKeyDown(%d, %#x) = %#x %v", c.code, c.ch, got, ok)
		}
	}
}

func TestMacModifierKey(t *testing.T) {
	k, m, ok := MacModifierKey(MacShiftL, MacFlagShift)
	if !ok || k != policy.KeyShiftL || m != policy.ShiftMask {
		t.Errorf("Shift down: %#x %#x %v", k, m, ok)
	}
	k, m, ok = MacModifierKey(MacShiftR, 0)
	if !ok || k != policy.KeyShiftR || m != policy.ReleaseMask {
		t.Errorf("Shift_R up: %#x %#x %v", k, m, ok)
	}
	if _, _, ok := MacModifierKey(MacCapsLock, MacFlagCapsLock); ok {
		t.Error("Caps Lock reported")
	}
	if m := MacMods(MacFlagCommand | MacFlagShift); m != policy.SuperMask|policy.ShiftMask {
		t.Errorf("MacMods = %#x", m)
	}
}

func TestNoAllocs(t *testing.T) {
	n := testing.AllocsPerRun(100, func() {
		MacKeyDown(0, 'a')
		MacModifierKey(MacShiftL, MacFlagShift)
	})
	if n != 0 {
		t.Fatal(n)
	}
}

// Releasing one side of a modifier while the other is held keeps the generic
// flag set; the device flags must still report it as a release.
func TestMacModifierKeyBothSides(t *testing.T) {
	for _, c := range []struct {
		name    string
		code    uint16
		flags   uint64
		keysym  uint32
		release bool
	}{
		{"Shift_L down", MacShiftL, MacFlagShift | MacFlagShiftL, policy.KeyShiftL, false},
		{"Shift_R down, Shift_L held", MacShiftR, MacFlagShift | MacFlagShiftL | MacFlagShiftR, policy.KeyShiftR, false},
		{"Shift_L up, Shift_R held", MacShiftL, MacFlagShift | MacFlagShiftR, policy.KeyShiftL, true},
		{"Shift_R up, Shift_L held", MacShiftR, MacFlagShift | MacFlagShiftL, policy.KeyShiftR, true},
		{"Shift_R up, last", MacShiftR, 0, policy.KeyShiftR, true},
		{"Control_L up, Control_R held", MacControlL, MacFlagControl | MacFlagControlR, policy.KeyControlL, true},
		{"Control_R down", MacControlR, MacFlagControl | MacFlagControlR, policy.KeyControlR, false},
		{"Control_R up, Control_L held", MacControlR, MacFlagControl | MacFlagControlL, policy.KeyControlR, true},
		// Posted events may lack device flags: the generic flag decides.
		{"posted Shift_R down", MacShiftR, MacFlagShift, policy.KeyShiftR, false},
		{"posted Control_L down", MacControlL, MacFlagControl, policy.KeyControlL, false},
		// Another modifier's side flags don't count as this one's.
		{"Shift_L down, Control_R held", MacShiftL, MacFlagShift | MacFlagControl | MacFlagControlR, policy.KeyShiftL, false},
	} {
		k, m, ok := MacModifierKey(c.code, c.flags)
		if !ok || k != c.keysym || (m&policy.ReleaseMask != 0) != c.release {
			t.Errorf("%s: %#x %#x %v", c.name, k, m, ok)
		}
	}
}

func TestMacMods(t *testing.T) {
	for _, c := range []struct {
		flags uint64
		want  uint32
	}{
		{0, 0},
		{MacFlagCapsLock, policy.LockMask},
		{MacFlagControl | MacFlagOption, policy.ControlMask | policy.AltMask},
		{MacFlagShift | MacFlagControl | MacFlagOption | MacFlagCommand | MacFlagCapsLock,
			policy.ShiftMask | policy.ControlMask | policy.AltMask | policy.SuperMask | policy.LockMask},
		// Device flags and unrelated bits (numeric pad, function) add nothing.
		{MacFlagShiftL | MacFlagControlR | 1<<21 | 1<<23, 0},
	} {
		if got := MacMods(c.flags); got != c.want {
			t.Errorf("MacMods(%#x) = %#x, want %#x", c.flags, got, c.want)
		}
	}
}

// Control characters and DEL never become keysyms on their own; codes
// past the table pass through unless they carry a printable character.
func TestMacKeyDownRejectsControlCharacters(t *testing.T) {
	for _, ch := range []byte{0, 0x01, 0x1b, ' ', 0x7f} {
		if k, ok := MacKeyDown(0, ch); ok {
			t.Errorf("MacKeyDown(0, %#x) = %#x", ch, k)
		}
	}
	if k, ok := MacKeyDown(200, 'q'); !ok || k != 'q' {
		t.Errorf("MacKeyDown(200, 'q') = %#x %v", k, ok)
	}
	if _, ok := MacKeyDown(200, 0); ok {
		t.Error("an unknown code without a character was accepted")
	}
}
