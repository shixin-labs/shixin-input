package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// upgradeInto replaces the installed bundle's contents with this (staged)
// bundle's, without registering anything with TIS or LaunchServices
// (DESIGN §5.5, verified in spike 2).
//
// Deleting and re-copying Shixin.app makes LaunchServices treat it as an
// uninstall: the input source disappears, must be registered again, and
// macOS asks the user for consent again. Keeping the .app directory and
// atomically swapping its Contents (renamex_np RENAME_SWAP) is seen as an
// in-place modification instead, so the enabled input source survives.
//
// It runs from the staged copy; afterwards the staged path holds the
// previous Contents, which the caller deletes once the old instance exited.
func upgradeInto(installed string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	staged := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // X.app/Contents/MacOS/X
	installed = filepath.Clean(installed)
	if filepath.Ext(staged) != ".app" || staged == installed {
		return fmt.Errorf("upgrade: run from a staged copy of the new bundle (running from %s)", staged)
	}
	for _, app := range []string{staged, installed} {
		plist, err := os.ReadFile(filepath.Join(app, "Contents/Info.plist"))
		if err != nil {
			return fmt.Errorf("upgrade: %w", err)
		}
		if !bytes.Contains(plist, []byte("<string>"+bundleID+"</string>")) {
			return fmt.Errorf("upgrade: %s is not %s", app, bundleID)
		}
	}
	if err := unix.RenamexNp(filepath.Join(staged, "Contents"), filepath.Join(installed, "Contents"), unix.RENAME_SWAP); err != nil {
		return fmt.Errorf("upgrade: swap contents: %w", err)
	}
	fmt.Printf("upgraded %s to %s\n", installed, version)
	return nil
}

// reselectSelf re-selects our input mode if it is the current one. After an
// upgrade restart, apps that were focused still hold a connection to the old
// instance and pass their next key through as plain text while reconnecting;
// re-selecting makes IMK activate them against the new instance right away.
func reselectSelf() {
	t := loadTIS()
	cur := t.current()
	if cur != inputMode {
		logf("after-upgrade: current source %s, not reselecting", cur)
		return
	}
	t.selectID(inputMode)
	logf("after-upgrade: reselected %s", inputMode)
}
