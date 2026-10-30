package main

import (
	"testing"

	"shixin/internal/client"
	"shixin/internal/keymap"
)

func TestShortcutModifierPassesWithoutEngine(t *testing.T) {
	old := core
	core = client.New(client.Options{}, frontApplier{})
	t.Cleanup(func() { core = old })
	for _, c := range []struct {
		code uint16
		flag uint64
	}{
		{55, keymap.MacFlagCommand}, {54, keymap.MacFlagCommand},
		{58, keymap.MacFlagOption}, {61, keymap.MacFlagOption},
		{keymap.MacCapsLock, keymap.MacFlagCapsLock},
	} {
		for _, flag := range []uint64{c.flag, 0} {
			if handleModifier(c.code, flag) {
				t.Errorf("modifier %d flags %#x eaten", c.code, flag)
			}
		}
	}
	if got := core.Stats().Keys; got != 0 {
		t.Fatalf("%d unwatched modifier events sent to the client key path", got)
	}
	if n := testing.AllocsPerRun(1000, func() {
		handleModifier(55, keymap.MacFlagCommand)
		handleModifier(55, 0)
	}); n != 0 {
		t.Fatalf("%v allocations per modifier press/release", n)
	}
}

func BenchmarkModifierSpacingReset(b *testing.B) {
	old := core
	core = client.New(client.Options{}, frontApplier{})
	b.Cleanup(func() { core = old })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handleModifier(55, keymap.MacFlagCommand)
		handleModifier(55, 0)
	}
}
