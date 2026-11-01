// Package logx is a logger that never blocks its caller (DESIGN §3.4):
// lines go into a bounded channel and a background goroutine writes them.
// When the channel is full, lines are dropped and counted.
//
// Never log typed text or candidates (AGENTS.md, privacy).
package logx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Dir returns the platform log directory: ~/Library/Logs/Shixin on macOS,
// %LOCALAPPDATA%\Shixin\Logs on Windows, or "" on other platforms where
// stderr goes to the service manager's journal.
func Dir() string {
	if runtime.GOOS == "windows" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return ""
		}
		return filepath.Join(cache, "Shixin", "Logs")
	}
	if runtime.GOOS != "darwin" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(home, "Library")); err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Logs", "Shixin")
}

// DefaultPath returns the log file name in Dir, or "-" (stderr) without
// a log directory.
func DefaultPath(name string) string {
	if dir := Dir(); dir != "" {
		return filepath.Join(dir, name)
	}
	return "-"
}

// Logger writes timestamped lines in the background.
type Logger struct {
	ch      chan string
	dropped atomic.Uint64
	done    chan struct{}
	stop    chan struct{}
	closed  atomic.Bool
	once    sync.Once
}

// New starts a logger writing to w.
func New(w io.Writer) *Logger {
	l := &Logger{ch: make(chan string, 1024), done: make(chan struct{}), stop: make(chan struct{})}
	go func() {
		defer close(l.done)
		for {
			select {
			case s := <-l.ch:
				io.WriteString(w, s)
			case <-l.stop:
				for {
					select {
					case s := <-l.ch:
						io.WriteString(w, s)
					default:
						return
					}
				}
			}
		}
	}()
	return l
}

// Open starts a logger appending to path, creating its directory. Files
// over 4 MiB are truncated first.
func Open(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() > 4<<20 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, err
	}
	return New(f), nil
}

// Printf logs a line. It formats on the caller's goroutine, so keep it off
// the key path.
func (l *Logger) Printf(format string, args ...any) {
	if l == nil {
		return
	}
	if l.closed.Load() {
		l.dropped.Add(1)
		return
	}
	s := time.Now().Format("2006-01-02 15:04:05.000 ") + fmt.Sprintf(format, args...) + "\n"
	select {
	case l.ch <- s:
	default:
		l.dropped.Add(1)
	}
}

// Dropped returns the number of lines dropped because the queue was full.
func (l *Logger) Dropped() uint64 { return l.dropped.Load() }

// Close flushes queued lines and stops the writer. It is safe to call more
// than once. Concurrent or later logging may be discarded, but never panics.
func (l *Logger) Close() {
	l.once.Do(func() {
		l.closed.Store(true)
		close(l.stop)
	})
	<-l.done
}
