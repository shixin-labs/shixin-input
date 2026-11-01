package main

import (
	"errors"
	"os"
	"os/exec"
	"shixin/internal/process"
	"sync"
	"time"

	"shixin/internal/lifecycle"
)

// supervisor starts shixin-engine when clients can't reach it, and restarts
// it after crashes, with backoff (DESIGN §3.5). This helper is the
// persistent coordinator: it runs the builder and owns worker generations.
// It outlives the front: after a restart the front connects to the running
// engine. An engine that exits with lifecycle.ExitLocked found another engine owning
// the user dir; the client simply connects to that one.
type supervisor struct {
	exe  string
	logf func(string, ...any)

	mu       sync.Mutex
	running  bool
	lastTry  time.Time
	backoff  time.Duration
	crashes  int
	disabled bool
}

func newSupervisor(exe string, logf func(string, ...any)) *supervisor {
	return &supervisor{exe: exe, logf: logf, backoff: 500 * time.Millisecond}
}

// ensure starts the engine unless it is running or was started recently.
// Called from the client's connection goroutine; never blocks.
func (s *supervisor) ensure(error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running || s.disabled || time.Since(s.lastTry) < s.backoff {
		return
	}
	s.lastTry = time.Now()
	cmd := exec.Command(s.exe)
	process.Detach(cmd) // survive the front
	if err := cmd.Start(); err != nil {
		s.logf("start engine %s: %v", s.exe, err)
		if errors.Is(err, os.ErrNotExist) {
			s.disabled = true
		}
		return
	}
	s.running = true
	s.logf("started engine (pid %d)", cmd.Process.Pid)
	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.running = false
		var ee *exec.ExitError
		switch {
		case err == nil:
			s.logf("engine exited")
			s.backoff = 500 * time.Millisecond
		case errors.As(err, &ee) && ee.ExitCode() == lifecycle.ExitLocked:
			s.logf("another engine is running; connecting to it")
		default:
			s.crashes++
			if time.Since(s.lastTry) < 10*time.Second {
				s.backoff = min(2*s.backoff, 30*time.Second)
			} else {
				s.backoff = 500 * time.Millisecond
			}
			s.logf("engine exited: %v (crash %d, next start in ≥ %v)", err, s.crashes, s.backoff)
		}
	}()
}
