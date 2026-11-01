// Command shixin is the macOS front, Shixin.app (DESIGN §5.5): the
// InputMethodKit input method with its input menu, the candidate window and
// the supervisor of shixin-engine. librime runs in the engine, never here.
//
//	Shixin                    run as the input method (launched by macOS); opened
//	                          from anywhere else, install itself (install_darwin.go)
//	Shixin -install           register the bundle with Text Input Sources
//	Shixin -refresh           quit System Settings and refresh its list of input sources
//	Shixin -forget            drop LaunchServices' records of other copies, then -repair (the installer runs it)
//	Shixin -repair            once 拾新 is added, clear stale input source caches that lack it
//	Shixin -cache-status      print pending app names as JSON (read-only)
//	Shixin -watch-caches      refresh live caches and clean idle app caches (internal helper)
//	Shixin -status [BUNDLE]   print the state of our (or BUNDLE's) input sources
//	Shixin -uninstall         turn them off, stop Shixin and remove the installed bundle
//	Shixin -added             exit 0 if the user has added 拾新 (both sources enabled)
//	Shixin -upgrade-into APP  swap this staged bundle into APP in place
//	Shixin -apply-update DMG -update-version V
//	                          verify the downloaded release DMG (notarized, same developer team, version V,
//	                          newer build) and upgrade in place; the settings app starts it detached
//	Shixin -after-upgrade     restart the engine, then re-select our input mode
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"shixin/internal/client"
	"shixin/internal/engine"
	"shixin/internal/i18n"
	"shixin/internal/ipc"
	"shixin/internal/logx"
	"shixin/internal/settings"
	"shixin/internal/ui/brand"
)

const (
	// Modern IMK falls back to this name for bundles outside Apple's legacy
	// connection-name allowlist. Older IMK reads the same name from Info.plist.
	connName  = bundleID + ".Connection"
	inputMode = bundleID + ".Hans"
)

// version is set with -ldflags "-X main.version=...".
var version = "dev"

var (
	debug  bool
	core   *client.Client
	logger *logx.Logger
	sup    *supervisor
)

func logf(format string, args ...any) { logger.Printf(format, args...) }

func main() {
	install := flag.Bool("install", false, "register this bundle with Text Input Sources")
	upgrade := flag.String("upgrade-into", "", "swap this staged bundle's contents into the installed bundle at this path, without registering")
	afterUpgrade := flag.Bool("after-upgrade", false, "restart the engine, then re-select our input source once ready")
	applyDMG := flag.String("apply-update", "", "verify the downloaded disk image at this path and upgrade the installed bundle from it")
	updateVersion := flag.String("update-version", "", "with -apply-update: the release version the disk image must contain")
	status := flag.Bool("status", false, "print the state of our input sources")
	remove := flag.Bool("uninstall", false, "turn our input sources off, stop Shixin and remove the installed bundle")
	forget := flag.Bool("forget", false, "after a few seconds, drop LaunchServices' records of every copy of Shixin but the installed one")
	watchCache := flag.Bool("watch-caches", false, "refresh stale input-source caches and clean closed apps' cache files")
	cacheStatus := flag.Bool("cache-status", false, "print JSON names of apps awaiting input-source cache repair")
	repair := flag.Bool("repair", false, "once Shixin is added, clear the input menu's and other apps' caches that lack it")
	refresh := flag.Bool("refresh", false, "quit System Settings and refresh its Keyboard pane's list of input sources")
	added := flag.Bool("added", false, "exit 0 if both of our input sources are enabled")
	// Finder may pass a process serial number (-psn_0_1234).
	os.Args = slices.DeleteFunc(os.Args, func(a string) bool { return strings.HasPrefix(a, "-psn_") })
	flag.Parse()

	switch {
	case *upgrade != "":
		must(upgradeInto(*upgrade))
		return
	case *applyDMG != "":
		openLog()
		err := applyUpdate(*applyDMG, *updateVersion)
		logger.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case *install:
		exe, _ := os.Executable()
		app := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		t := loadTIS()
		must(t.registerBundle(app))
		t.enableBundle(bundleID)
		return
	case *status:
		id := bundleID
		if flag.NArg() > 0 {
			id = flag.Arg(0) // another input method, for comparison
		}
		loadTIS().status(id)
		return
	case *added:
		if !loadTIS().added(bundleID) {
			os.Exit(1)
		}
		return
	case *cacheStatus:
		must(printCacheStatus())
		return
	case *watchCache:
		openLog()
		watchCaches()
		logger.Close()
		return
	case *refresh:
		refreshSettingsList()
		return
	case *forget:
		openLog()
		time.Sleep(forgetDelay)
		forgetCopies()
		repairCaches()
		logger.Close()
		return
	case *repair:
		openLog()
		repairCaches()
		logger.Close()
		return
	case *remove:
		must(uninstall())
		return
	case !runningInstalled() && len(os.Args) == 1:
		// Opened by the user from a download: install, then guide.
		openLog()
		logf("Shixin %s installing from %s", version, ownBundle())
		if err := installSelf(); err != nil {
			logf("install: %v", err)
			logger.Close()
			os.Exit(1)
		}
		logger.Close()
		return
	}

	openLog()
	logf("Shixin %s starting (pid %d)", version, os.Getpid())
	// Diagnostics that include key codes are opt-in (AGENTS.md, privacy).
	settingsPath, err := settings.Path()
	if err == nil {
		debug = fileExists(filepath.Join(filepath.Dir(settingsPath), "debug"))
	}
	prefs, err := settings.Load(settingsPath)
	if err != nil {
		logf("%v (reset to defaults)", err)
	}
	initObjC()
	disableAppNap()

	dir, err := ipc.RuntimeDir()
	must(err)
	sup = newSupervisor(enginePath(), logf)
	switch {
	case *afterUpgrade:
		// The engine helper may be a new version too: replace the running
		// one before connecting.
		restartEngine()
	case staleEngine():
		// An upgrade whose engine restart didn't happen: don't keep
		// talking to old code.
		logf("the running engine predates the installed one; restarting it")
		restartEngine()
	}
	core = client.New(client.Options{
		Socket:         ipc.EnginePath(dir),
		Kind:           ipc.KindMacOS,
		App:            bundleID,
		PerAppContexts: true,
		Wake:           wake,
		Unreachable:    sup.ensure,
		Logf:           logf,
	}, frontApplier{})
	core.Start(make(chan struct{}))

	mygo.App.SetName(i18n.T("app.name"))
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		// MyGo floats a window above other apps only as a tray icon's
		// flyout. The icon only lives until the candidate window exists:
		// the window keeps its level, Shixin places it itself, and its
		// menu is the system input menu's (menu_darwin.go).
		tray, err := mygo.NewTray(mygo.TrayOptions{ID: "shixin.anchor", Icon: trayIcon(), IconIsTemplate: true})
		must(err)
		newCandidateWindow(tray, &prefs)
		newModeWindow(tray)
		tray.Destroy()
		// The settings app writes settings.json; appearance changes apply
		// here at once (the coordinator hot-swaps Rime changes).
		go settings.Watch(settingsPath, time.Second, nil, func(s settings.Settings) { setLook(&s) })
		registerIMK()
		startCacheWatcher()
		installSpacingResets()
		if *afterUpgrade {
			reselectSelf()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// enginePath is the engine helper in the bundle, or next to the executable
// in a development build.
func enginePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "shixin-engine"
	}
	exe, _ = filepath.EvalSymlinks(exe)
	contents := filepath.Dir(filepath.Dir(exe))
	if p := filepath.Join(contents, "Helpers", "shixin-engine"); fileExists(p) {
		return p
	}
	return filepath.Join(filepath.Dir(exe), "shixin-engine")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// openLog opens the front's log, or stderr if it can't.
func openLog() {
	if l, err := logx.Open(logx.DefaultPath("shixin.log")); err == nil {
		logger = l
	} else {
		logger = logx.New(os.Stderr)
	}
}

// restartEngine stops the running engine (if any) and lets the supervisor
// start a new one. Keys pass through while it restarts.
func restartEngine() {
	user, err := settings.RimeUserDir()
	if err != nil {
		logf("restart engine: %v", err)
		return
	}
	// Stop the coordinator before its worker, or it would immediately
	// recreate the old generation while an upgraded front is starting.
	if err := engine.StopRunning(filepath.Join(user, "supervisor"), 2*time.Second); err != nil {
		logf("restart coordinator: %v", err)
		return
	}
	if err := engine.StopRunning(user, 2*time.Second); err != nil {
		logf("restart engine: %v", err)
	}
}

// openSettings launches the settings app, a separate process (DESIGN §6).
func openSettings() {
	exe, _ := os.Executable()
	contents := filepath.Dir(filepath.Dir(exe))
	app := filepath.Join(contents, "Helpers", "Shixin Settings.app")
	var cmd *exec.Cmd
	if fileExists(app) {
		cmd = exec.Command("/usr/bin/open", app)
	} else {
		cmd = exec.Command(filepath.Join(filepath.Dir(exe), "shixin-settings"))
	}
	if err := cmd.Start(); err != nil {
		logf("open settings: %v", err)
		return
	}
	go cmd.Wait()
}

// trayIcon uses the input source's shared mark for the temporary tray anchor.
// It is rendered once at startup, before any key can arrive.
func trayIcon() []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, brand.Template(32))
	return b.Bytes()
}

func must(err error) {
	if err != nil {
		if logger != nil {
			logf("fatal: %v", err)
			logger.Close()
		}
		log.Fatal(err)
	}
}
