//go:build darwin || linux

package main

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func dbOwner(t *testing.T, user string) int {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(user, "rime_ice.userdb", "LOCK"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lk := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(f.Fd(), unix.F_GETLK, &lk); err != nil {
		t.Fatal(err)
	}
	if lk.Type == unix.F_UNLCK {
		return 0
	}
	return int(lk.Pid)
}
