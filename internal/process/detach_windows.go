package process

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Detach lets a helper outlive its UI parent without opening a console.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW, HideWindow: true}
}
