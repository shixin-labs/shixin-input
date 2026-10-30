package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
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
	if got := removeStaleCaches(dir, idle); !slices.Equal(got, []string{"com.tinyspeck.slackmacgap"}) {
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
