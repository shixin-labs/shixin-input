//go:build darwin

// Command m2-mouse-monitor checks whether a background (accessory) app can
// observe mouse presses in other apps with NSEvent's global monitor
// without any permission, which DESIGN §7.1's auto-spacing reset relies
// on. A key-down monitor runs alongside for comparison: macOS only
// delivers those to apps trusted for Accessibility / Input Monitoring.
// It logs to ~/Library/Logs/mouse-monitor.log and exits after -t.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

func init() { runtime.LockOSThread() } // AppKit on the main thread

func main() {
	dur := flag.Duration("t", 90*time.Second, "how long to watch")
	flag.Parse()
	home, _ := os.UserHomeDir()
	f, err := os.Create(filepath.Join(home, "Library", "Logs", "mouse-monitor.log"))
	if err != nil {
		panic(err)
	}
	logf := func(format string, a ...any) {
		fmt.Fprintf(f, time.Now().Format("15:04:05.000 ")+format+"\n", a...)
		f.Sync()
	}

	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		panic(err)
	}
	as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(err)
	}
	var axTrusted func() bool
	purego.RegisterLibFunc(&axTrusted, as, "AXIsProcessTrusted")

	app := objc.ID(objc.GetClass("NSApplication")).Send(objc.RegisterName("sharedApplication"))
	app.Send(objc.RegisterName("setActivationPolicy:"), 1) // accessory: no Dock icon, like an IME
	selType := objc.RegisterName("type")
	monitor := func(name string, mask uint64) objc.ID {
		blk := objc.NewBlock(func(_ objc.Block, ev objc.ID) {
			logf("%s event type %d", name, ev.Send(selType))
		})
		return objc.ID(objc.GetClass("NSEvent")).Send(objc.RegisterName("addGlobalMonitorForEventsMatchingMask:handler:"), mask, blk)
	}
	// NSEventMaskLeftMouseDown | RightMouseDown | OtherMouseDown; KeyDown.
	mouse := monitor("mouse", 1<<1|1<<3|1<<25)
	key := monitor("key", 1<<10)
	logf("macOS monitors: mouse %#x key %#x; AXIsProcessTrusted %v", mouse, key, axTrusted())
	go func() {
		time.Sleep(*dur)
		logf("done")
		os.Exit(0)
	}()
	app.Send(objc.RegisterName("run"))
}
