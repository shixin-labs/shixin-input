package main

import (
	"golang.org/x/sys/windows"
	"path/filepath"
	"shixin/internal/settings"
	"testing"
)

func TestWindowsDesktopAppRules(t *testing.T) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_System, 0x4000)
	if err != nil {
		t.Fatal(err)
	}
	id := windows.GUID(*windows.FOLDERID_System).String() + `\cmd.exe`
	alias := settings.AppKey(filepath.Join(base, "cmd.exe"))
	s := settings.Default()
	s.Apps = []settings.AppRule{{ID: id, Mode: settings.ModeEnglish}}
	s.AutoSpace = true
	s.NoAutoSpace = []string{id}
	m := appModes(&s)
	if !m.English[id] || !m.English[alias] || !m.NoAutoSpace[alias] {
		t.Fatalf("desktop app rules not mapped: %+v", m)
	}
	// A path rule is authoritative over an indirect shortcut rule.
	s.Apps = append(s.Apps, settings.AppRule{ID: alias, Mode: settings.ModeChinese})
	if m := appModes(&s); m.English[alias] {
		t.Fatal("indirect rule overrode explicit path")
	}
	// Conflicting shortcut IDs resolving to one executable never pick a mode.
	other := windows.GUID(*windows.FOLDERID_Windows).String() + `\System32\cmd.exe`
	s.Apps = []settings.AppRule{{ID: id, Mode: settings.ModeEnglish}, {ID: other, Mode: settings.ModeChinese}}
	m = appModes(&s)
	if _, found := m.English[alias]; found {
		t.Fatal("conflicting aliases guessed a mode")
	}
}
