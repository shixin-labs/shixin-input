package rime

import (
	"os"
	"testing"
)

const brewLib = "/opt/homebrew/opt/librime/lib/librime.1.dylib"

func loadForTest(t *testing.T) *Lib {
	t.Helper()
	path := os.Getenv("SHIXIN_LIBRIME")
	if path == "" {
		path = brewLib
		if _, err := os.Stat(path); err != nil {
			t.Skip("librime not installed; set SHIXIN_LIBRIME")
		}
	}
	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// The key path must not allocate (DESIGN R5). purego.SyscallN allocates its
// variadic array when given literal arguments; Lib.call must not.
func TestCallDoesNotAllocate(t *testing.T) {
	l := loadForTest(t)
	// Session 0 does not exist, so librime returns False without side effects.
	if n := testing.AllocsPerRun(1000, func() { l.call(l.api.processKey, 0, 'a', 0) }); n != 0 {
		t.Fatalf("Lib.call allocates %.2f times per call", n)
	}
}
