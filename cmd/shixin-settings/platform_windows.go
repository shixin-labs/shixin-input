package main

import (
	"os"
	"path/filepath"
)

var plat = platform{
	front: func() string {
		// Shixin.exe sits beside shixin-settings.exe.
		exe, err := os.Executable()
		if err != nil {
			return ""
		}
		p := filepath.Join(filepath.Dir(exe), "Shixin.exe")
		if _, err := os.Stat(p); err != nil {
			return ""
		}
		return p
	},
	// The Windows front doesn't answer -added yet, Windows asks no consent,
	// and releases are installed by hand until there's a signed installer.
	inputSettings: "ms-settings:regionlanguage",
	guide:         windowsGuide,
}
