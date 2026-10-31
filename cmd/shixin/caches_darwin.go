package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"shixin/internal/ipc"
)

// Stale input source caches (DESIGN §5.5, docs/macos-input-menu-recovery.md).
// macOS keeps copies of the input source list that it doesn't always
// rebuild when an input method is added; on one Mac (macOS 26.7.1) two
// of them hid an added 拾新 without a logout. Once 拾新 is added,
// repairCaches checks both and clears only what's stale: they're caches,
// which macOS rebuilds.

// repairCaches repairs the input menu's list and other apps' caches if
// they lack 拾新. It does nothing until 拾新 is added.
func repairCaches() {
	if !loadTIS().added(bundleID) {
		return
	}
	if listed, ok := menuListed(); ok && !listed {
		clearMenuList()
		logf("input menu: its list lacked 拾新; cleared it")
	}
	// A previous repair may already have rebuilt every disk cache while
	// leaving a client's parent-source enabled flag stale.
	if err := invalidateEnabledInputSources(); err != nil {
		logf("input source enabled-state refresh: %v", err)
	}
	startCacheWatcher()
}

// menuPasteboard is the named pasteboard where the input menu keeps its
// list of input sources, in order.
const menuPasteboard = "AppleInputSourcesInUIOrderPasteboard"

// carbonPasteboard is the Pasteboard Manager, which reads the menu's list
// (NSPasteboard sees its flavor but returns no data).
type carbonPasteboard struct {
	create, sync, count, itemID, flavors, flavorData, clear  uintptr
	cfString, arrayCount, arrayAt, dataLen, dataPtr, release uintptr
}

func loadPasteboard() (*carbonPasteboard, bool) {
	cf, err1 := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	as, err2 := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err1 != nil || err2 != nil {
		return nil, false
	}
	ok := true
	sym := func(h uintptr, n string) uintptr {
		p, err := purego.Dlsym(h, n)
		if err != nil {
			ok = false
		}
		return p
	}
	p := &carbonPasteboard{
		create: sym(as, "PasteboardCreate"), sync: sym(as, "PasteboardSynchronize"),
		count: sym(as, "PasteboardGetItemCount"), itemID: sym(as, "PasteboardGetItemIdentifier"),
		flavors: sym(as, "PasteboardCopyItemFlavors"), flavorData: sym(as, "PasteboardCopyItemFlavorData"),
		clear:    sym(as, "PasteboardClear"),
		cfString: sym(cf, "CFStringCreateWithCString"), arrayCount: sym(cf, "CFArrayGetCount"),
		arrayAt: sym(cf, "CFArrayGetValueAtIndex"), dataLen: sym(cf, "CFDataGetLength"),
		dataPtr: sym(cf, "CFDataGetBytePtr"), release: sym(cf, "CFRelease"),
	}
	return p, ok
}

// open returns the named pasteboard, 0 on failure; the caller releases it.
func (p *carbonPasteboard) open(name string) uintptr {
	b := append([]byte(name), 0)
	s, _, _ := purego.SyscallN(p.cfString, 0, uintptr(unsafe.Pointer(&b[0])), 0x08000100) // UTF-8
	if s == 0 {
		return 0
	}
	defer purego.SyscallN(p.release, s)
	var ref uintptr
	if st, _, _ := purego.SyscallN(p.create, s, uintptr(unsafe.Pointer(&ref))); int32(st) != 0 {
		return 0
	}
	return ref
}

// menuListed reports whether the input menu's list has 拾新's mode; ok is
// false if the list can't be read (then nothing is touched).
func menuListed() (listed, ok bool) {
	p, loaded := loadPasteboard()
	if !loaded {
		return false, false
	}
	ref := p.open(menuPasteboard)
	if ref == 0 {
		return false, false
	}
	defer purego.SyscallN(p.release, ref)
	purego.SyscallN(p.sync, ref)
	var n uintptr
	purego.SyscallN(p.count, ref, uintptr(unsafe.Pointer(&n)))
	want := []byte("<string>" + inputMode + "</string>")
	read := false
	for i := uintptr(1); i <= n; i++ {
		var id, flavors uintptr
		purego.SyscallN(p.itemID, ref, i, uintptr(unsafe.Pointer(&id)))
		if st, _, _ := purego.SyscallN(p.flavors, ref, id, uintptr(unsafe.Pointer(&flavors))); int32(st) != 0 || flavors == 0 {
			continue
		}
		fc, _, _ := purego.SyscallN(p.arrayCount, flavors)
		for j := uintptr(0); j < fc; j++ {
			fl, _, _ := purego.SyscallN(p.arrayAt, flavors, j)
			var data uintptr
			if st, _, _ := purego.SyscallN(p.flavorData, ref, id, fl, uintptr(unsafe.Pointer(&data))); int32(st) != 0 || data == 0 {
				continue
			}
			l, _, _ := purego.SyscallN(p.dataLen, data)
			ptr, _, _ := purego.SyscallN(p.dataPtr, data)
			has := l > 0 && bytes.Contains(unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&ptr))), l), want)
			purego.SyscallN(p.release, data)
			read = true
			if has {
				purego.SyscallN(p.release, flavors)
				return true, true
			}
		}
		purego.SyscallN(p.release, flavors)
	}
	return false, read
}

// clearMenuList clears the input menu's list and restarts the menu agent,
// which rebuilds it (launchd starts the agent again).
func clearMenuList() {
	clearMenuPasteboard()
	restartMenuAgent()
}

func clearMenuPasteboard() {
	if err := clearNamedPasteboard(menuPasteboard); err != nil {
		logf("input menu cache: %v", err)
	}
}

// intlCachePasteboard is Carbon's modification-date marker for IntlDataCache.
// Invalidating this marker makes clients recheck their cached input sources on
// their next text-input activity. It is separate from the general clipboard.
const intlCachePasteboard = "AppleIntlFileCacheModificationDatePasteboard"

// This is HIToolbox's internal enable-state invalidation, not the public
// kTISNotifyEnabledKeyboardInputSourcesChanged notification. The latter only
// announces a change; it does not discard clients' cached enabled flags.
const enabledSourcesChangedNotification = "AppleEnabledInputSourcesChangedNotification"

func refreshInputSourceCaches() error {
	if err := clearNamedPasteboard(intlCachePasteboard); err != nil {
		return err
	}
	return invalidateEnabledInputSources()
}

func invalidateEnabledInputSources() error {
	// A rebuilt file cache can still leave an already-running app believing
	// the parent input method is disabled while its mode is enabled. Ask
	// HIToolbox to reread that state after invalidating the file-cache marker.
	// Neither operation changes the user's enabled sources or clipboard.
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("load cache notification API: %w", err)
	}
	defer purego.Dlclose(cf)
	get, err := purego.Dlsym(cf, "CFNotificationCenterGetDistributedCenter")
	if err != nil {
		return err
	}
	post, err := purego.Dlsym(cf, "CFNotificationCenterPostNotification")
	if err != nil {
		return err
	}
	t := loadTIS()
	name := t.cfstr(enabledSourcesChangedNotification)
	defer purego.SyscallN(t.cfRelease, name)
	center, _, _ := purego.SyscallN(get)
	if center == 0 {
		return fmt.Errorf("distributed notification center unavailable")
	}
	purego.SyscallN(post, center, name, 0, 0, 1)
	return nil
}

func clearNamedPasteboard(name string) error {
	p, ok := loadPasteboard()
	if !ok {
		return fmt.Errorf("load Pasteboard Manager")
	}
	ref := p.open(name)
	if ref == 0 {
		return fmt.Errorf("open %s", name)
	}
	defer purego.SyscallN(p.release, ref)
	if st, _, _ := purego.SyscallN(p.clear, ref); int32(st) != 0 {
		return fmt.Errorf("clear %s: OSStatus %d", name, int32(st))
	}
	return nil
}

func restartMenuAgent() {
	exec.Command("/usr/bin/pkill", "-u", strconv.Itoa(os.Getuid()), "-x", "TextInputMenuAgent").Run()
}

// cacheApps inspects only installed apps with stale input-source caches.
// It runs in a helper process, never on the front's key or UI thread.
func cacheApps(repair bool) ([]string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cacheDir, err := userCacheDir()
	if err != nil {
		return nil, err
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return nil, err
	}
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	workspace := objc.ID(objc.GetClass("NSWorkspace")).Send(objc.RegisterName("sharedWorkspace"))
	running := objc.ID(objc.GetClass("NSRunningApplication"))
	var pending []string
	removed, err := removeStaleCaches(cacheDir, func(app string) bool {
		id := nsStringOf(app)
		url := workspace.Send(objc.RegisterName("URLForApplicationWithBundleIdentifier:"), id)
		if url == 0 {
			return false
		}
		active := running.Send(objc.RegisterName("runningApplicationsWithBundleIdentifier:"), id).Send(objc.RegisterName("count")) != 0
		// Include idle caches until the watcher has actually removed them.
		if active || !repair {
			name := url.Send(objc.RegisterName("lastPathComponent")).Send(objc.RegisterName("stringByDeletingPathExtension"))
			label := ""
			if ptr := name.Send(objc.RegisterName("UTF8String")); ptr != 0 {
				label = string(cbytes(uintptr(ptr)))
			}
			if label == "" {
				label = app
			}
			pending = append(pending, label)
		}
		return repair && !active
	})
	for _, app := range removed {
		logf("input source cache of %s lacked 拾新; removed it", app)
	}
	return pending, err
}

// userCacheDir is the per-user cache directory, looked up once: the
// watcher asks every second for as long as a stale app keeps running.
var userCacheDir = sync.OnceValues(func() (string, error) {
	out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_CACHE_DIR").Output()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
})

func startCacheWatcher() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "-watch-caches")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logf("cache watcher: %v", err)
		return
	}
	cmd.Process.Release()
}

// watchCaches survives Settings closing. A process lock prevents duplicate
// watchers after repeated repairs; all I/O stays in this disposable helper.
func watchCaches() {
	dir, err := ipc.RuntimeDir()
	if err != nil {
		logf("cache watcher: %v", err)
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "cache-repair.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		logf("cache watcher: %v", err)
		return
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	refreshed := false
	for loadTIS().added(bundleID) {
		pending, err := cacheApps(true)
		if err != nil {
			logf("cache watcher: %v", err)
			return
		}
		if len(pending) == 0 {
			return
		}
		if !refreshed {
			// Running clients may refresh in place; do not ask them to quit
			// before trying Carbon's marker. Keep watching for clients that
			// only rebuild on their next key, or eventually quit.
			if err := refreshInputSourceCaches(); err != nil {
				logf("input source cache refresh: %v", err)
			} else {
				logf("input source caches: invalidated file-cache marker and enabled-source state for running apps")
			}
			refreshed = true // never repeatedly invalidate while a client is idle
		}
		time.Sleep(time.Second)
	}
}

func printCacheStatus() error {
	var pending []string
	if loadTIS().added(bundleID) {
		var err error
		pending, err = cacheApps(false)
		if err != nil {
			return fmt.Errorf("inspect input source caches: %w", err)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(pending)
}

// removeStaleCaches removes, under cacheDir, the input source caches that
// lack 拾新 and belong to an app idle reports as installed and not running.
// It returns those apps' bundle ids.
func removeStaleCaches(cacheDir string, idle func(app string) bool) ([]string, error) {
	caches, err := filepath.Glob(filepath.Join(cacheDir, "*", "com.apple.IntlDataCache.le.kbdx"))
	if err != nil {
		return nil, err
	}
	want := []byte(bundleID)
	var removed []string
	for _, kbdx := range caches {
		dir := filepath.Dir(kbdx)
		app := filepath.Base(dir)
		if app == "com.apple.Keyboard-Settings.extension" || app == bundleID || strings.HasPrefix(app, bundleID+".") {
			continue // System Settings' copy is refreshed on its own (refreshSettingsList)
		}
		data, err := os.ReadFile(kbdx)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return removed, fmt.Errorf("read %s: %w", kbdx, err)
		}
		if bytes.Contains(data, want) || !idle(app) {
			continue
		}
		for _, path := range []string{filepath.Join(dir, "com.apple.IntlDataCache.le"), kbdx} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return removed, fmt.Errorf("remove %s: %w", path, err)
			}
		}
		removed = append(removed, app)
	}
	return removed, nil
}

// nsStringOf returns an autoreleased NSString for s, without initObjC (cold
// path, command-line modes).
func nsStringOf(s string) objc.ID {
	b := append([]byte(s), 0)
	id := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), unsafe.Pointer(&b[0]))
	runtime.KeepAlive(b)
	return id
}
