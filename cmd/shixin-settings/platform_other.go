//go:build !darwin && !windows

package main

// No front yet (Linux comes with fcitx5, DESIGN §10); the settings app
// still builds and shows the macOS guide's wording.
var plat = platform{
	front: func() string { return "" },
	guide: macGuide,
}
