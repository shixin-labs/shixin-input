//go:build !windows

package settings

// DevApps are terminals and code editors: offered as English-mode rules in
// one click, and without auto spacing by default. These are macOS bundle
// ids; Windows has its own list (devapps_windows.go).
var DevApps = []string{
	"com.apple.Terminal", "com.googlecode.iterm2", "com.mitchellh.ghostty", "dev.warp.Warp-Stable",
	"com.microsoft.VSCode", "com.apple.dt.Xcode", "dev.zed.Zed", "com.todesktop.230313mzl4w4u92",
}
