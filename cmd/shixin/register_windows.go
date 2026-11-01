package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The elevated helper uses exit 73 after TIP removal if COM cleanup fails.
// The caller must not re-enable a keyboard whose machine profile is gone.
var errMachineRemoved = errors.New("Windows text service was removed but COM cleanup failed")

const (
	unregisterCleanupHRESULT  uint32 = 0x8004a001
	unregisterCleanupExitCode        = 73 // distinct from Go runtime/flag-parser exit 2
)

// Registration is deliberately split: Windows owns the machine TIP, while
// the invoking user owns the enabled keyboard list. UAC may use a different
// administrator account, so the elevated child must never enable its profile.
func registerWindows(remove bool) error {
	enabled := false
	if remove {
		query, _ := windows.UTF16PtrFromString("user-query")
		hr, err := registrationResult("DllInstall", 0, uintptr(unsafe.Pointer(query)))
		runtime.KeepAlive(query)
		if err != nil {
			return err
		}
		enabled = hr == 0 // S_OK enabled, S_FALSE disabled
	}
	return registrationSteps(remove, enabled, registerWindowsSystemElevated, installWindowsUser, cleanupWindowsUser)
}

func registrationSteps(remove, enabled bool, system func(bool, func() error) error, user func(bool) error, cleanup func() error) error {
	if remove {
		// Windows can report success yet leave a stale Settings entry if the
		// machine TIP disappears before user disablement. Only disable an
		// enabled profile; rollback must never add a previously absent keyboard.
		disabled := false
		err := system(true, func() error {
			if enabled {
				if err := user(true); err != nil {
					return err
				}
				disabled = true
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errMachineRemoved) {
				return errors.Join(err, cleanup())
			}
			if disabled {
				return errors.Join(err, user(false))
			}
			return err
		}
		return cleanup()
	}
	if err := system(false, nil); err != nil {
		return err
	}
	return user(false)
}

func registrationExport(name string, args ...uintptr) error {
	_, err := registrationResult(name, args...)
	return err
}

func registrationResult(name string, args ...uintptr) (uint32, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	h, err := windows.LoadLibraryEx(filepath.Join(filepath.Dir(exe), "shixin_tsf.dll"), 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return 0, err
	}
	defer windows.FreeLibrary(h)
	proc, err := windows.GetProcAddress(h, name)
	if err != nil {
		return 0, err
	}
	result, _, callErr := syscall.SyscallN(proc, args...)
	if int32(result) < 0 {
		if name == "DllUnregisterServer" && uint32(result) == unregisterCleanupHRESULT {
			return uint32(result), fmt.Errorf("%w: %v", errMachineRemoved, callErr)
		}
		return uint32(result), fmt.Errorf("%s failed: HRESULT 0x%08x", name, uint32(result))
	}
	return uint32(result), nil
}

func registerWindowsSystem(remove bool) error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return fmt.Errorf("Windows requires administrator approval to register the machine text service")
	}
	name := "DllRegisterServer"
	if remove {
		name = "DllUnregisterServer"
	}
	return registrationExport(name)
}

func installWindowsUser(remove bool) error {
	install := uintptr(1)
	if remove {
		install = 0
	}
	user, _ := windows.UTF16PtrFromString("user")
	err := registrationExport("DllInstall", install, uintptr(unsafe.Pointer(user)))
	runtime.KeepAlive(user)
	return err
}

func cleanupWindowsUser() error {
	command, _ := windows.UTF16PtrFromString("user-cleanup")
	err := registrationExport("DllInstall", 0, uintptr(unsafe.Pointer(command)))
	runtime.KeepAlive(command)
	return err
}

// SHELLEXECUTEINFOW, including its pointer-sized union. ShellExecuteEx gives
// us a process handle so a rejected UAC prompt or child failure is reported
// before user enablement (or after a rollback for failed unregistration).
type shellExecuteInfo struct {
	size, mask    uint32
	window        windows.Handle
	verb, file    *uint16
	parameters    *uint16
	directory     *uint16
	show          int32
	instance      windows.Handle
	idList        uintptr
	class         *uint16
	classKey      windows.Handle
	hotKey        uint32
	icon, process windows.Handle
}

func registerWindowsSystemElevated(remove bool, before func() error) error {
	if windows.GetCurrentProcessToken().IsElevated() {
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		return registerWindowsSystem(remove)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	args := "-register -system-registration"
	if remove {
		args = "-unregister -system-registration"
	}
	var gate windows.Handle
	if before != nil {
		// UAC must finish before user disablement. The elevated child waits
		// on this unpredictable, user/administrator-only event until the
		// original caller has removed its keyboard while the TIP still exists.
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return err
		}
		sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;BA)")
		if err != nil {
			return err
		}
		sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
		name := `Local\Shixin.Registration.` + rand.Text()
		p, _ := windows.UTF16PtrFromString(name)
		gate, err = windows.CreateEvent(&sa, 1, 0, p)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(gate)
		args += " -registration-gate " + name
	}
	parameters, _ := windows.UTF16PtrFromString(args)
	info := shellExecuteInfo{mask: 0x40 | 0x100 | 0x400, verb: verb, file: file, parameters: parameters, show: 0} // NOCLOSEPROCESS, NOASYNC, FLAG_NO_UI
	info.size = uint32(unsafe.Sizeof(info))
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	ok, _, callErr := proc.Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(info)
	if ok == 0 {
		return fmt.Errorf("Windows text service administrator approval: %w", callErr)
	}
	if info.process == 0 {
		return fmt.Errorf("Windows text service registration returned no process")
	}
	defer windows.CloseHandle(info.process)
	if before != nil {
		if err := before(); err != nil {
			windows.TerminateProcess(info.process, 1) // child has not passed the gate
			return err
		}
		if err := windows.SetEvent(gate); err != nil {
			windows.TerminateProcess(info.process, 1)
			return err
		}
	}
	if _, err := windows.WaitForSingleObject(info.process, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.process, &code); err != nil {
		return err
	}
	if code != 0 {
		if code == unregisterCleanupExitCode {
			return fmt.Errorf("%w; see shixin.log in the administrator account", errMachineRemoved)
		}
		return fmt.Errorf("Windows machine text service registration failed (exit %d); see shixin.log in the administrator account", code)
	}
	return nil
}

func waitRegistrationGate(name string) error {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	gate, err := windows.OpenEvent(windows.SYNCHRONIZE, false, p)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(gate)
	state, err := windows.WaitForSingleObject(gate, 30000)
	if err != nil {
		return err
	}
	if state != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("registration caller did not complete user disablement")
	}
	return nil
}
