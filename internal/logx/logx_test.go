package logx

import (
	"bytes"
	"strings"
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
