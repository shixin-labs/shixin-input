package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"shixin/internal/lifecycle"
)

// An engine that keeps exiting with ExitLocked (another engine owns the
// user dir) must not be respawned every half second: after an upgrade the
// old engine held the lock and the front started fourteen in 27 s.
func TestSupervisorBacksOffWhileLocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the engine")
	}
	exe := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(exe, []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", lifecycle.ExitLocked)), 0o755); err != nil {
		t.Fatal(err)
	}
	s := newSupervisor(exe, t.Logf)
	var backoffs []time.Duration
	for range 3 {
		s.mu.Lock()
		s.lastTry = time.Time{} // let ensure start it now
		s.mu.Unlock()
		s.ensure(nil)
		for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(5 * time.Millisecond) {
			s.mu.Lock()
			running := s.running
			s.mu.Unlock()
			if !running {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("engine didn't exit")
			}
		}
		s.mu.Lock()
		backoffs = append(backoffs, s.backoff)
		s.mu.Unlock()
	}
	if !(backoffs[0] > 500*time.Millisecond && backoffs[1] > backoffs[0] && backoffs[2] > backoffs[1]) {
		t.Fatalf("backoff after locked exits: %v, want growing", backoffs)
	}
}
