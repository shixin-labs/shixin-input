package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"shixin/internal/ipc"
)

// openRecord opens the record the way an older reader would: shared for
// read and write but not for delete.
func openRecord(t *testing.T, path string, share uint32) windows.Handle {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, share, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// A reader that shares for delete (ipc.ReadEndpoint) never blocks publish,
// and sees the old or the new record.
func TestWindowsPublishWhileRecordOpen(t *testing.T) {
	dir := t.TempDir()
	public := ipc.EnginePath(dir)
	first, second := filepath.Join(dir, "g-first"), filepath.Join(dir, "g-second")
	if err := publish(public, first); err != nil {
		t.Fatal(err)
	}
	h := openRecord(t, public, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
	defer windows.CloseHandle(h)
	if err := publish(public, second); err != nil {
		t.Fatalf("publish with a delete-sharing reader open: %v", err)
	}
	b, err := ipc.ReadEndpoint(public)
	if err != nil || string(b) != second {
		t.Fatalf("read %q, %v; want %q", b, err, second)
	}

	// Concurrent readers and publishers.
	done := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			b, err := ipc.ReadEndpoint(public)
			if err != nil {
				done <- err
				return
			}
			if s := string(b); s != first && s != second {
				done <- os.ErrInvalid
				return
			}
		}
	}()
	for i := 0; i < 200; i++ {
		target := first
		if i%2 == 1 {
			target = second
		}
		if err := publish(public, target); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatalf("concurrent reader: %v", err)
	}
}

// A reader without delete sharing makes the rename fail; publish waits for
// it to finish instead of failing the generation swap.
func TestWindowsPublishRetriesSharingViolation(t *testing.T) {
	dir := t.TempDir()
	public := ipc.EnginePath(dir)
	first, second := filepath.Join(dir, "g-first"), filepath.Join(dir, "g-second")
	if err := publish(public, first); err != nil {
		t.Fatal(err)
	}
	h := openRecord(t, public, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	time.AfterFunc(200*time.Millisecond, func() { windows.CloseHandle(h) })
	start := time.Now()
	if err := publish(public, second); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Log("rename did not collide with the open reader")
	}
	b, err := ipc.ReadEndpoint(public)
	if err != nil || string(b) != second {
		t.Fatalf("read %q, %v; want %q", b, err, second)
	}
}

// A reader that never lets go fails publish after the bounded wait.
func TestWindowsPublishGivesUp(t *testing.T) {
	dir := t.TempDir()
	public := ipc.EnginePath(dir)
	if err := publish(public, filepath.Join(dir, "g-first")); err != nil {
		t.Fatal(err)
	}
	h := openRecord(t, public, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	defer windows.CloseHandle(h)
	start := time.Now()
	if err := publish(public, filepath.Join(dir, "g-second")); err == nil {
		t.Fatal("publish succeeded over a held record")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("publish waited %v", d)
	}
	if _, err := os.Stat(public + ".next"); !os.IsNotExist(err) {
		t.Fatalf("temp record left behind: %v", err)
	}
}

func TestWindowsPublishReplacement(t *testing.T) {
	dir := t.TempDir()
	public := ipc.EnginePath(dir)
	// Keep both private listeners alive while replacing discovery. This
	// models hot swap and needs neither symlinks nor registration privileges.
	for _, name := range []string{"g-first", "g-second"} {
		target := filepath.Join(dir, name)
		l, err := ipc.Listen(target)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		if err := publish(public, target); err != nil {
			t.Fatal(err)
		}
		type result struct {
			c   net.Conn
			err error
		}
		accepted := make(chan result, 1)
		go func() { c, err := ipc.Accept(l); accepted <- result{c, err} }()
		c, err := ipc.Dial(public, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
		select {
		case r := <-accepted:
			if r.err != nil {
				t.Fatal(r.err)
			}
			r.c.Close()
		case <-time.After(3 * time.Second):
			t.Fatal("published worker was not reachable")
		}
	}
}
