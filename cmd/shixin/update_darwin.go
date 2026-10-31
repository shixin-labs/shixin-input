package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// execRunner runs the real tools.
func execRunner(name string, args ...string) (string, string, error) {
	var out, errb bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

// applyUpdate installs the update in the disk image dmg, which the settings
// app downloaded: it mounts the image read-only, verifies it (verifyUpdate)
// and swaps the new bundle's contents into the installed one, as an upgrade
// does (DESIGN §5.5), then reopens the settings app on 关于. It runs
// detached, since the upgrade stops the settings app that started it.
func applyUpdate(dmg, version string) (err error) {
	dest := installedApp()
	logf("update: applying %s from %s", version, filepath.Base(dmg))
	defer func() {
		if err != nil {
			logf("update: failed: %v", err)
		}
	}()
	// Whatever happens, the download goes: it is untrusted data, and the
	// next check fetches it again. Registered first, it runs after the
	// image is detached. Only the file and its (then empty) folder are
	// removed, whatever path was passed.
	defer func() {
		os.Remove(dmg)
		os.Remove(filepath.Dir(dmg))
	}()
	mount, err := os.MkdirTemp("", "shixin-update-")
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	defer os.Remove(mount)
	if _, stderr, err := execRunner("/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mount, dmg); err != nil {
		return fmt.Errorf("update: mount: %w: %s", err, strings.TrimSpace(stderr))
	}
	defer func() {
		if _, _, err := execRunner("/usr/bin/hdiutil", "detach", mount); err != nil {
			execRunner("/usr/bin/hdiutil", "detach", "-force", mount)
		}
	}()
	logf("update: mounted the image")
	app, err := verifyUpdate(execRunner, dmg, mount, dest, version)
	if err != nil {
		return fmt.Errorf("update: not installing: %w", err)
	}
	logf("update: verified; installing")
	if err := upgradeInstalled(app, dest); err != nil {
		return err
	}
	logf("update: installed %s", version)
	if err := run("/usr/bin/open", filepath.Join(dest, "Contents", "Helpers", "Shixin Settings.app"), "--args", "-page", "about"); err != nil {
		logf("update: reopen settings: %v", err)
	}
	return nil
}
