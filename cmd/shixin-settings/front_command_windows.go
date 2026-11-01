package main

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

func frontCommand(path string, args ...string) *exec.Cmd {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}
