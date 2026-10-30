package engine

import "golang.org/x/sys/unix"

// setInteractivePriority raises the calling thread's priority where
// permitted (DESIGN R7). Failure is fine: unprivileged users can't go below
// nice 0.
func setInteractivePriority() {
	_ = unix.Setpriority(unix.PRIO_PROCESS, unix.Gettid(), -5)
}
