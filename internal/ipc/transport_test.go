//go:build darwin || linux

package ipc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shixin-ipc-") // short path for the 104-byte limit
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "e.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil { // stale file
		t.Fatal(err)
	}
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := Accept(l)
		if err != nil {
			return
		}
		defer c.Close()
		r := NewReader(c, 256)
		typ, payload, err := r.Next()
		var k Key
		if err != nil || typ != TypeKey || k.Decode(payload) != nil {
			return
		}
		res := Result{Seq: k.Seq, Flags: ResultHandled}
		c.Write(res.Append(nil))
	}()
	c, err := Dial(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write((&Key{Seq: 9, Keysym: 'a'}).Append(nil)); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	typ, payload, err := NewReader(c, 256).Next()
	var res Result
	if err != nil || typ != TypeResult || res.Decode(payload) != nil || res.Seq != 9 {
		t.Fatalf("reply: %v %v %+v", typ, err, res)
	}
}

func TestRuntimeDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	dir, err := RuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RuntimeDir(); err == nil {
		t.Fatal("accepted a 0755 runtime dir")
	}
}
