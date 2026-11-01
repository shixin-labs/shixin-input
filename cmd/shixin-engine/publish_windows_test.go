package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"shixin/internal/ipc"
)

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
