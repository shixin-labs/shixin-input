package ipc

import (
	"os"
	"runtime"
)

// TempDir creates a scratch directory for IPC tools and tests. Unix socket
// paths need the short /tmp prefix rather than macOS's long TMPDIR.
// The caller must remove it after closing all connections.
func TempDir(prefix string) (string, error) {
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	return os.MkdirTemp(base, prefix)
}
