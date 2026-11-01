package logx

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestLogger(t *testing.T) {
	var b bytes.Buffer
	l := New(&b)
	l.Printf("hello %d", 1)
	l.Close()
	if !strings.HasSuffix(b.String(), " hello 1\n") {
		t.Fatalf("%q", b.String())
	}
	var nilLogger *Logger
	nilLogger.Printf("ignored")
}

// block is a writer that never returns until released.
type block chan struct{}

func (b block) Write(p []byte) (int, error) { <-b; return len(p), nil }

func TestNeverBlocks(t *testing.T) {
	w := make(block)
	l := New(w)
	for i := 0; i < 5000; i++ {
		l.Printf("line %d", i)
	}
	if l.Dropped() == 0 {
		t.Fatal("expected drops with a stuck writer")
	}
	close(w)
	l.Close()
}

func TestPlatformLogDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("LOCALAPPDATA", root)
	if err := os.Mkdir(filepath.Join(root, "Library"), 0o700); err != nil {
		t.Fatal(err)
	}
	var want string
	switch runtime.GOOS {
	case "windows":
		want = filepath.Join(root, "Shixin", "Logs")
	case "darwin":
		want = filepath.Join(root, "Library", "Logs", "Shixin")
	}
	if got := Dir(); got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
	path := "-"
	if want != "" {
		path = filepath.Join(want, "engine.log")
	}
	if got := DefaultPath("engine.log"); got != path {
		t.Fatalf("DefaultPath = %q, want %q", got, path)
	}
}

func TestCloseDuringBackgroundLogging(t *testing.T) {
	var b bytes.Buffer
	l := New(&b)
	l.Printf("before close")
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 1000 {
				l.Printf("background diagnostic")
			}
		})
	}
	l.Close()
	workers.Wait()
	l.Close()
	l.Printf("after close")
	if !strings.Contains(b.String(), "before close") || strings.Contains(b.String(), "after close") {
		t.Fatalf("shutdown logs: %q", b.String())
	}
}
