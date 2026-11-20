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
	defaultFont  string   // c.Theme().Font from candView
	locale       string   // c.Locale() from candView
	screens      []candidateScreen
	callback     uintptr
	args         [3]uintptr // immutable arguments for dispatch_async_f
	queued       bool
	framePending bool
	// The panel's size in the last measurement (retained for diagnostics/tests).
	lastW, lastH float64
	updating     int // depth of updateCandidateWindow, which can nest
	// wanted is the last bounds requested via SetBounds; bounds is the actual
	// window bounds macOS applied (matching c.Size() in candView).
	wanted, bounds candidateBounds
	mask           candidateSurfaceMask
	drawnGen       uint64
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
	cand.flyout = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:    tray,
		Hidden:  true,
		Width:   candWindowW,
		Height:  candWindowH,
		Shadow:  false,
		Content: ui.View(candView),
	})
	if actual := cand.flyout.Bounds(); actual.Width > 0 && actual.Height > 0 {
		cand.bounds = candidateBounds{X: actual.X, Y: actual.Y, W: actual.Width, H: actual.Height}
	} else {
		cand.bounds = candidateBounds{W: candWindowW, H: candWindowH}
	}
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

func candStyle(dark bool) candview.Style {
	l := &cand.look
	style := candview.Style{Skin: l.skin, Dark: dark, FontFamily: l.fontFamily, FontSize: l.fontSize, Vertical: l.vertical}
	refreshCandidateScreens()
	work := candidateRect{W: candWindowW, H: candWindowH}
	if len(cand.screens) > 0 {
		work = cand.screens[candidateScreenAt(cand.caret.caret, cand.screens)].work
	}
	style.MaxWidth, style.MaxHeight = candidateCardBounds(work, candWindowW, candWindowH, shadowPad)
	return style
}

func candView(c *ui.Context) {
	cand.framePending = false
	cand.defaultFont = c.Theme().Font
	cand.locale = c.Locale()
	c.Root().Background(ui.Transparent).Padding(shadowPad) // room for the shadow
	cand.pending.apply(&cand.state)
	if len(cand.state.Items) == 0 {
		if debug && cand.shown {
			logf("debug: candidates frame: empty while shown")
		}
		return
	}
	style := candStyle(cand.look.dark(c))
	w, h := candview.Measure(c, &cand.state, &style)
	cand.lastW, cand.lastH = float64(w), float64(h)
	vw, vh := c.Size()
	// Never call SetBounds from inside a UI frame: MyGo's runFrame already
	// captured the old viewport size, so resizing mid-frame paints an old-sized
	// drawable into a new-sized CAMetalLayer (stretching it for a frame). If
	// look, theme, locale or display geometry changed bounds since
	// updateCandidateWindow, queue an update to apply the bounds after this frame.
	sizeMismatch := false
	if cand.shown {
		if b, _, ok := candidateWindowBounds(cand.caret.caret, cand.screens, float64(w), float64(h), candWindowW, candWindowH, shadowPad); ok && b != cand.wanted {
			sizeMismatch = b.W != cand.wanted.W || b.H != cand.wanted.H
			queueCandidateUpdate()
		}
	}
	if !sizeMismatch && cand.mask.revealForFrame(cand.shown, true, vw, vh) {
		maskCandidateSurface(cand.flyout, false)
		if debug {
			logf("debug: candidates unmasked at %gx%g", vw, vh)
		}
	}
	if debug {
		logf("debug: candidates frame: %d items, %gx%g, view %gx%g, shown %v, masked %v, %s",
			len(cand.state.Items), w, h, vw, vh, cand.shown, cand.mask.masked, windowState(cand.flyout))
	}
	if cand.mask.masked {
		// Leave an unready/mismatched frame transparent (important before the
		// first GPU frame creates CAMetalLayer and under software rendering).
		return
	}
	candview.Draw(c, &cand.state, &style, selectCandidate)
	cand.drawnGen++
}

func selectCandidate(i int) {
	core.SetSecure(focusedSecureInput())
	core.Select(i)
}

var (
	screenRect           func(uintptr, objc.SEL) candidateRect
	selScreens           = objc.RegisterName("screens")
	selCount             = objc.RegisterName("count")
	selObjectAtIndex     = objc.RegisterName("objectAtIndex:")
	selFrame             = objc.RegisterName("frame")
	selVisibleFrame      = objc.RegisterName("visibleFrame")
	selContentView       = objc.RegisterName("contentView")
	selSubviews          = objc.RegisterName("subviews")
	selLayer             = objc.RegisterName("layer")
	selSublayers         = objc.RegisterName("sublayers")
	selIsKindOfClass     = objc.RegisterName("isKindOfClass:")
	selSetHidden         = objc.RegisterName("setHidden:")
	selSetContents       = objc.RegisterName("setContents:")
	selBegin             = objc.RegisterName("begin")
	selCommit            = objc.RegisterName("commit")
	selSetDisableActions = objc.RegisterName("setDisableActions:")
)

// Refresh on each update so Dock and display arrangement changes are honored.
// This runs on the UI callback/frame, outside the app's key callback.
func refreshCandidateScreens() {
	list := send(uintptr(objc.GetClass("NSScreen")), selScreens)
	n := send(list, selCount)
	cand.screens = cand.screens[:0]
	for i := uintptr(0); i < n; i++ {
		s := send(list, selObjectAtIndex, i)
		cand.screens = append(cand.screens, candidateScreen{screenRect(s, selFrame), screenRect(s, selVisibleFrame)})
	}
}

// positionBesideBadge places the transient mode bubble at the caret, or beside
// macOS's input source badge (modeBesideBadge). Main thread only.
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
	window.SetPosition(x, y)
}

// candidateLayers returns the backing layer of MyGo's surface view and its
// CAMetalLayer sublayer (0 when software rendering or before the first frame).
func candidateLayers(win *mygo.Window) (surfaceLayer, metalLayer uintptr) {
	w := windowHandle(win)
	if w == 0 {
		return 0, 0
	}
	cv := send(w, selContentView)
	if cv == 0 {
		return 0, 0
	}
	metalClass := uintptr(objc.GetClass("CAMetalLayer"))
	subs := send(cv, selSubviews)
	n := send(subs, selCount)
	for i := uintptr(0); i < n; i++ {
		v := send(subs, selObjectAtIndex, i)
		l := send(v, selLayer)
		if l == 0 {
			continue
		}
		surfaceLayer = l
		if metalClass == 0 {
			continue
		}
		ls := send(l, selSublayers)
		m := send(ls, selCount)
		for j := uintptr(0); j < m; j++ {
			sl := send(ls, selObjectAtIndex, j)
			if byte(send(sl, selIsKindOfClass, metalClass)) != 0 {
				return surfaceLayer, sl
			}
		}
	}
	return surfaceLayer, 0
}

// maskCandidateSurface hides or reveals the candidate window's rendered layer
// inside a non-animated Core Animation transaction. Hiding CAMetalLayer (rather
// than the surface view or NSWindow) keeps AppKit's display cycle, CADisplayLink
// and occlusionState notifications intact while preventing stale or stretched
// drawables from appearing before the target-size frame commits atomically with
// the reveal. Tests may replace this hook.
var maskCandidateSurface = func(win *mygo.Window, masked bool) {
	sl, ml := candidateLayers(win)
	tx := uintptr(objc.GetClass("CATransaction"))
	if tx == 0 {
		return
	}
	if ml != 0 {
		var v uintptr
		if masked {
			v = 1
		}
		send(tx, selBegin)
		send(tx, selSetDisableActions, 1)
		send(ml, selSetHidden, v)
		send(tx, selCommit)
		return
	}
	// Software renderer fallback (PresentPixels writes surfaceLayer.contents).
	if masked && sl != 0 {
		send(tx, selBegin)
		send(tx, selSetDisableActions, 1)
		send(sl, selSetContents, 0)
		send(tx, selCommit)
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

func hideCandidateWindow() {
	cand.caret.reset()
	cand.framePending = false
	cand.pending.visible, cand.pending.dirty = false, false
	cand.state.Clear()
	if cand.shown {
		cand.shown = false // before the native call, which may nest an update
		if cand.mask.beginHide() {
			maskCandidateSurface(cand.flyout, true)
		}
		cand.flyout.Hide()
	}
}

// updateCandidateWindow is a separate main-queue callback, after handleEvent
// has returned. Frames consume only the latest display. Measuring, sizing and
// positioning all happen here before ShowInactive or Invalidate so candView
// never resizes the window mid-frame and stale/stretched drawables never show.
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
		hideCandidateWindow()
		return
	}
	if !cand.caret.valid {
		readCaret()
		if !cand.pending.visible || imkClient == 0 || focusedSecureInput() {
			// An update nested in the caret query hid the candidates: don't
			// show an emptied window.
			if debug {
				logf("debug: candidates hidden during the caret query; not showing")
			}
			return
		}
	}
	cand.pending.apply(&cand.state)
	if len(cand.state.Items) == 0 {
		hideCandidateWindow()
		return
	}
	style := candStyle(false)
	w, h := candview.MeasureWith(&cand.state, &style, cand.defaultFont, cand.locale)
	cand.lastW, cand.lastH = float64(w), float64(h)
	b, si, ok := candidateWindowBounds(cand.caret.caret, cand.screens, float64(w), float64(h), candWindowW, candWindowH, shadowPad)
	beforeGen := cand.drawnGen
	target := cand.bounds
	if ok {
		target = b
	}
	if cand.mask.prepareShowOrResize(cand.shown, cand.bounds, target) {
		maskCandidateSurface(cand.flyout, true)
	}
	if ok && (!cand.shown || b != cand.wanted) {
		// SetBounds moves and sizes the native window at once. MyGo's
		// flyout sizing (SetContentSize) re-anchors to the tray icon that
		// main_darwin.go destroyed at startup.
		cand.wanted = b
		cand.bounds = b
		cand.flyout.SetBounds(mygo.Rectangle{X: b.X, Y: b.Y, Width: b.W, Height: b.H})
		if actual := cand.flyout.Bounds(); actual.Width > 0 && actual.Height > 0 {
			cand.bounds = candidateBounds{X: actual.X, Y: actual.Y, W: actual.Width, H: actual.Height}
		}
		if debug && (b.X != cand.loggedX || b.Y != cand.loggedY) {
			cand.loggedX, cand.loggedY = b.X, b.Y
			s := &cand.screens[si]
			logf("debug: candidates at %d,%d (%gx%g, canvas %dx%d) for caret {%g %g %g %g} on screen %d of %d, work %+v",
				b.X, b.Y, w, h, cand.bounds.W, cand.bounds.H, cand.caret.caret.X, cand.caret.caret.Y, cand.caret.caret.W, cand.caret.caret.H, si, len(cand.screens), s.work)
		}
		if !cand.pending.visible || imkClient == 0 || focusedSecureInput() {
			return
		}
		cand.mask.syncActualSize(cand.bounds.W, cand.bounds.H)
	} else {
		cand.mask.syncActualSize(cand.bounds.W, cand.bounds.H)
	}
	if !cand.shown {
		cand.shown = true // before the native call, which may nest an update
		cand.flyout.ShowInactive()
		if debug {
			x, y := cand.flyout.Position()
			logf("debug: candidates shown at %d,%d, visible %v, masked %v, %s", x, y, cand.flyout.IsVisible(), cand.mask.masked, windowState(cand.flyout))
		}
		if !cand.pending.visible || imkClient == 0 || focusedSecureInput() {
			return
		}
	}
	if (cand.drawnGen == beforeGen || cand.mask.masked) && !cand.framePending {
		cand.framePending = true
		cand.flyout.Invalidate()
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
