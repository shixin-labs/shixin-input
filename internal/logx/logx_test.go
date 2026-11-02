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

func TestOpenAppendsAndCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Logs", "Shixin", "engine.log")
	for _, line := range []string{"first", "second"} {
		l, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		l.Printf("%s", line)
		l.Close()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"); len(lines) != 2 ||
		!strings.HasSuffix(lines[0], " first") || !strings.HasSuffix(lines[1], " second") {
		t.Fatalf("log: %q", b)
	}
}

func TestOpenTruncatesLargeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.log")
	for _, c := range []struct {
		size int
		kept bool
	}{{4 << 20, true}, {4<<20 + 1, false}} {
		if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, c.size), 0o644); err != nil {
			t.Fatal(err)
		}
		l, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		l.Printf("after")
		l.Close()
		b, _ := os.ReadFile(path)
		if kept := len(b) > c.size; kept != c.kept || !strings.HasSuffix(string(b), " after\n") {
			t.Errorf("%d bytes: now %d bytes, kept %v", c.size, len(b), kept)
		}
	}
}

func TestOpenFailsWithoutDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, "engine.log")); err == nil {
		t.Fatal("opened a log under a regular file")
	}
}

// closeCounter records Close calls on the writer.
type closeCounter struct {
	bytes.Buffer
	closes int
}

func (c *closeCounter) Close() error { c.closes++; return nil }

// Open's logger owns its file; New's writer stays the caller's.
func TestCloseOwnership(t *testing.T) {
	w := &closeCounter{}
	l := New(w)
	l.Close()
	if w.closes != 0 {
		t.Fatal("New closed the caller's writer")
	}
	l = start(w, w)
	l.Printf("x")
	l.Close()
	l.Close()
	if w.closes != 1 || !strings.HasSuffix(w.String(), " x\n") {
		t.Fatalf("closed %d times after %q", w.closes, w.String())
	}
	path := filepath.Join(t.TempDir(), "engine.log")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	// The file is closed: removing it works even where open files can't be
	// removed (Windows).
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
