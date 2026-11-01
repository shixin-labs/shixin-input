package winsetup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WaitProcesses waits for Shixin's own executables, across sessions when
// called by MSI as SYSTEM. Applications merely containing the TSF DLL are
// deliberately excluded. It never terminates a process or trusts a PID file.
func WaitProcesses(dir string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		running, err := runningApplication(dir)
		if err != nil {
			return err
		}
		if running == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("setup: close Shixin in all user sessions before continuing (%s is running)", running)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func runningApplication(dir string) (string, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		if entry.ProcessID == uint32(os.Getpid()) {
			continue
		}
		switch name {
		case "shixin.exe", "shixin-settings.exe", "shixin-engine.exe", "shixin-builder.exe", "shixin-ai.exe":
		default:
			continue
		}
		process, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if openErr != nil {
			continue
		} // exited, or an inaccessible unrelated process
		var image [32768]uint16
		n := uint32(len(image))
		queryErr := windows.QueryFullProcessImageName(process, 0, &image[0], &n)
		windows.CloseHandle(process)
		if queryErr == nil && strings.EqualFold(filepath.Dir(windows.UTF16ToString(image[:n])), dir) {
			return name, nil
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return "", err
	}
	return "", nil
}
