package dynlib

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

func TestWindowsLibrary(t *testing.T) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	h, err := Open(filepath.Join(dir, "kernel32.dll"), false)
	if err != nil {
		t.Fatal(err)
	}
	sym, err := Symbol(h, "GetCurrentProcessId")
	if err != nil {
		t.Fatal(err)
	}
	pid, _, _ := purego.SyscallN(sym)
	if pid != uintptr(os.Getpid()) {
		t.Fatalf("PID = %d, want %d", pid, os.Getpid())
	}
	if _, err := Symbol(h, "ShixinMissingExport"); err == nil {
		t.Fatal("resolved nonexistent export")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "missing.dll"), false); err == nil {
		t.Fatal("loaded nonexistent DLL")
	}
}
