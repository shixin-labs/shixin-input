package main

import "time"

const secureOwnerTTL = 250 * time.Millisecond

// secureProcesses contains positive PIDs only when ownership and the
// focused client's app could both be identified.
type secureProcesses struct{ owner, client int32 }

func (p secureProcesses) matches(enabled bool) bool {
	return enabled && p.owner > 0 && p.client > 0 && p.owner == p.client
}

// secureOwnerCache is main-thread state. Identity lookups are limited to
// flag/client/activation changes and one refresh per TTL while enabled.
type secureOwnerCache struct {
	client  uintptr
	valid   bool
	until   time.Time
	process secureProcesses
}

func (s *secureOwnerCache) invalidate() { s.valid = false }

func (s *secureOwnerCache) check(enabled bool, client uintptr, now time.Time, resolve func() secureProcesses) bool {
	if !enabled || client == 0 {
		s.invalidate()
		return false
	}
	if !s.valid || client != s.client || !now.Before(s.until) {
		s.process = resolve()
		s.client, s.valid, s.until = client, true, now.Add(secureOwnerTTL)
	}
	return s.process.matches(enabled)
}
