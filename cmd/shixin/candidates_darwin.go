package main

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shixin/internal/ipc"
	"shixin/internal/settings"
	"shixin/internal/ui/candview"
	"shixin/internal/ui/fonts"
	"shixin/internal/ui/skin"
)

// The candidate window (DESIGN §3.6) is a flyout created once at startup
// and only moved, shown and hidden afterwards. It is transparent; the panel
// is drawn at its top-left corner, with scrolling when its content exceeds
// the screen or flyout bounds. Clicks on the
// transparent part reach the window below. The panel is the same view the
// settings app previews skins with (internal/ui/candview).
const (
	candWindowW = 1400 // ten long candidates at the largest font size
	candWindowH = 560  // ten vertical rows at the largest font size
	shadowPad   = 12   // around the panel, for its shadow
)

var cand struct {
	flyout       *mygo.Window
	shown        bool
	caret        caretTracker
	state        candview.State
	pending      candidateUpdate
	look         look     // main thread
	fonts        []string // immutable after startup
	screens      []candidateScreen
	callback     uintptr
	args         [3]uintptr // immutable arguments for dispatch_async_f
	queued       bool
	framePending bool
	// The panel's size in the last frame, to place the window before it
	// shows: until the next frame measures the new candidates.
	lastW, lastH float64
	updating     int // depth of updateCandidateWindow, which can nest
	// canvasW and canvasH are the flyout's size (candidateCanvas).
	canvasW, canvasH int
	// The last position logged in debug mode.
	loggedX, loggedY int
}

// look is the candidate window's part of the settings.
type look struct {
	skin       *skin.Skin
	theme      string // "system", "light", "dark"
	fontFamily string
	fontSize   float32
	vertical   bool
}

// dark reports whether the panel draws in its dark palette.
func (l *look) dark(c *ui.Context) bool {
	return l.theme == "dark" || l.theme == "system" && c.Theme().Dark
}

func lookOf(s *settings.Settings) look {
	return look{skin: skin.Get(s.Skin), theme: s.Theme, fontFamily: fonts.Resolve(s.FontFamily, cand.fonts), fontSize: float32(s.FontSize), vertical: s.Layout == "vertical"}
}

func newCandidateWindow(tray *mygo.Tray, s *settings.Settings) {
	cand.fonts = fonts.Families()
	cand.look = lookOf(s)
	fonts.Warm(cand.look.fontFamily, cand.look.fontSize)
	cand.screens = make([]candidateScreen, 0, 8)
	cand.pending.init()
	initScreenRect()
	cand.canvasW, cand.canvasH = candWindowW, candWindowH
	cand.flyout = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:    tray,
		Hidden:  true,
		Width:   candWindowW,
		Height:  candWindowH,
		Shadow:  false,
		Content: ui.View(candView),
	})
	cand.callback = purego.NewCallback(func(uintptr) uintptr {
		updateCandidateWindow()
		return 0
	})
	cand.args = [3]uintptr{mainQueue, 0, cand.callback}
}

// initScreenRect binds screenRect. NSRect returns use objc_msgSend_stret
// on Intel, objc_msgSend on ARM.
func initScreenRect() {
	addr := msgSend
	if runtime.GOARCH == "amd64" {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		must(err)
		addr, err = purego.Dlsym(lib, "objc_msgSend_stret")
		must(err)
	}
	purego.RegisterFunc(&screenRect, addr)
}

// setLook applies changed appearance settings. Any goroutine.
func setLook(s *settings.Settings) {
	l := lookOf(s)
	// The watcher prepares fonts before the UI sees the new appearance.
	// In particular, changing the family never loads it inside handleEvent.
	fonts.Warm(l.fontFamily, l.fontSize)
	cand.flyout.Update(func() {
		cand.look = l
		modeBubble.window.Invalidate()
	})
}

func candView(c *ui.Context) {
	cand.framePending = false
	c.Root().Background(ui.Transparent).Padding(shadowPad) // room for the shadow
	cand.pending.apply(&cand.state)
	if len(cand.state.Items) == 0 {
		if debug && cand.shown {
			logf("debug: candidates frame: empty while shown")
		}
		return
	}
	l := &cand.look
	style := candview.Style{Skin: l.skin, Dark: l.dark(c), FontFamily: l.fontFamily, FontSize: l.fontSize, Vertical: l.vertical}
	refreshCandidateScreens()
	work := candidateRect{W: candWindowW, H: candWindowH}
	if len(cand.screens) > 0 {
		work = cand.screens[candidateScreenAt(cand.caret.caret, cand.screens)].work
	}
	style.MaxWidth, style.MaxHeight = candidateCardBounds(work, candWindowW, candWindowH, shadowPad)
	w, h := candview.Measure(c, &cand.state, &style)
	cand.lastW, cand.lastH = float64(w), float64(h)
	if debug {
		logf("debug: candidates frame: %d items, %gx%g, shown %v, %s", len(cand.state.Items), w, h, cand.shown, windowState(cand.flyout))
	}
	if cand.shown {
		positionCandidates(float64(w), float64(h))
	}
	candview.Draw(c, &cand.state, &style, selectCandidate)
}

func selectCandidate(i int) {
	core.SetSecure(focusedSecureInput())
	core.Select(i)
}

var (
	screenRect       func(uintptr, objc.SEL) candidateRect
	selScreens       = objc.RegisterName("screens")
	selCount         = objc.RegisterName("count")
	selObjectAtIndex = objc.RegisterName("objectAtIndex:")
	selFrame         = objc.RegisterName("frame")
	selVisibleFrame  = objc.RegisterName("visibleFrame")
)

// candView has already refreshed the work areas for this frame.
func positionCandidates(width, height float64) {
	positionOnCandidateScreens(cand.flyout, nsRect(cand.caret.caret), width, height)
}

// positionCaretWindow shares the same screen selection and edge clamping for
// candidates and the transient mode bubble. Main thread only.
func positionCaretWindow(window *mygo.Window, rect nsRect, width, height float64) {
	refreshCandidateScreens()
	positionOnCandidateScreens(window, rect, width, height)
}

// Refresh on each view update so Dock and display arrangement changes are
// honored. This runs on the UI frame, outside the app's key callback.
func refreshCandidateScreens() {
	list := send(uintptr(objc.GetClass("NSScreen")), selScreens)
	n := send(list, selCount)
	cand.screens = cand.screens[:0]
	for i := uintptr(0); i < n; i++ {
		s := send(list, selObjectAtIndex, i)
		cand.screens = append(cand.screens, candidateScreen{screenRect(s, selFrame), screenRect(s, selVisibleFrame)})
	}
}

func positionOnCandidateScreens(window *mygo.Window, rect nsRect, width, height float64) {
	positionBesideBadge(window, rect, width, height, false)
}

// positionBesideBadge places window at the caret like the candidates, or
// beside macOS's input source badge (modeBesideBadge).
func positionBesideBadge(window *mygo.Window, rect nsRect, width, height float64, beside bool) {
	if len(cand.screens) == 0 {
		return
	}
	caret := candidateRect(rect)
	s := &cand.screens[candidateScreenAt(caret, cand.screens)]
	if beside {
		caret = modeBesideBadge(caret, s.work, width)
	}
	x, y := candidatePosition(caret, s.work, width, height, cand.screens[0].frame.H, shadowPad)
	if window == cand.flyout {
		// SetBounds moves and sizes the native window at once. MyGo's
		// flyout sizing (SetContentSize) re-anchors to the tray icon that
		// main_darwin.go destroyed at startup.
		cand.canvasW, cand.canvasH = candidateCanvas(x, y, *s, cand.screens[0].frame.H, candWindowW, candWindowH)
		window.SetBounds(mygo.Rectangle{X: x, Y: y, Width: cand.canvasW, Height: cand.canvasH})
	} else {
		window.SetPosition(x, y)
	}
	if debug && window == cand.flyout && (x != cand.loggedX || y != cand.loggedY) {
		cand.loggedX, cand.loggedY = x, y
		logf("debug: candidates at %d,%d (%gx%g) for caret {%g %g %g %g} on screen %d of %d, work %+v",
			x, y, width, height, rect.X, rect.Y, rect.W, rect.H, candidateScreenAt(candidateRect(rect), cand.screens), len(cand.screens), s.work)
	}
}

// showCandidates runs inside Drain, including on an ASK/barrier key path.
// Copy the display into owned storage and queue the preallocated callback;
// never retain r, convert strings, invalidate a view or call the app here.
func showCandidates(r *ipc.Result) {
	if len(r.Candidates) == 0 {
		// Preserve composition boundaries even when a hide and the next
		// composition coalesce before their UI callback/frame can run.
		cand.caret.reset()
	}
	cand.pending.capture(r)
	queueCandidateUpdate()
}

func dispatchCandidateUpdate() {
	purego.SyscallN(dispatchAsyncF, cand.args[:]...)
}

func queueCandidateUpdate() {
	if cand.flyout != nil && !cand.queued {
		cand.queued = true
		dispatchCandidateUpdate()
	}
}

// updateCandidateWindow is a separate main-queue callback, after handleEvent
// has returned. Frames consume only the latest display. The caret query is
// still once per composition, and now also outside the app's key callback.
func updateCandidateWindow() {
	cand.queued = false
	// The app's calls below (the caret query) can run the main run loop,
	// and a queued update with it.
	cand.updating++
	defer func() { cand.updating-- }()
	if debug && cand.updating > 1 {
		logf("debug: nested candidate update (depth %d)", cand.updating)
	}
	if secure := focusedSecureInput(); !cand.pending.visible || imkClient == 0 || secure {
		if debug && (cand.shown || cand.pending.visible) {
			logf("debug: candidates hidden: visible %v, client %v, secure %v", cand.pending.visible, imkClient != 0, secure)
		}
		cand.caret.reset()
		cand.framePending = false
		cand.pending.visible, cand.pending.dirty = false, false
		cand.state.Clear()
		if cand.shown {
			cand.shown = false // before the native call, which may nest an update
			cand.flyout.Hide()
		}
		return
	}
	if !cand.caret.valid {
		readCaret()
		if !cand.pending.visible || imkClient == 0 {
			// An update nested in the caret query hid the candidates: don't
			// show an emptied window.
			if debug {
				logf("debug: candidates hidden during the caret query; not showing")
			}
			return
		}
	}
	if !cand.framePending {
		cand.framePending = true
		cand.flyout.Invalidate()
	}
	if !cand.shown {
		// Move the hidden window to this caret first: it still sits where
		// the last composition was, often in another app, and the frame
		// that positions it runs after it shows. The frame then fits the
		// new candidates' size.
		positionCaretWindow(cand.flyout, nsRect(cand.caret.caret), cand.lastW, cand.lastH)
		cand.shown = true // before the native call, which may nest an update
		cand.flyout.ShowInactive()
		if debug {
			x, y := cand.flyout.Position()
			logf("debug: candidates shown at %d,%d, visible %v, %s", x, y, cand.flyout.IsVisible(), windowState(cand.flyout))
		}
	}
}

// readCaret asks the app for this composition's caret (caretTracker); an
// unusable answer asks again a frame later. Main thread, outside the
// app's key callback.
func readCaret() {
	var caret nsRect
	send(imkClient, selAttributesAt, 0, uintptr(unsafe.Pointer(&caret)))
	refreshCandidateScreens()
	retry := cand.caret.read(candidateRect(caret), cand.screens)
	if debug {
		logf("debug: caret {%g %g %g %g}, usable %v", caret.X, caret.Y, caret.W, caret.H, cand.caret.valid)
	}
	if retry {
		if debug {
			logf("debug: unusable caret {%g %g %g %g}; asking again", caret.X, caret.Y, caret.W, caret.H)
		}
		purego.SyscallN(dispatchAfterF, uintptr(dispatchTime(0, caretRetryDelay*1e6)), mainQueue, 0, cand.callback)
	}
}

func hideCandidates() {
	var empty ipc.Result
	showCandidates(&empty)
}

// windowVisible reports whether macOS says some of window shows.
func windowVisible(window *mygo.Window) bool {
	const occlusionVisible = 1 << 1
	w := windowHandle(window)
	return w != 0 && send(w, selOcclusionState)&occlusionVisible != 0
}

// windowHandle is window's NSWindow, or 0 before it has one.
func windowHandle(window *mygo.Window) uintptr {
	if window == nil {
		return 0
	}
	return window.NativeHandle()
}

// windowState describes, for debug logs, how macOS sees window: whether
// any of it shows (occlusionState), its space, number and level.
func windowState(window *mygo.Window) string {
	w := window.NativeHandle()
	if w == 0 {
		return "no window"
	}
	const occlusionVisible = 1 << 1
	return fmt.Sprintf("occlusion visible %v, active space %v, number %d, level %d",
		send(w, selOcclusionState)&occlusionVisible != 0, send(w, selOnActiveSpace)&0xff != 0,
		send(w, selWindowNumber), send(w, selLevel))
}

var (
	selOcclusionState = objc.RegisterName("occlusionState")
	selOnActiveSpace  = objc.RegisterName("isOnActiveSpace")
	selWindowNumber   = objc.RegisterName("windowNumber")
	selLevel          = objc.RegisterName("level")
)
