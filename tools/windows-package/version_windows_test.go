package main

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsReleaseFileVersion(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "owned.dll")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	// A second build with the same upstream API still receives the next
	// release's monotonically increasing file version.
	for _, release := range []string{"0.2.2", "0.2.3"} {
		if err := stampFile(path, release); err != nil {
			t.Fatal(err)
		}
		p, _ := windows.UTF16PtrFromString(path)
		var version, language [128]uint16
		n, m := uint32(len(version)), uint32(len(language))
		result, _, _ := windows.NewLazySystemDLL("msi.dll").NewProc("MsiGetFileVersionW").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&version[0])), uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&language[0])), uintptr(unsafe.Pointer(&m)))
		if result != 0 {
			t.Fatalf("Windows rejected resource: %d", result)
		}
		if got := windows.UTF16ToString(version[:]); got != "100."+release {
			t.Fatalf("file version %q", got)
		}
		if got := windows.UTF16ToString(language[:]); got != "1033" {
			t.Fatalf("file language %q", got)
		}
	}
}
