//go:build darwin || linux

package ipc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// RuntimeDir returns Shixin's per-user directory for sockets and locks,
// creating it with mode 0700. On macOS it lives in the per-user temporary
// directory, which keeps socket paths well under the 104-byte limit; on
// Linux in $XDG_RUNTIME_DIR.
func RuntimeDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "shixin-"+strconv.Itoa(os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("ipc: runtime dir: %w", err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("ipc: runtime dir: %w", err)
	}
	if !fi.IsDir() || fi.Mode().Perm() != 0o700 || !ownedByMe(fi) {
		return "", fmt.Errorf("ipc: runtime dir %s must be a 0700 directory owned by this user", dir)
	}
	return dir, nil
}

// EnginePath is the default engine socket in dir.
func EnginePath(dir string) string { return filepath.Join(dir, "engine.sock") }

// Listen listens on the Unix socket at path, replacing a stale socket file.
// The caller must hold the engine lock, which makes the file stale by
// definition if it exists.
func Listen(path string) (*net.UnixListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("ipc: remove stale socket: %w", err)
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("ipc: listen: %w", err)
	}
	l.SetUnlinkOnClose(true)
	return l, nil
}

// Accept accepts a connection from a process of the same user and closes
// connections from anyone else.
func Accept(l net.Listener) (net.Conn, error) {
	for {
		c, err := l.Accept()
		if err != nil {
			return nil, err
		}
		uc, ok := c.(*net.UnixConn)
		if !ok {
			c.Close()
			return nil, errors.New("ipc: listener did not return a Unix connection")
		}
		uid, err := peerUID(uc)
		if err == nil && uid == os.Getuid() {
			return c, nil
		}
		c.Close()
	}
}

// Dial connects to the engine socket at path. Clients call it off the key
// path (DESIGN §3.3: never connect on the key path).
func Dial(path string, timeout time.Duration) (*net.UnixConn, error) {
	d := net.Dialer{Timeout: timeout}
	c, err := d.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return c.(*net.UnixConn), nil
}
