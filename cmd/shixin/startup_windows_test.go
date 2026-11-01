package main

import (
	"crypto/rand"
	"errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"testing"
)

func TestWindowsStartupOwnership(t *testing.T) {
	path := `Software\Shixin\Tests\Startup-` + rand.Text()
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.DeleteKey(registry.CURRENT_USER, path)
	defer key.Close()
	exe := `C:\Program Files\拾新\Shixin.exe`
	if err := startupValue(key, exe, false); err != nil {
		t.Fatal(err)
	}
	command, _, err := key.GetStringValue("Shixin")
	if err != nil {
		t.Fatal(err)
	}
	args, err := windows.DecomposeCommandLine(command)
	if err != nil || len(args) != 2 || args[0] != exe || args[1] != "-startup" {
		t.Fatalf("command %q: %v, %v", command, args, err)
	}
	if err := startupValue(key, `C:\Other\Shixin.exe`, true); err != nil {
		t.Fatal(err)
	}
	if kept, _, err := key.GetStringValue("Shixin"); err != nil || kept != command {
		t.Fatal("unrelated installation removed", err)
	}
	for _, custom := range []string{command + " -custom", `"` + exe + `" -STARTUP`} {
		if err := key.SetStringValue("Shixin", custom); err != nil {
			t.Fatal(err)
		}
		if err := startupValue(key, exe, true); err != nil {
			t.Fatal(err)
		}
		if kept, _, err := key.GetStringValue("Shixin"); err != nil || kept != custom {
			t.Fatal("custom command removed", err)
		}
	}
	if err := startupValue(key, exe, false); err != nil {
		t.Fatal(err)
	}
	if err := startupValue(key, exe, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := key.GetStringValue("Shixin"); !errors.Is(err, registry.ErrNotExist) {
		t.Fatal("startup command remains", err)
	}
	if err := startupValue(key, exe, true); err != nil {
		t.Fatal("repeated cleanup", err)
	}
}
