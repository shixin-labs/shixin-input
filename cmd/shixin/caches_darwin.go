package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
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
	for _, app := range staleAppCaches() {
		logf("input source cache of %s lacked 拾新; removed it", app)
	}
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
	if p, ok := loadPasteboard(); ok {
		if ref := p.open(menuPasteboard); ref != 0 {
			purego.SyscallN(p.clear, ref)
			purego.SyscallN(p.release, ref)
		}
	}
}

func restartMenuAgent() {
	exec.Command("/usr/bin/pkill", "-u", strconv.Itoa(os.Getuid()), "-x", "TextInputMenuAgent").Run()
}

// staleAppCaches removes the input source caches of apps that aren't
// running and whose cache lacks 拾新, and returns those apps' bundle ids.
// Each app keeps its own copy in <user cache dir>/<bundle id>/; Slack's,
// written weeks before 拾新 was installed, survived restarts, and Slack
// fell back to ABC. A running app's cache is left alone: it would keep its
// copy. Only directories named after an installed app are considered.
func staleAppCaches() []string {
	out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_CACHE_DIR").Output()
	if err != nil {
		return nil
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return nil
	}
	workspace := objc.ID(objc.GetClass("NSWorkspace")).Send(objc.RegisterName("sharedWorkspace"))
	running := objc.ID(objc.GetClass("NSRunningApplication"))
	selURL := objc.RegisterName("URLForApplicationWithBundleIdentifier:")
	selRunning := objc.RegisterName("runningApplicationsWithBundleIdentifier:")
	selCount := objc.RegisterName("count")
	return removeStaleCaches(strings.TrimSpace(string(out)), func(app string) bool {
		id := nsStringOf(app)
		return workspace.Send(selURL, id) != 0 && running.Send(selRunning, id).Send(selCount) == 0
	})
}

// removeStaleCaches removes, under cacheDir, the input source caches that
// lack 拾新 and belong to an app idle reports as installed and not running.
// It returns those apps' bundle ids.
func removeStaleCaches(cacheDir string, idle func(app string) bool) []string {
	caches, _ := filepath.Glob(filepath.Join(cacheDir, "*", "com.apple.IntlDataCache.le.kbdx"))
	want := []byte(bundleID)
	var removed []string
	for _, kbdx := range caches {
		dir := filepath.Dir(kbdx)
		app := filepath.Base(dir)
		if app == "com.apple.Keyboard-Settings.extension" || app == bundleID || strings.HasPrefix(app, bundleID+".") {
			continue // System Settings' copy is refreshed on its own (refreshSettingsList)
		}
		data, err := os.ReadFile(kbdx)
		if err != nil || bytes.Contains(data, want) || !idle(app) {
			continue
		}
		os.Remove(filepath.Join(dir, "com.apple.IntlDataCache.le"))
		os.Remove(kbdx)
		removed = append(removed, app)
	}
	return removed
}

// nsStringOf returns an autoreleased NSString for s, without initObjC (cold
// path, command-line modes).
func nsStringOf(s string) objc.ID {
	b := append([]byte(s), 0)
	id := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), unsafe.Pointer(&b[0]))
	runtime.KeepAlive(b)
	return id
}
