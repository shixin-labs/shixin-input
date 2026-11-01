package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var errNotAdded = errors.New("Shixin is not enabled for this user")

// windowsAdded queries the current caller, never the account used for UAC.
// It neither starts the host nor enables or selects a keyboard.
func windowsAdded() (bool, error) {
	command, _ := syscall.UTF16PtrFromString("user-query")
	result, err := registrationResult("DllInstall", 0, uintptr(unsafe.Pointer(command)))
	runtime.KeepAlive(command)
	return result == 0 && err == nil, err
}

func windowsStartup(remove bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		if remove {
			return nil
		}
		key, _, err = registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE|registry.SET_VALUE)
	}
	if err != nil {
		return fmt.Errorf("Windows logon startup: %w", err)
	}
	defer key.Close()
	return startupValue(key, exe, remove)
}

// startupValue only removes this copy's command. A newer installation or
// a user's replacement command must survive an old uninstaller's cleanup.
func startupValue(key registry.Key, exe string, remove bool) error {
	command := syscall.EscapeArg(exe) + " -startup"
	if !remove {
		return key.SetStringValue("Shixin", command)
	}
	old, _, err := key.GetStringValue("Shixin")
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	args, parseErr := windows.DecomposeCommandLine(old)
	if parseErr != nil || len(args) != 2 || args[1] != "-startup" ||
		!strings.EqualFold(filepath.Clean(args[0]), filepath.Clean(exe)) {
		return nil
	}
	return key.DeleteValue("Shixin")
}
