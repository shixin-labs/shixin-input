package main

import (
	"testing"
	"time"
)

func TestSecureProcesses(t *testing.T) {
	for _, c := range []struct {
		name    string
		enabled bool
		p       secureProcesses
		want    bool
	}{
		{"flag off", false, secureProcesses{42, 42}, false},
		{"focused owner", true, secureProcesses{42, 42}, true},
		{"background owner", true, secureProcesses{41, 42}, false},
		{"missing owner", true, secureProcesses{0, 42}, false},
		{"missing client", true, secureProcesses{42, 0}, false},
		{"both unknown", true, secureProcesses{}, false},
		{"invalid owner", true, secureProcesses{-1, 42}, false},
		{"invalid client", true, secureProcesses{42, -1}, false},
		{"same invalid PID", true, secureProcesses{-1, -1}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.matches(c.enabled); got != c.want {
				t.Fatalf("secure = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSecureOwnerCache(t *testing.T) {
	var s secureOwnerCache
	now := time.Unix(1e9, 0)
	p := secureProcesses{42, 42}
	calls := 0
	resolve := func() secureProcesses { calls++; return p }
	check := func(enabled bool, client uintptr, want bool, wantCalls int) {
		t.Helper()
		if got := s.check(enabled, client, now, resolve); got != want || calls != wantCalls {
			t.Fatalf("secure %v, lookups %d; want %v, %d", got, calls, want, wantCalls)
		}
	}
	check(false, 1, false, 0)
	check(true, 0, false, 0)
	check(true, 1, true, 1)
	// The owner can change while the global flag stays on. Poll at the
	// TTL even when the same app and the same proxy keep focus.
	p.owner = 77
	now = now.Add(secureOwnerTTL - time.Nanosecond)
	check(true, 1, true, 1)
	now = now.Add(time.Nanosecond)
	check(true, 1, false, 2)
	check(true, 1, false, 2) // negative results are cached too
	p.owner = 42
	check(true, 2, true, 3) // another proxy, even in the same app
	p.client = 0            // frontmost bundle doesn't match the IMK app
	s.invalidate()          // activation or workspace app switch
	check(true, 2, false, 4)
	p.client = 42
	check(false, 2, false, 4)
	check(true, 2, true, 5) // off/on invalidates the old ownership
	check(true, 0, false, 5)
	check(true, 2, true, 6)
}

func TestSecureOwnerCacheDoesNotAllocate(t *testing.T) {
	var s secureOwnerCache
	now := time.Unix(1e9, 0)
	resolve := func() secureProcesses { return secureProcesses{42, 42} }
	s.check(true, 1, now, resolve)
	if n := testing.AllocsPerRun(1000, func() { s.check(true, 1, now, resolve) }); n != 0 {
		t.Fatalf("%v allocations per cached owner check", n)
	}
}
