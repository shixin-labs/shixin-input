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
