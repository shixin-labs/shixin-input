package main

import (
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"shixin/internal/engine"
	"shixin/internal/settings"
)

// staleEngine reports whether the running engine coordinator started before
// the installed engine binary was put in place. An upgrade swaps the
// bundle's Contents, giving its files fresh change times; if the old
// coordinator survived (macOS relaunched the front before the installer
// could pass -after-upgrade, which 0.1.3 and older relied on), it keeps
// running old code next to the new front.
func staleEngine() bool {
	user, err := settings.RimeUserDir()
	if err != nil {
		return false
	}
	pid := engine.RunningPID(filepath.Join(user, "supervisor"))
	if pid == 0 {
		return false
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return false
	}
	started := time.Unix(kp.Proc.P_starttime.Sec, int64(kp.Proc.P_starttime.Usec)*1000)
	var st unix.Stat_t
	if err := unix.Stat(enginePath(), &st); err != nil {
		return false
	}
	return predates(started, time.Unix(st.Ctim.Sec, st.Ctim.Nsec))
}
