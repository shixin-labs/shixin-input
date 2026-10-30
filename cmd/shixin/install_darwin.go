package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"shixin/internal/engine"
	"shixin/internal/settings"
)

// Installing by double-click (DESIGN §5.5). Opened from anywhere but
// ~/Library/Input Methods (Downloads, a disk image), Shixin.app installs
// itself and opens the settings app's 开始使用 guide; macOS itself only
// ever launches the installed copy. macOS 26 doesn't let an app add its
// own input source, so the guide walks the user through the last step.

// installedApp is where Shixin.app lives once installed.
func installedApp() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Input Methods", "Shixin.app")
}

// ownBundle is the bundle this executable runs from.
func ownBundle() string {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(exe))) // X.app/Contents/MacOS/X
}

// runningInstalled reports whether this is the installed copy.
func runningInstalled() bool {
	own, inst := ownBundle(), installedApp()
	if real, err := filepath.EvalSymlinks(inst); err == nil {
		inst = real
	}
	return own == inst
}

// installSelf copies this bundle into ~/Library/Input Methods, or upgrades
// the installed one in place (never to an older or the same build), then
// opens the guide. macOS 26 asks for consent in a dialog inside System
// Settings, which may open behind other windows: until 拾新 is added,
// System Settings is brought to the front.
func installSelf() error {
	src, dest := ownBundle(), installedApp()
	if filepath.Ext(src) != ".app" {
		return fmt.Errorf("install: not running from an app bundle (%s)", src)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	front := filepath.Join(dest, "Contents", "MacOS", "Shixin")
	// What to open while 拾新 isn't added: System Settings with the consent
	// dialog after a first install, else its input source editor, whose +
	// adds it (a repeated request's Allow adds nothing).
	show := []string{"-b", systemSettings}
	if fileExists(filepath.Join(dest, "Contents", "Info.plist")) {
		if have, got := bundleBuild(dest), bundleBuild(src); newerBuild(got, have) {
			if err := upgradeInstalled(src, dest); err != nil {
				return err
			}
		} else {
			logf("build %s is installed; not replacing it with build %s", have, got)
		}
		if run(front, "-added") != nil {
			prepareSetup()
			show = []string{inputSources}
		}
	} else {
		if err := run("/usr/bin/ditto", src, dest); err != nil {
			return fmt.Errorf("install: copy: %w", err)
		}
		unquarantine(dest)
		prepareSetup()
		// Registers, and asks macOS to enable both input sources: it shows
		// a consent dialog for each.
		if err := run(front, "-install"); err != nil {
			return fmt.Errorf("install: register: %w", err)
		}
	}
	forgetLater()
	if err := run("/usr/bin/open", filepath.Join(dest, "Contents", "Helpers", "Shixin Settings.app"), "--args", "-welcome"); err != nil {
		return err
	}
	if run(front, "-added") != nil {
		time.Sleep(time.Second) // after the guide, so System Settings is on top
		run("/usr/bin/open", show...)
	}
	return nil
}

const (
	// systemSettings is System Settings' bundle id.
	systemSettings = "com.apple.systempreferences"
	// inputSources opens System Settings' input source editor.
	inputSources = "x-apple.systempreferences:com.apple.Keyboard-Settings.extension?InputSources"
)

// bundleBuild is app's CFBundleVersion, "" if unreadable.
func bundleBuild(app string) string {
	out, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleVersion", "raw", filepath.Join(app, "Contents", "Info.plist")).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// newerBuild reports whether build got is newer than have. Builds are
// timestamps (20261027235857, or 20261027-223527 from older scripts), so
// their digits compare as numbers; an unreadable installed build is older
// than anything.
func newerBuild(got, have string) bool {
	digits := func(s string) string {
		return strings.TrimLeft(strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s), "0")
	}
	g, h := digits(got), digits(have)
	if len(g) != len(h) {
		return len(g) > len(h)
	}
	return g > h
}

// prepareSetup readies macOS for adding 拾新: System Settings' input source
// list is refreshed and the input method launcher restarted, so neither
// holds what it knew before this install.
func prepareSetup() {
	refreshSettingsList()
	restartLauncher()
}

// restartLauncher restarts macOS's input method launcher (launchd starts
// it again on demand). After an older build had run as the input method
// from ~/Downloads, selecting the installed copy did nothing until the
// launcher was restarted.
func restartLauncher() {
	exec.Command("/usr/bin/pkill", "-u", strconv.Itoa(os.Getuid()), "-x", "imklaunchagent").Run()
}

// lsregister is LaunchServices' registration tool.
const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// lsUnregister drops LaunchServices' record of app.
func lsUnregister(app string) { exec.Command(lsregister, "-u", app).Run() }

// forgetDelay lets the copies that registered themselves by running (the
// one opened, the staged upgrade) exit first: LaunchServices registers a
// running app again.
const forgetDelay = 5 * time.Second

// forgetLater starts the installed front with -forget, detached, to drop
// LaunchServices' records of every other copy once this one has exited.
func forgetLater() {
	cmd := exec.Command(filepath.Join(installedApp(), "Contents", "MacOS", "Shixin"), "-forget")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err == nil {
		cmd.Process.Release()
	}
}

// forgetCopies drops LaunchServices' records of 拾新 outside the installed
// bundle. On one Mac (macOS 26.7.1), records of deleted copies (an
// upgrade's staging directory, a Downloads copy) stayed under the same
// bundle id; macOS should find 拾新 only where it's installed.
func forgetCopies() {
	out, err := exec.Command(lsregister, "-dump").Output()
	if err != nil {
		return
	}
	dest := installedApp()
	for _, p := range shixinRecords(string(out)) {
		if p != dest && !strings.HasPrefix(p, dest+"/") {
			lsUnregister(p)
		}
	}
}

// shixinRecords lists the bundle paths in `lsregister -dump` output whose
// identifier is one of 拾新's (the input method and its settings app).
func shixinRecords(dump string) []string {
	var paths []string
	seen := map[string]bool{}
	path := ""
	for _, line := range strings.Split(dump, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "path":
			if i := strings.LastIndex(v, " (0x"); i >= 0 {
				v = v[:i]
			}
			path = v
		case "identifier":
			if (v == bundleID || strings.HasPrefix(v, bundleID+".")) && path != "" && !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	return paths
}

// refreshSettingsList quits System Settings and drops its Keyboard pane's
// private copy of the input source list. That copy can be weeks old: it
// lacks input methods installed after it was written, so 拾新 is missing
// from + until the user logs out (seen on macOS 26.7.1). macOS rebuilds it
// the next time the pane opens. Call it before asking for consent: the
// dialog is in System Settings.
func refreshSettingsList() {
	uid := strconv.Itoa(os.Getuid())
	exec.Command("/usr/bin/pkill", "-u", uid, "-x", "System Settings").Run()
	exec.Command("/usr/bin/pkill", "-u", uid, "-x", "KeyboardSettings").Run()
	out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_CACHE_DIR").Output()
	if err != nil {
		return
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "com.apple.Keyboard-Settings.extension")
	caches, _ := filepath.Glob(filepath.Join(dir, "com.apple.IntlDataCache.le*"))
	for _, f := range caches {
		os.Remove(f)
	}
}

// upgradeInstalled swaps src's contents into the installed bundle and
// restarts it, keeping the user's enabled input source (DESIGN §5.5): the
// same steps as scripts/macos-vm.sh upgrade.
func upgradeInstalled(src, dest string) error {
	home, _ := os.UserHomeDir()
	stageDir := filepath.Join(home, "Library", "Caches", "ShixinInstall")
	os.RemoveAll(stageDir)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("upgrade: %w", err)
	}
	staged := filepath.Join(stageDir, "Shixin.app")
	if err := run("/usr/bin/ditto", src, staged); err != nil {
		return fmt.Errorf("upgrade: copy: %w", err)
	}
	unquarantine(staged)
	if err := run(filepath.Join(staged, "Contents", "MacOS", "Shixin"), "-upgrade-into", dest); err != nil {
		return fmt.Errorf("upgrade: %w", err)
	}
	stopFronts(dest)
	// An open settings app would keep running the old code; the installer
	// opens the new one.
	stopProcs(filepath.Join(dest, "Contents", "Helpers", "Shixin Settings.app", "Contents", "MacOS", "shixin-settings"))
	if err := run("/usr/bin/open", "-g", dest, "--args", "-after-upgrade"); err != nil {
		return fmt.Errorf("upgrade: restart: %w", err)
	}
	os.RemoveAll(stageDir) // the previous contents
	return nil
}

// uninstall turns off 拾新's input sources, stops the front and the engine,
// and removes the installed bundle. Settings, learned words and logs stay
// in ~/Library/Application Support/Shixin and ~/Library/Logs/Shixin.
func uninstall() error {
	t := loadTIS()
	t.disableBundle(bundleID)
	dest := installedApp()
	stopFronts(dest)
	if user, err := settings.RimeUserDir(); err == nil {
		engine.StopRunning(filepath.Join(user, "supervisor"), 2*time.Second)
		engine.StopRunning(user, 2*time.Second)
	}
	forgetCopies()
	lsUnregister(dest)
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("uninstall: %w", err)
	}
	fmt.Println("removed", dest)
	return nil
}

// stopFronts stops other running copies of the front in bundle app, and
// waits for them to exit.
func stopFronts(app string) { stopProcs(filepath.Join(app, "Contents", "MacOS", "Shixin")) }

// stopProcs stops the processes running exe, other than this one, and
// waits up to five seconds for them to exit.
func stopProcs(exe string) {
	out, _ := exec.Command("/usr/bin/pgrep", "-f", exe).Output()
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil && pid != os.Getpid() {
			pids = append(pids, pid)
			syscall.Kill(pid, syscall.SIGTERM)
		}
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		alive := false
		for _, pid := range pids {
			if syscall.Kill(pid, 0) == nil {
				alive = true
			}
		}
		if !alive {
			return
		}
	}
}

// unquarantine clears the download quarantine on our own copy, so macOS
// loads the installed input method without asking again.
func unquarantine(app string) {
	run("/usr/bin/xattr", "-dr", "com.apple.quarantine", app)
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(out) > 0 {
			return fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return nil
}
