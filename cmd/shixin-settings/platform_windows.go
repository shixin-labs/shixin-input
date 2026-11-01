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
	// Windows reports enablement without macOS's cache-repair operation.
	added:         true,
	inputSettings: "ms-settings:regionlanguage",
	guide:         windowsGuide,
}
