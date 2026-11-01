package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsEngineLock(t *testing.T) {
	if path := os.Getenv("SHIXIN_TEST_LOCK_CHILD"); path != "" {
		release, err := Lock(path)
		if err == nil {
			release()
			os.Exit(10)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if second, err := Lock(dir); err == nil {
		second()
		t.Fatal("second owner acquired the engine lock")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestWindowsEngineLock$")
	cmd.Env = append(os.Environ(), "SHIXIN_TEST_LOCK_CHILD="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child acquired lock: %v %s", err, out)
	}
	if err := StopRunning(dir, time.Millisecond); err == nil {
		t.Fatal("reported a locked engine as stopped")
	}
	release()
	second, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	second()
}

func TestWindowsDictionaryOwnership(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "test.userdb")
	if err := os.Mkdir(db, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(db, "LOCK")
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// Match LevelDB's WindowsEnv::LockFile: share reads, deny writes.
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if h != windows.InvalidHandle {
			windows.CloseHandle(h)
		}
	}()
	if err := UserLocksFree(dir); err == nil {
		t.Fatal("accepted a dictionary owned by another handle")
	}
	windows.CloseHandle(h)
	h = windows.InvalidHandle
	if err := UserLocksFree(dir); err != nil {
		t.Fatal(err)
	}
}
