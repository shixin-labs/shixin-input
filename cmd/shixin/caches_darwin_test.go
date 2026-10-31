package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// TestRemoveStaleCaches removes only caches that lack 拾新 and belong to
// an installed app that isn't running, and never System Settings' copy.
func TestRemoveStaleCaches(t *testing.T) {
	dir := t.TempDir()
	write := func(app, kbdx string) {
		d := filepath.Join(dir, app)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(d, "com.apple.IntlDataCache.le"), []byte("x"), 0o600)
		os.WriteFile(filepath.Join(d, "com.apple.IntlDataCache.le.kbdx"), []byte(kbdx), 0o600)
	}
	stale := "/Library/Input Methods/Squirrel.app im.rime.inputmethod.Squirrel"
	fresh := stale + " /Users/u/Library/Input Methods/Shixin.app " + bundleID
	write("com.tinyspeck.slackmacgap", stale)             // stale and idle: removed
	write("com.apple.Safari", fresh)                      // lists 拾新: kept
	write("com.tencent.xinWeChat", stale)                 // stale but running: kept
	write("com.apple.Keyboard-Settings.extension", stale) // System Settings': kept
	idle := func(app string) bool { return app != "com.tencent.xinWeChat" }
	if got, err := removeStaleCaches(dir, idle); err != nil || !slices.Equal(got, []string{"com.tinyspeck.slackmacgap"}) {
		t.Errorf("removed %q", got)
	}
	for app, keep := range map[string]bool{
		"com.tinyspeck.slackmacgap": false, "com.apple.Safari": true,
		"com.tencent.xinWeChat": true, "com.apple.Keyboard-Settings.extension": true,
	} {
		for _, f := range []string{"com.apple.IntlDataCache.le", "com.apple.IntlDataCache.le.kbdx"} {
			if _, err := os.Stat(filepath.Join(dir, app, f)); (err == nil) != keep {
				t.Errorf("%s/%s exists %v, want %v", app, f, err == nil, keep)
			}
		}
	}
	// A later pass after the app quits repairs its cache, retaining fresh ones.
	got, err := removeStaleCaches(dir, func(string) bool { return true })
	if err != nil || !slices.Equal(got, []string{"com.tencent.xinWeChat"}) {
		t.Fatalf("after quit: %v, %v", got, err)
	}
	got, err = removeStaleCaches(dir, func(string) bool { return true })
	if err != nil || len(got) != 0 {
		t.Fatalf("repeat repair: %v, %v", got, err)
	}
}

// TestMenuListRebuild clears the input menu's list, as repairCaches does
// when it lacks 拾新, and checks that macOS rebuilds it with 拾新. It
// changes the logged-in user's real menu list: run it only in the VM, with
// 拾新 added (SHIXIN_TEST_MENU=1).
func TestMenuListRebuild(t *testing.T) {
	if os.Getenv("SHIXIN_TEST_MENU") != "1" {
		t.Skip("clears the real input menu list; VM only (SHIXIN_TEST_MENU=1)")
	}
	if !loadTIS().added(bundleID) {
		t.Fatal("拾新 isn't added")
	}
	if listed, ok := menuListed(); !ok || !listed {
		t.Fatalf("before: listed %v, readable %v", listed, ok)
	}
	clearMenuPasteboard()
	if listed, _ := menuListed(); listed {
		t.Fatal("still listed after clearing: the clear didn't take")
	}
	restartMenuAgent()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if listed, ok := menuListed(); ok && listed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the menu list wasn't rebuilt with 拾新 in 20 s")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// A failed removal must not be reported as a successful repair. Keep the
// discovery file so a later repair can find the app again.
func TestRemoveStaleCachesFailure(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "com.example.App")
	cache := filepath.Join(app, "com.apple.IntlDataCache.le")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	kbdx := filepath.Join(app, "com.apple.IntlDataCache.le.kbdx")
	if err := os.WriteFile(kbdx, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := removeStaleCaches(dir, func(string) bool { return true })
	if err == nil || len(got) != 0 {
		t.Fatalf("failed removal reported as success: %v, %v", got, err)
	}
	if _, err := os.Stat(kbdx); err != nil {
		t.Fatalf("lost retry marker: %v", err)
	}
}

// This test refreshes source caches and seeds the general clipboard with test
// data. Run only in the VM; ordinary unit tests never touch either pasteboard.
func TestCacheRefreshPreservesClipboard(t *testing.T) {
	if os.Getenv("SHIXIN_TEST_CACHE_REFRESH") != "1" {
		t.Skip("changes clipboard and input cache marker; VM only")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		t.Fatal(err)
	}
	initObjC()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	board := send(uintptr(objc.GetClass("NSPasteboard")), objc.RegisterName("generalPasteboard"))
	if board == 0 {
		t.Fatal("general clipboard unavailable")
	}
	send(board, objc.RegisterName("clearContents"))
	flavors := map[string][]byte{
		"public.utf8-plain-text": []byte("Shixin clipboard regression"),
		"public.html":            []byte("<b>Shixin clipboard regression</b>"),
		"public.file-url":        []byte("file:///tmp/shixin-clipboard-fixture"),
		"im.shixin.test.binary":  {0, 255, 1, 0, 128, 42},
	}
	var types []uintptr
	for flavor := range flavors {
		types = append(types, uintptr(nsStringOf(flavor)))
	}
	array := send(uintptr(objc.GetClass("NSArray")), objc.RegisterName("arrayWithObjects:count:"), uintptr(unsafe.Pointer(&types[0])), uintptr(len(types)))
	send(board, objc.RegisterName("declareTypes:owner:"), array, 0)
	for flavor, value := range flavors {
		data := send(uintptr(objc.GetClass("NSData")), objc.RegisterName("dataWithBytes:length:"), uintptr(unsafe.Pointer(&value[0])), uintptr(len(value)))
		if send(board, objc.RegisterName("setData:forType:"), data, uintptr(nsStringOf(flavor))) == 0 {
			t.Fatalf("seed %s", flavor)
		}
	}
	count := send(board, objc.RegisterName("changeCount"))
	for range 2 {
		if err := refreshInputSourceCaches(); err != nil {
			t.Fatal(err)
		}
		if got := send(board, objc.RegisterName("changeCount")); got != count {
			t.Fatalf("general clipboard changed: %d -> %d", count, got)
		}
		for flavor, want := range flavors {
			data := send(board, objc.RegisterName("dataForType:"), uintptr(nsStringOf(flavor)))
			n := send(data, objc.RegisterName("length"))
			ptr := send(data, objc.RegisterName("bytes"))
			if n != uintptr(len(want)) || ptr == 0 {
				t.Fatalf("lost %s", flavor)
			}
			got := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&ptr))), int(n))
			if !bytes.Equal(got, want) {
				t.Fatalf("changed %s", flavor)
			}
		}
	}
}
