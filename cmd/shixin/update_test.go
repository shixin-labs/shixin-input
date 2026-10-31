package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeamID(t *testing.T) {
	for _, c := range []struct{ out, want string }{
		{"Executable=/x\nIdentifier=a\nTeamIdentifier=ABCDE12345\nSealed Resources=none", "ABCDE12345"},
		{"Identifier=a\nTeamIdentifier=not set\n", ""},
		{"Identifier=a\nSignature=adhoc\n", ""},
		{"  TeamIdentifier= ZZ99 \n", "ZZ99"},
		{"", ""},
	} {
		if got := teamID(c.out); got != c.want {
			t.Errorf("teamID(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

// fakeTools answers the commands verifyUpdate runs.
type fakeTools struct {
	dmg, volume, installed string
	failing                map[string]bool // by tool name and first verb, e.g. "spctl"
	team, installedTeam    string
	plist                  map[string]string // by key, for the new app
	installedPlist         map[string]string
}

func (f *fakeTools) run(name string, args ...string) (string, string, error) {
	tool := filepath.Base(name)
	joined := strings.Join(args, " ")
	key := tool
	if tool == "codesign" {
		switch {
		case args[0] == "-dv":
			key = "codesign-dv"
		case strings.Contains(joined, "--deep"):
			key = "codesign-app"
		default:
			key = "codesign-dmg"
		}
	}
	if f.failing[key] {
		return "", "boom", errors.New("exit status 1")
	}
	switch key {
	case "codesign-dv":
		team := f.team
		if strings.HasPrefix(args[len(args)-1], f.installed) {
			team = f.installedTeam
		}
		if team == "" {
			team = "not set"
		}
		return "", "Identifier=x\nTeamIdentifier=" + team + "\n", nil
	case "plutil":
		plist := f.plist
		if strings.HasPrefix(args[len(args)-1], f.installed) {
			plist = f.installedPlist
		}
		v, ok := plist[args[1]]
		if !ok {
			return "", "no key", errors.New("exit status 1")
		}
		return v + "\n", "", nil
	}
	return "", "", nil
}

func newFake(t *testing.T) *fakeTools {
	volume := t.TempDir()
	if err := os.MkdirAll(filepath.Join(volume, "Shixin.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeTools{
		dmg: "/tmp/Shixin.dmg", volume: volume, installed: "/fake/Input Methods/Shixin.app",
		team: "TEAM123456", installedTeam: "TEAM123456",
		plist: map[string]string{
			"CFBundleIdentifier": bundleID, "CFBundleShortVersionString": "0.2.0", "CFBundleVersion": "20261101000000",
		},
		installedPlist: map[string]string{"CFBundleVersion": "20261027235857"},
	}
}

func TestVerifyUpdate(t *testing.T) {
	for _, c := range []struct {
		name    string
		mod     func(*fakeTools)
		version string
		want    string // "" accepts; else a substring of the error
	}{
		{"ok", func(*fakeTools) {}, "0.2.0", ""},
		{"spctl fails", func(f *fakeTools) { f.failing = map[string]bool{"spctl": true} }, "0.2.0", "not notarized"},
		{"dmg signature", func(f *fakeTools) { f.failing = map[string]bool{"codesign-dmg": true} }, "0.2.0", "disk image's signature"},
		{"app signature", func(f *fakeTools) { f.failing = map[string]bool{"codesign-app": true} }, "0.2.0", "app's signature"},
		{"no app", func(f *fakeTools) { os.Remove(filepath.Join(f.volume, "Shixin.app")) }, "0.2.0", "no Shixin.app"},
		{"other team", func(f *fakeTools) { f.team = "OTHER98765" }, "0.2.0", "OTHER98765"},
		{"new app ad hoc", func(f *fakeTools) { f.team = "" }, "0.2.0", "signed by team"},
		{"installed ad hoc", func(f *fakeTools) { f.installedTeam = "" }, "0.2.0", "ad hoc"},
		{"wrong id", func(f *fakeTools) { f.plist["CFBundleIdentifier"] = "com.example.Other" }, "0.2.0", "com.example.Other"},
		{"wrong version", func(f *fakeTools) { f.plist["CFBundleShortVersionString"] = "0.1.9" }, "0.2.0", "version"},
		{"no expected version", func(*fakeTools) {}, "", "version"},
		{"same build", func(f *fakeTools) { f.plist["CFBundleVersion"] = "20261027235857" }, "0.2.0", "not newer"},
		{"older build", func(f *fakeTools) { f.plist["CFBundleVersion"] = "20260101000000" }, "0.2.0", "not newer"},
		{"no build", func(f *fakeTools) { delete(f.plist, "CFBundleVersion") }, "0.2.0", "CFBundleVersion"},
		{"installed unreadable", func(f *fakeTools) { f.installedPlist = nil }, "0.2.0", "CFBundleVersion"},
	} {
		f := newFake(t)
		c.mod(f)
		app, err := verifyUpdate(f.run, f.dmg, f.volume, f.installed, c.version)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want == "" && app != filepath.Join(f.volume, "Shixin.app"):
			t.Errorf("%s: app %q", c.name, app)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: error %v, want one containing %q", c.name, err, c.want)
		case c.want != "" && app != "":
			t.Errorf("%s: returned app %q with an error", c.name, app)
		}
	}
}

// An ad hoc installed copy is reported as such (the settings app shows it).
func TestVerifyUpdateAdHocMessage(t *testing.T) {
	f := newFake(t)
	f.installedTeam = ""
	if _, err := verifyUpdate(f.run, f.dmg, f.volume, f.installed, "0.2.0"); !errors.Is(err, errAdHoc) {
		t.Errorf("%v", err)
	}
}
