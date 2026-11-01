//go:build darwin || linux

package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("engine: lock: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("engine: another engine owns %s: %w", path, err)
	}
	// The holder's pid, for StopRunning.
	f.Truncate(0)
	f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return func() { f.Close() }, nil
}

// RunningPID returns the pid of the engine holding userDataDir's lock, or 0
// if none holds it.
func RunningPID(userDataDir string) int {
	path := lockPath(userDataDir)
	if free(path) {
		return 0
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return 0
	}
	return pid
}

// StopRunning asks the engine that owns userDataDir to exit (SIGTERM) and
// waits up to timeout for it to release the user dictionary; then it kills
// it and waits for the lock (DESIGN §3.5: a replacement never starts while
// the old engine might still hold the user dictionary). It returns nil if
// no engine was running.
func StopRunning(userDataDir string, timeout time.Duration) error {
	path := lockPath(userDataDir)
	if free(path) {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return fmt.Errorf("engine: bad pid in %s", path)
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if free(path) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	for i := 0; i < 500; i++ {
		if free(path) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("engine: old engine still holds the lock")
}

// free reports whether nobody holds the lock at path.
func free(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return true
	}
	defer f.Close()
	if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return false
	}
	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return true
}

// UserLocksFree verifies that no other process still holds a LevelDB lock
// in the live user dir. Call only after acquiring Shixin's engine lock.
func UserLocksFree(user string) error {
	paths, err := filepath.Glob(filepath.Join(user, "*.userdb", "LOCK"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("engine: inspect user dictionary lock: %w", err)
		}
		lk := unix.Flock_t{Type: unix.F_WRLCK}
		err = unix.FcntlFlock(f.Fd(), unix.F_GETLK, &lk)
		f.Close()
		if err != nil {
			return fmt.Errorf("engine: inspect user dictionary lock: %w", err)
		}
		if lk.Type != unix.F_UNLCK {
			return errors.New("engine: user dictionary is still owned by another process")
		}
	}
	return nil
}
