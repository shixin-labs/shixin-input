package main

import (
	"os"
	"path/filepath"
)

var plat = platform{
	front: func() string {
		// Shixin.app/Contents/Helpers/Shixin Settings.app/Contents/MacOS →
		// Shixin.app/Contents/MacOS/Shixin.
		exe, err := os.Executable()
		if err != nil {
			return ""
		}
		p := filepath.Join(filepath.Dir(exe), "..", "..", "..", "..", "MacOS", "Shixin")
		if _, err := os.Stat(p); err != nil {
			return ""
		}
		return filepath.Clean(p)
	},
	added:      true,
	consent:    true,
	selfUpdate: true,
	// System Settings' input source editor, with + one click away.
	inputSettings: "x-apple.systempreferences:com.apple.Keyboard-Settings.extension?InputSources",
	refresh:       true,
	guide:         macGuide,
}
