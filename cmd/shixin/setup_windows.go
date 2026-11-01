package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"shixin/internal/engine"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/process"
	"shixin/internal/settings"
	shixupdate "shixin/internal/update"
	"shixin/internal/winsetup"
)

func prepareWindowsUpgrade() error {
	dir, err := ipc.RuntimeDir()
	if err != nil {
		return err
	}
	// Settings flushes pending valid edits before acknowledging its quit.
	for _, endpoint := range []string{"settings.stop", "host.stop", "engine.sock.control"} {
		if err := lifecycle.Stop(filepath.Join(dir, endpoint)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stop %s: %w", endpoint, err)
		}
	}
	user, err := settings.RimeUserDir()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		err = errors.Join(engine.StopRunning(filepath.Join(user, "supervisor"), 0), engine.StopRunning(user, 0))
		if err == nil {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			return winsetup.WaitProcesses(filepath.Dir(exe), 10*time.Second)
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func applyWindowsUpdate(path, announced string) error {
	if !winsetup.ValidVersion(announced) || !shixupdate.CanInstall(&shixupdate.Release{Version: announced, Newer: shixupdate.Newer(announced, version)}, version) {
		return errors.New("update: expected a newer stable release")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	installed, err := winsetup.LockedFile(exe)
	if err != nil {
		return err
	}
	defer installed.Close()
	publisher, err := winsetup.Publisher(installed)
	if err != nil {
		return err
	}
	candidate, err := winsetup.LockedFile(path)
	if err != nil {
		return err
	}
	defer candidate.Close()
	signer, err := winsetup.Publisher(candidate)
	if err != nil {
		return err
	}
	if signer != publisher {
		return errors.New("update: the installer has a different publisher key")
	}
	st, err := candidate.Stat()
	if err != nil {
		return err
	}
	bundle, err := winsetup.Read(candidate, st.Size())
	if err != nil {
		return err
	}
	if bundle.Arch != runtime.GOARCH {
		return errors.New("update: installer architecture does not match this installation")
	}
	if bundle.Version != announced {
		return errors.New("update: the signed installer version differs from the release")
	}
	// Pass only a ready event and a wait-only handle to this process. The
	// signed child locks/verifies itself before acknowledging, then waits
	// for our actual exit. Thus MSI never replaces an executable we hold.
	var parent, ready windows.Handle
	if err = windows.DuplicateHandle(windows.CurrentProcess(), windows.CurrentProcess(), windows.CurrentProcess(), &parent, windows.SYNCHRONIZE, true, 0); err != nil {
		return err
	}
	defer windows.CloseHandle(parent)
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	if err = windows.DuplicateHandle(windows.CurrentProcess(), event, windows.CurrentProcess(), &ready, windows.EVENT_MODIFY_STATE, true, 0); err != nil {
		return err
	}
	defer windows.CloseHandle(ready)
	cmd := exec.Command(path, "-handoff-ready", strconv.FormatUint(uint64(ready), 10), "-handoff-parent", strconv.FormatUint(uint64(parent), 10))
	process.Detach(cmd)
	cmd.SysProcAttr.AdditionalInheritedHandles = []syscall.Handle{syscall.Handle(ready), syscall.Handle(parent)}
	if err = cmd.Start(); err != nil {
		return err
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return err
	}
	defer windows.CloseHandle(child)
	result, err := windows.WaitForMultipleObjects([]windows.Handle{event, child}, false, 40000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		cmd.Process.Kill()
		cmd.Wait()
		return errors.New("update: the verified installer did not accept the handoff")
	}
	go cmd.Wait()
	return nil
}
