package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("engine: lock: %w", err)
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		f.Close()
		return nil, fmt.Errorf("engine: another engine owns %s: %w", path, err)
	}
	// Keep metadata outside the locked byte: Windows byte-range locks also
	// reject reads and writes from other handles, including PID diagnostics.
	if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 1); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// StopRunning refuses to kill a PID read from a stale lock file. Windows
// upgrades must stop the coordinator through its lifecycle before replacing it.
func StopRunning(userDataDir string, _ time.Duration) error {
	if _, err := os.Stat(lockPath(userDataDir)); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	release, err := lockFile(lockPath(userDataDir))
	if err != nil {
		return errors.New("engine: stop the running coordinator before replacing the engine")
	}
	release()
	return nil
}

// UserLocksFree checks LevelDB's exclusive CreateFile ownership before attach.
// The caller must already hold Shixin's engine lock.
func UserLocksFree(user string) error {
	paths, err := filepath.Glob(filepath.Join(user, "*.userdb", "LOCK"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		p, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			return fmt.Errorf("engine: user dictionary lock unavailable: %w", err)
		}
		windows.CloseHandle(h)
	}
	return nil
}
