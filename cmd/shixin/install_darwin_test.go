package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// TestConnectionName keeps the IMK connection name in step with
// Info.plist and the name resolved by the installed InputMethodKit.
func TestConnectionName(t *testing.T) {
	plist, err := os.ReadFile("../../packaging/macos/Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`<key>InputMethodConnectionName</key>\s*<string>([^<]*)</string>`).FindSubmatch(plist)
	if m == nil || string(m[1]) != connName {
		t.Fatalf("Info.plist InputMethodConnectionName %q, want %q", m, connName)
	}
	if connName != bundleID+".Connection" {
		t.Errorf("connection name %q must be bundle-qualified", connName)
	}
	// This private resolver is used only in the test: it is the same check
	// imklaunchagent uses before connecting apps to an input method. A test
	// that only compares our two constants misses OS naming-rule changes.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initObjC()
	if _, err := purego.Dlopen("/System/Library/Frameworks/InputMethodKit.framework/InputMethodKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(t.TempDir(), "Shixin.app")
	contents := filepath.Join(app, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), plist, 0o644); err != nil {
		t.Fatal(err)
	}
	cls := uintptr(objc.GetClass("IMKServer"))
	resolver := objc.RegisterName("connectionNameFor:")
	if send(cls, objc.RegisterName("respondsToSelector:"), uintptr(resolver)) == 0 {
		t.Skip("IMK connection-name resolver unavailable")
	}
	bundle := send(uintptr(objc.GetClass("NSBundle")), objc.RegisterName("bundleWithPath:"), cstring(app))
	if bundle == 0 {
		t.Fatal("could not load test bundle")
	}
	if got := goString(send(cls, resolver, bundle)); got != connName {
		t.Fatalf("IMK resolves %q, but the front publishes %q", got, connName)
	}
}

// TestShixinRecords parses `lsregister -dump` (abridged from a real one,
// macOS 26.7.1) for the bundles registered under 拾新's identifiers.
func TestShixinRecords(t *testing.T) {
	const dump = `--------------------------------------------------------------------------------
bundle id:                  14176
path:                       /Users/u/Library/Input Methods/Shixin.app (0x16d7244)
name:                       拾新
identifier:                 im.shixin.inputmethod.Shixin
--------------------------------------------------------------------------------
bundle id:                  14160
path:                       /private/tmp/shixin-upgrade/Shixin.app (0x16d7210)
identifier:                 im.shixin.inputmethod.Shixin
--------------------------------------------------------------------------------
bundle id:                  14112
path:                       /Users/u/Library/Input Methods/Shixin.app/Contents/Helpers/Shixin Settings.app (0x16d7224)
identifier:                 im.shixin.inputmethod.Shixin.Settings
--------------------------------------------------------------------------------
bundle id:                  9000
path:                       /Library/Input Methods/Squirrel.app (0x16ce36c)
identifier:                 im.rime.inputmethod.Squirrel
--------------------------------------------------------------------------------
bundle id:                  9001
path:                       /Users/u/Shixin.app (0x16ce370)
identifier:                 im.shixin.inputmethod.ShixinOther
    claim id:               1
    path:                   /nested/claim (0x1)
`
	got := shixinRecords(dump)
	want := []string{
		"/Users/u/Library/Input Methods/Shixin.app",
		"/private/tmp/shixin-upgrade/Shixin.app",
		"/Users/u/Library/Input Methods/Shixin.app/Contents/Helpers/Shixin Settings.app",
	}
	if !slices.Equal(got, want) {
		t.Errorf("shixinRecords = %q, want %q", got, want)
	}
}

func TestNewerBuild(t *testing.T) {
	for _, c := range []struct {
		got, have string
		want      bool
	}{
		{"20261027235857", "20261027235433", true},
		{"20261027235433", "20261027235857", false}, // a downgrade
		{"20261027235857", "20261027235857", false}, // the same build
		{"20261027235857", "20261027-223527", true}, // older scripts' format
		{"20261027-223527", "20261027235857", false},
		{"20261027235857", "", true}, // unreadable installed build
		{"", "20261027235857", false},
		{"", "", false},
	} {
		if got := newerBuild(c.got, c.have); got != c.want {
			t.Errorf("newerBuild(%q, %q) = %v, want %v", c.got, c.have, got, c.want)
		}
	}
}
