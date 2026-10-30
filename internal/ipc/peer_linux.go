package ipc

import (
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func peerUID(c *net.UnixConn) (int, error) {
	rc, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	uid := -1
	var serr error
	err = rc.Control(func(fd uintptr) {
		var u *unix.Ucred
		u, serr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if serr == nil {
			uid = int(u.Uid)
		}
	})
	if err != nil {
		return 0, err
	}
	return uid, serr
}

func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
