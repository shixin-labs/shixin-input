//go:build darwin || linux

// Package process holds platform-specific child process setup.
package process

import (
	"os/exec"
	"syscall"
)

// Detach lets a helper outlive its UI parent.
func Detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
