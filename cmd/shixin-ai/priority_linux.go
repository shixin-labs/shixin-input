package main

import "golang.org/x/sys/unix"

// setInteractivePriority raises the calling thread's priority where
// permitted (DESIGN R7).
func setInteractivePriority() {
	_ = unix.Setpriority(unix.PRIO_PROCESS, unix.Gettid(), -5)
}
