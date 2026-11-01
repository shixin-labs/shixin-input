package ipc

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pipeFixture(t *testing.T, path string) (net.Listener, net.Conn, net.Conn) {
	t.Helper()
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	type accepted struct {
		c   net.Conn
		err error
	}
	ch := make(chan accepted, 1)
	go func() { c, err := Accept(l); ch <- accepted{c, err} }()
	c, err := Dial(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	select {
	case a := <-ch:
		if a.err != nil {
			t.Fatal(a.err)
		}
		t.Cleanup(func() { a.c.Close() })
		return l, c, a.c
	case <-time.After(3 * time.Second):
		t.Fatal("accept timeout")
	}
	return nil, nil, nil
}

func TestNamedPipeFraming(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.sock")
	_, c, server := pipeFixture(t, path)
	c.SetDeadline(time.Now().Add(3 * time.Second))
	server.SetDeadline(time.Now().Add(3 * time.Second))
	key := (&Key{Ctx: 2, Seq: 9, Keysym: 'a'}).Append(nil)
	if _, err := c.Write(key); err != nil {
		t.Fatal(err)
	}
	typ, b, err := NewReader(server, 256).Next()
	var k Key
	if err != nil || typ != TypeKey || k.Decode(b) != nil || k.Seq != 9 {
		t.Fatalf("key: %v %v %+v", typ, err, k)
	}
	if _, err := server.Write((&Result{Ctx: k.Ctx, Seq: k.Seq, Commit: []byte("你好"), Flags: ResultHandled}).Append(nil)); err != nil {
		t.Fatal(err)
	}
	typ, b, err = NewReader(c, 256).Next()
	var r Result
	if err != nil || typ != TypeResult || r.Decode(b) != nil || string(r.Commit) != "你好" {
		t.Fatalf("result: %v %v %+v", typ, err, r)
	}
}

func TestNamedPipeOwnershipAndDeadlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.sock")
	l, c, server := pipeFixture(t, path)
	if other, err := Listen(path); err == nil {
		other.Close()
		t.Fatal("duplicate listener stole the pipe")
	}
	c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	var b [1]byte
	if _, err := c.Read(b[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	server.Close()
	if _, err := c.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("close: %v", err)
	}
	l.Close()
	if _, err := Accept(l); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed listener: %v", err)
	}
}

func TestNamedPipeDiscovery(t *testing.T) {
	dir := t.TempDir()
	private := filepath.Join(dir, "g-worker")
	l, _, _ := pipeFixture(t, private)
	public := EnginePath(dir)
	if err := os.WriteFile(public, []byte(private), 0o600); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan error, 1)
	go func() {
		c, err := Accept(l)
		if c != nil {
			c.Close()
		}
		accepted <- err
	}()
	c, err := Dial(public, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("published endpoint accept timeout")
	}
	for _, target := range []string{"g-relative", filepath.Join(dir, "wrong"), filepath.Join(filepath.Dir(dir), "g-other")} {
		if err := os.WriteFile(public, []byte(target), 0o600); err != nil {
			t.Fatal(err)
		}
		if c, err := Dial(public, 20*time.Millisecond); err == nil {
			c.Close()
			t.Fatalf("accepted invalid record %q", target)
		}
	}
}

func TestNamedPipeHalfClose(t *testing.T) {
	_, c, server := pipeFixture(t, filepath.Join(t.TempDir(), "e.sock"))
	c.SetDeadline(time.Now().Add(3 * time.Second))
	server.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := server.Read(b[:])
		if !errors.Is(err, io.EOF) {
			done <- err
			return
		}
		_, err = server.Write([]byte("final reply"))
		done <- err
	}()
	half, ok := c.(interface{ CloseWrite() error })
	if !ok {
		t.Fatal("pipe does not support draining migration")
	}
	if err := half.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	var reply [11]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		t.Fatal(err)
	}
	if string(reply[:]) != "final reply" {
		t.Fatalf("reply = %q", reply)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestNamedPipeDoesNotAllocate: the engine's connection to shixin-ai reads
// and writes on the engine thread's budget. The echo makes every client
// Read wait for its completion, and the sizes cover a Write the pipe takes
// at once.
func TestNamedPipeDoesNotAllocate(t *testing.T) {
	_, c, server := pipeFixture(t, filepath.Join(t.TempDir(), "e.sock"))
	go func() {
		var b [256]byte
		for {
			n, err := server.Read(b[:])
			if err != nil {
				return
			}
			if _, err := server.Write(b[:n]); err != nil {
				return
			}
		}
	}()
	msg := []byte("0123456789abcdef0123456789abcdef")
	var back [256]byte
	round := func() {
		if _, err := c.Write(msg); err != nil {
			t.Fatal(err)
		}
		if n, err := c.Read(back[:]); err != nil || n != len(msg) {
			t.Fatalf("read %d: %v", n, err)
		}
	}
	round()
	if a := testing.AllocsPerRun(200, round); a != 0 {
		t.Errorf("%v allocations per write and read", a)
	}
	// A deadline that never passes, set around every call as the host does.
	if a := testing.AllocsPerRun(200, func() {
		c.SetWriteDeadline(time.Now().Add(time.Minute))
		c.SetReadDeadline(time.Now().Add(time.Minute))
		round()
	}); a != 0 {
		t.Errorf("%v allocations per write and read with deadlines", a)
	}
	// And a deadline that does pass cancels the read.
	c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, err := c.Read(back[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}

func TestWindowsRuntimeDir(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir, err := RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	again, err := RuntimeDir()
	if err != nil || dir != again {
		t.Fatalf("runtime dir changed: %q %q %v", dir, again, err)
	}
	name, err := pipeName(EnginePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	other, err := pipeName(filepath.Join(dir, "g-worker"))
	if err != nil || name == other {
		t.Fatalf("endpoints collide: %v", err)
	}
}
