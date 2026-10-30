package main

import (
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
// and only moved, shown and hidden afterwards. It is larger than any
// candidate panel and transparent; the panel is drawn at its top-left
// corner and sized to its content, so candidates never wrap. Clicks on the
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
	caret        nsRect
	caretValid   bool
	state        candview.State
	pending      candidateUpdate
	look         look     // main thread
	fonts        []string // immutable after startup
	screens      []candidateScreen
	callback     uintptr
	args         [3]uintptr // immutable arguments for dispatch_async_f
	queued       bool
	framePending bool
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
	// NSRect returns use objc_msgSend_stret on Intel, objc_msgSend on ARM.
	addr := msgSend
	if runtime.GOARCH == "amd64" {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		must(err)
		addr, err = purego.Dlsym(lib, "objc_msgSend_stret")
		must(err)
	}
	purego.RegisterFunc(&screenRect, addr)
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
		return
	}
	l := &cand.look
	style := candview.Style{Skin: l.skin, Dark: l.dark(c), FontFamily: l.fontFamily, FontSize: l.fontSize, Vertical: l.vertical}
	w, h := candview.Measure(c, &cand.state, &style)
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

// positionCandidates reads Cocoa work areas on the main thread. Refresh on
// each view update so a moved Dock or a changed display arrangement is honored.
func positionCandidates(width, height float64) {
	positionCaretWindow(cand.flyout, cand.caret, width, height)
}

// positionCaretWindow shares the same screen selection and edge clamping for
// candidates and the transient mode bubble. Main thread only.
func positionCaretWindow(window *mygo.Window, rect nsRect, width, height float64) {
	list := send(uintptr(objc.GetClass("NSScreen")), selScreens)
	n := send(list, selCount)
	cand.screens = cand.screens[:0]
	for i := uintptr(0); i < n; i++ {
		s := send(list, selObjectAtIndex, i)
		cand.screens = append(cand.screens, candidateScreen{screenRect(s, selFrame), screenRect(s, selVisibleFrame)})
	}
	if len(cand.screens) == 0 {
		return
	}
	caret := candidateRect(rect)
	s := &cand.screens[candidateScreenAt(caret, cand.screens)]
	x, y := candidatePosition(caret, s.work, width, height, cand.screens[0].frame.H, shadowPad)
	window.SetPosition(x, y)
}

// showCandidates runs inside Drain, including on an ASK/barrier key path.
// Copy the display into owned storage and queue the preallocated callback;
// never retain r, convert strings, invalidate a view or call the app here.
func showCandidates(r *ipc.Result) {
	if len(r.Candidates) == 0 {
		// Preserve composition boundaries even when a hide and the next
		// composition coalesce before their UI callback/frame can run.
		cand.caretValid = false
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
	if !cand.pending.visible || imkClient == 0 || focusedSecureInput() {
		cand.caretValid = false
		cand.framePending = false
		cand.pending.visible, cand.pending.dirty = false, false
		cand.state.Clear()
		if cand.shown {
			cand.flyout.Hide()
			cand.shown = false
		}
		return
	}
	if !cand.caretValid {
		send(imkClient, selAttributesAt, 0, uintptr(unsafe.Pointer(&cand.caret)))
		cand.caretValid = true
	}
	if !cand.framePending {
		cand.framePending = true
		cand.flyout.Invalidate()
	}
	if !cand.shown {
		cand.flyout.ShowInactive()
		cand.shown = true
	}
}

func hideCandidates() {
	var empty ipc.Result
	showCandidates(&empty)
}
