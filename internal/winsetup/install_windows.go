package winsetup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ErrCancelled is the normal UAC/installer cancellation result.
var ErrCancelled = errors.New("setup: installation cancelled")

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Shixin`

// InstallDir is the fixed protected machine directory, independent of the
// invoking user's environment and of the bootstrapper's download location.
func InstallDir() (string, error) {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "Shixin"), nil
}

// ProductCode reports the MSI owning this installation. An absent product
// is distinct from a failed registry read.
func ProductCode() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstallKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer k.Close()
	code, _, err := k.GetStringValue("ProductCode")
	if err != nil {
		return "", err
	}
	if _, err = windows.GUIDFromString(code); err != nil {
		return "", err
	}
	return code, nil
}

// RunMSI uses Windows Installer's transaction, elevation and reboot handling.
// It never restarts the computer or forcibly closes applications.
func RunMSI(path string, remove bool, logPath string, before func() error) (bool, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return false, err
	}
	action := "/i"
	if remove {
		action = "/x"
	}
	cmd := exec.Command(filepath.Join(system, "msiexec.exe"), action, path, "/passive", "/norestart", "REBOOT=ReallySuppress", "/l*v", logPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var gate *approval
	if before != nil {
		gate, err = newApproval()
		if err != nil {
			return false, err
		}
		defer gate.close()
		cmd.Args = append(cmd.Args, "SHIXIN_GATE="+gate.goName, "SHIXIN_READY="+gate.readyName)
	}
	if err = cmd.Start(); err != nil {
		return false, err
	}
	var beforeErr error
	if gate != nil {
		child, openErr := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
		if openErr != nil {
			cmd.Wait()
			return false, openErr
		}
		result, waitErr := windows.WaitForMultipleObjects([]windows.Handle{gate.ready, child}, false, windows.INFINITE)
		windows.CloseHandle(child)
		if waitErr != nil {
			beforeErr = waitErr
		} else if result == windows.WAIT_OBJECT_0 {
			beforeErr = before()
			if beforeErr == nil {
				beforeErr = windows.SetEvent(gate.goEvent)
			}
		}
	}
	err = cmd.Wait()
	if beforeErr != nil {
		return false, errors.Join(beforeErr, err)
	}
	if err == nil {
		return false, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 3010:
			return true, nil
		case 1602:
			return false, ErrCancelled
		}
	}
	return false, fmt.Errorf("setup: Windows Installer failed (%w); log: %s", err, logPath)
}

// Front runs a cold user operation with no console. It must finish before
// installation proceeds; no PID from a lock file is ever terminated.
func Front(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "Shixin.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd.Run()
}

// StartHost launches the installed host as the original caller.
func StartHost(dir string) error { return start(dir, "Shixin.exe", "-startup") }

// StartSettings opens the first-run guide or returns to About after an upgrade.
func StartSettings(dir string, first bool) error {
	if first {
		return start(dir, "shixin-settings.exe", "-welcome")
	}
	return start(dir, "shixin-settings.exe", "-page", "about")
}

func start(dir, name string, args ...string) error {
	cmd := exec.Command(filepath.Join(dir, name), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// CheckUnmanaged refuses to adopt unknown files in the protected product
// directory. This also prevents reinstall over files awaiting uninstall at
// restart; user data lives elsewhere and is never removed here.
func CheckUnmanaged(dir string) error {
	code, err := ProductCode()
	if err != nil {
		return err
	}
	if code != "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("setup: an unmanaged installation remains; uninstall it or restart Windows before installing")
	}
	return nil
}
