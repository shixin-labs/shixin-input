// Command shixin-setup installs the MSI embedded in its executable. Signed
// releases verify Authenticode; explicitly compiled previews warn and use
// manual downloads instead of installing updates from inside the app.
// The payloadless copy installed beside Shixin is its uninstaller.
package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"shixin/internal/i18n"
	"shixin/internal/winsetup"
)

var version = "dev"

func message(text string, kind uint32) bool {
	title, _ := windows.UTF16PtrFromString(i18n.T("setup.title"))
	body, _ := windows.UTF16PtrFromString(text)
	ret, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), uintptr(kind|0x10000))
	return ret == 1
}

func main() {
	remove := flag.Bool("uninstall", false, "uninstall Shixin, preserving settings and learned words")
	verify := flag.Bool("verify", false, "check this release without installing (preview: integrity only)")
	ready := flag.Uint64("handoff-ready", 0, "internal update handoff event")
	parent := flag.Uint64("handoff-parent", 0, "internal wait-only updater process handle")
	checkStopped := flag.Bool("check-stopped", false, "internal MSI process drain check")
	flag.Parse()
	if *checkStopped {
		exe, err := os.Executable()
		if err == nil {
			var f *os.File
			f, err = winsetup.LockedFile(exe)
			if err == nil {
				_, err = winsetup.SetupPublisher(f)
				f.Close()
			}
		}
		if err == nil {
			var dir string
			dir, err = winsetup.InstallDir()
			if err == nil {
				err = winsetup.CheckUnmanaged(dir)
				if err == nil {
					err = winsetup.CheckPreviewTarget(dir)
					if err == nil {
						err = winsetup.WaitProcesses(dir, time.Second)
					}
				}
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if (*ready == 0) != (*parent == 0) || (*ready != 0 && (*remove || *verify)) {
		fmt.Fprintln(os.Stderr, "invalid update handoff")
		os.Exit(1)
	}
	if winsetup.UnsignedPreview && *ready != 0 {
		fmt.Fprintln(os.Stderr, "unsigned previews require manual installation")
		os.Exit(1)
	}
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected setup arguments")
		os.Exit(1)
	}
	if err := run(*remove, *verify, windows.Handle(*ready), windows.Handle(*parent)); err != nil {
		if err == winsetup.ErrCancelled {
			os.Exit(1602)
		}
		fmt.Fprintln(os.Stderr, err)
		if !*verify {
			message(fmt.Sprintf(i18n.T("setup.failed"), err), 0x10)
		}
		os.Exit(1)
	}
}

func run(remove, verify bool, ready, parent windows.Handle) (err error) {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	self, err := winsetup.LockedFile(exe)
	if err != nil {
		return err
	}
	defer self.Close()
	publisher, err := winsetup.SetupPublisher(self)
	if err != nil {
		return err
	}
	dir, err := winsetup.InstallDir()
	if err != nil {
		return err
	}
	code, err := winsetup.ProductCode()
	if err != nil {
		return err
	}
	var bundle winsetup.Bundle
	if !remove {
		st, err := self.Stat()
		if err != nil {
			return err
		}
		bundle, err = winsetup.Read(self, st.Size())
		if err != nil {
			return err
		}
		if bundle.Version != version || bundle.Arch != runtime.GOARCH {
			return errors.New("setup: release metadata does not match this executable")
		}
		// Reject an emulated x64 bootstrapper on ARM64: that would install a
		// DLL unusable by native applications. ARM64 includes both backends.
		var machine, emulation uint16
		if err := windows.IsWow64Process2(windows.CurrentProcess(), &emulation, &machine); err != nil {
			return err
		}
		if (machine == 0xaa64) != (bundle.Arch == "arm64") {
			return errors.New("setup: choose the installer for this Windows architecture")
		}
	}
	if runtime.GOARCH == "arm64" && windows.RtlGetVersion().BuildNumber < 22000 {
		return errors.New("setup: ARM64 requires Windows 11 or later")
	}
	if windows.RtlGetVersion().MajorVersion < 10 {
		return errors.New("setup: Windows 10 or later is required")
	}
	if verify {
		label := ""
		if winsetup.UnsignedPreview {
			label = " unsigned-preview (integrity only)"
		}
		fmt.Fprintf(os.Stdout, "%s %s%s\n", bundle.Version, bundle.Arch, label)
		return nil
	}
	if err := winsetup.CheckPreviewTarget(dir); err != nil {
		return err
	}
	if !remove && code == "" {
		if err := winsetup.CheckUnmanaged(dir); err != nil {
			return err
		}
	}
	mutexName, _ := windows.UTF16PtrFromString(`Local\Shixin.Setup`)
	mutex, mutexErr := windows.CreateMutex(nil, false, mutexName)
	if mutexErr != nil {
		if mutex != 0 {
			windows.CloseHandle(mutex)
		}
		return fmt.Errorf("setup: another installer is running: %w", mutexErr)
	}
	defer windows.CloseHandle(mutex)
	if ready != 0 {
		defer windows.CloseHandle(ready)
		defer windows.CloseHandle(parent)
		if err = windows.SetEvent(ready); err != nil {
			return err
		}
		result, waitErr := windows.WaitForSingleObject(parent, 10000)
		if waitErr != nil || result != windows.WAIT_OBJECT_0 {
			return errors.New("setup: updater process did not exit")
		}
	}
	if remove && code == "" {
		return errors.New("setup: Shixin is not installed by Windows Installer")
	}
	prompt := i18n.T("setup.confirm")
	if winsetup.UnsignedPreview {
		prompt = i18n.T("setup.confirm.preview")
	}
	if remove {
		prompt = i18n.T("setup.remove")
	}
	if !message(prompt, 0x21) {
		return nil
	} // OK/Cancel, question
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	logs := filepath.Join(cache, "Shixin", "Logs")
	if err = os.MkdirAll(logs, 0700); err != nil {
		return err
	}
	logPath := filepath.Join(logs, "setup-"+time.Now().Format("20060102-150405")+".log")
	var msi string
	var payload *os.File
	if !remove {
		temp, err := os.MkdirTemp("", "shixin-setup-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		msi = filepath.Join(temp, "Shixin.msi")
		out, err := os.OpenFile(msi, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, io.NewSectionReader(self, bundle.Offset, bundle.Size))
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		payload, err = winsetup.LockedFile(msi)
		if err != nil {
			return err
		}
		defer payload.Close()
		st, err := payload.Stat()
		if err != nil {
			return err
		}
		hash := sha256.New()
		if _, err = io.Copy(hash, io.NewSectionReader(payload, 0, st.Size())); err != nil {
			return err
		}
		if st.Size() != bundle.Size || !bytes.Equal(hash.Sum(nil), bundle.SHA256[:]) {
			return errors.New("setup: the extracted MSI differs from the embedded bundle")
		}
		if !winsetup.UnsignedPreview {
			signer, err := winsetup.Publisher(payload)
			if err != nil {
				return err
			}
			if signer != publisher {
				return errors.New("setup: the MSI has a different publisher key")
			}
		}
	} else {
		msi = code
	}
	enabled := false
	prepared, profileChanged, committed := false, false, false
	defer func() {
		if err != nil && prepared && !committed {
			if profileChanged {
				if restoreErr := winsetup.Front(dir, "-user-register", "-preserve-startup"); restoreErr != nil {
					err = errors.Join(err, restoreErr)
				}
			}
			if restartErr := winsetup.StartHost(dir); restartErr != nil {
				err = errors.Join(err, restartErr)
			}
		}
	}()
	if code != "" {
		enabled = winsetup.Front(dir, "-added") == nil
		prepared = true
		if err = winsetup.Front(dir, "-prepare-upgrade"); err != nil {
			return fmt.Errorf("setup: close Shixin and its settings before installing: %w", err)
		}
	}
	var before func() error
	if remove && enabled {
		before = func() error {
			profileChanged = true
			return winsetup.Front(dir, "-user-unregister")
		}
	}
	reboot, err := winsetup.RunMSI(msi, remove, logPath, before)
	if err != nil {
		return err
	}
	committed = true
	if remove {
		if err := winsetup.RemoveStartup(dir); err != nil {
			return err
		}
		text := i18n.T("setup.removed")
		if reboot {
			text += "\n" + i18n.T("setup.restart")
		}
		message(text, 0x40)
		return nil
	}
	// A replacement must preserve a deliberately disabled keyboard. Only a
	// first install enables the original caller, never the UAC administrator.
	if code == "" || enabled {
		args := []string{"-user-register"}
		if code != "" {
			args = append(args, "-preserve-startup")
		}
		if err = winsetup.Front(dir, args...); err != nil {
			return fmt.Errorf("setup: files installed; enable Shixin in Windows input settings: %w", err)
		}
	}
	if reboot {
		message(i18n.T("setup.reboot"), 0x40)
		return nil
	}
	if err = winsetup.StartHost(dir); err != nil {
		return err
	}
	return winsetup.StartSettings(dir, code == "")
}
