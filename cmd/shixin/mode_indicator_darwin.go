package main

import (
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shixin/internal/ipc"
	"shixin/internal/ui/modeview"
	"shixin/internal/ui/skin"
)

type modeImageKey struct {
	skin        *skin.Skin
	dark, ascii bool
}

var modeBubble struct {
	state    modeIndicator // main thread only
	window   *mygo.Window
	images   map[modeImageKey]*ui.Bitmap
	callback uintptr
	args     [3]uintptr // immutable after startup; shared with the timer goroutine
	timer    *time.Timer
	queued   bool
	shown    bool
	paint    paintWait // main thread only
}

func newModeWindow(tray *mygo.Tray) {
	modeBubble.images = make(map[modeImageKey]*ui.Bitmap, 4*len(skin.All))
	for i := range skin.All {
		for _, dark := range []bool{false, true} {
			for _, ascii := range []bool{false, true} {
				key := modeImageKey{&skin.All[i], dark, ascii}
				img := ui.Render(func(c *ui.Context) {
					modeview.Draw(c, ascii, key.skin.Palette(dark))
				}, modeview.Extent, modeview.Extent, 2)
				modeBubble.images[key] = ui.NewBitmap(img)
			}
		}
	}
	modeBubble.window = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray: tray, Hidden: true, Width: modeview.Extent, Height: modeview.Extent,
		Content: ui.View(modeBubbleView),
	})
	modeBubble.window.SetIgnoreMouseEvents(true)
	modeBubble.callback = purego.NewCallback(func(uintptr) uintptr {
		updateModeBubble()
		return 0
	})
	modeBubble.args = [3]uintptr{mainQueue, 0, modeBubble.callback}
	// Allocate the timer once. Its goroutine only queues a UI callback; it
	// never reads main-thread state or touches either window.
	modeBubble.timer = time.AfterFunc(time.Hour, dispatchModeBubble)
	modeBubble.timer.Stop()
}

func modeBubbleView(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	l := &cand.look
	key := modeImageKey{l.skin, l.dark(c), modeBubble.state.ascii}
	ui.Image(c, modeBubble.images[key]).Size(modeview.Extent, modeview.Extent)
}

// dispatchModeBubble is safe on the timer goroutine too. No Go callback is
// allocated here; dispatch_async_f always uses the startup callback.
func dispatchModeBubble() {
	purego.SyscallN(dispatchAsyncF, modeBubble.args[:]...)
}

func queueModeBubble() {
	if modeBubble.window != nil && !modeBubble.queued {
		modeBubble.queued = true
		dispatchModeBubble()
	}
}

// focusModeIndicator runs when client, a field of app, becomes the text
// field (activation or a new proxy). The caller reads app once for this
// and the engine's focus.
func focusModeIndicator(client uintptr, app string) {
	changed := modeBubble.state.focusApp(client, app, time.Now())
	if debug {
		m := &modeBubble.state
		logf("debug: mode focus %s (client %v): update %v, switched %v, pending %v", app, client != 0, changed, m.switched, m.pending)
	}
	if changed {
		queueModeBubble()
	}
}

// leaveModeIndicator runs when the text field goes away (deactivation).
func leaveModeIndicator() {
	changed := modeBubble.state.left(time.Now())
	if debug {
		logf("debug: mode left %s: update %v", modeBubble.state.leftApp, changed)
	}
	if changed {
		queueModeBubble()
	}
}

// selectModeIndicator runs when 拾新 becomes the input source.
func selectModeIndicator() {
	if modeBubble.state.selected() {
		queueModeBubble()
	}
}

func dismissModeBubble() {
	if modeBubble.state.dismiss() {
		queueModeBubble()
	}
}

// observeModeResult runs with the existing result application. Local clears
// and disconnects use an empty result (Ctx 0) and must not invent a mode.
func observeModeResult(r *ipc.Result) {
	if r.Ctx != 0 && modeBubble.state.observe(r.Flags&ipc.ResultASCII != 0) {
		if debug {
			logf("debug: mode result: show ascii %v", modeBubble.state.ascii)
		}
		queueModeBubble()
	}
	if len(r.Candidates) > 0 {
		dismissModeBubble() // candidates always take priority
	}
}

// updateModeBubble is a separate dispatch callback, after handleEvent: has
// returned. Only here may the bubble query the caret, move or show its window.
func updateModeBubble() {
	modeBubble.queued = false
	m := &modeBubble.state
	refresh := m.pending
	now := time.Now()
	// A field that left (imkClient and m.client both 0) is the grace's:
	// visible ends it.
	if imkClient != m.client || cand.shown || imkClient != 0 && focusedSecureInput() {
		if debug && (m.pending || !m.until.IsZero()) {
			logf("debug: mode dismissed: client match %v, candidates %v", imkClient == m.client, cand.shown)
		}
		m.dismiss()
	}
	if !m.visible(now) {
		if debug && modeBubble.shown {
			logf("debug: mode hidden")
		}
		modeBubble.timer.Stop()
		if modeBubble.shown {
			modeBubble.window.Hide()
			modeBubble.shown = false
		}
		return
	}
	// Query afresh on each notification; the candidate cache belongs to a
	// composition and can point at a different caret after a mode switch.
	if (refresh || !modeBubble.shown) && imkClient != 0 {
		var caret nsRect
		send(imkClient, selAttributesAt, 0, uintptr(unsafe.Pointer(&caret)))
		refreshCandidateScreens()
		switch {
		case usableCaret(candidateRect(caret), cand.screens):
			positionBesideBadge(modeBubble.window, caret, modeview.Size, modeview.Size, m.beside)
		case !modeBubble.shown:
			// A bubble away from the caret would only distract.
			if debug {
				logf("debug: unusable caret {%g %g %g %g} for the mode", caret.X, caret.Y, caret.W, caret.H)
			}
			return
		}
		// A shown bubble stays where it is when the caret is unusable.
		modeBubble.window.Invalidate()
		modeBubble.window.ShowInactive()
		modeBubble.shown = true
		modeBubble.paint.start()
		if debug {
			logf("debug: mode shown, ascii %v, %s", m.ascii, windowState(modeBubble.window))
		}
	}
	waiting := modeBubble.paint.waiting
	if modeBubble.paint.check(m, windowVisible(modeBubble.window), now) {
		purego.SyscallN(dispatchAfterF, uintptr(dispatchTime(0, caretRetryDelay*1e6)), mainQueue, 0, modeBubble.callback)
	} else if debug && waiting && modeBubble.paint.checks > 0 {
		logf("debug: mode visible after %d frame checks", modeBubble.paint.checks)
	}
	modeBubble.timer.Reset(time.Until(m.deadline()))
}
