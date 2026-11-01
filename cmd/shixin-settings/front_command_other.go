//go:build !windows

package main

import "os/exec"

func frontCommand(path string, args ...string) *exec.Cmd { return exec.Command(path, args...) }
