package winsetup

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// RemoveStartup removes only this installation's unmodified logon command.
// A custom command or another installation remains owned by its author.
func RemoveStartup(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	value, _, err := k.GetStringValue("Shixin")
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	argv, err := windows.DecomposeCommandLine(value)
	if err != nil || len(argv) != 2 || argv[1] != "-startup" || !strings.EqualFold(filepath.Clean(argv[0]), filepath.Join(dir, "Shixin.exe")) {
		return nil
	}
	return k.DeleteValue("Shixin")
}
