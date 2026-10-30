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

func focusModeIndicator(client uintptr) {
	if modeBubble.state.focus(client) {
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
	if imkClient == 0 || imkClient != m.client || cand.shown || focusedSecureInput() {
		m.dismiss()
	}
	if !m.visible(now) {
		modeBubble.timer.Stop()
		if modeBubble.shown {
			modeBubble.window.Hide()
			modeBubble.shown = false
		}
		return
	}
	// Query afresh on each notification; the candidate cache belongs to a
	// composition and can point at a different caret after a mode switch.
	if refresh || !modeBubble.shown {
		var caret nsRect
		send(imkClient, selAttributesAt, 0, uintptr(unsafe.Pointer(&caret)))
		positionCaretWindow(modeBubble.window, caret, modeview.Size, modeview.Size)
		modeBubble.window.Invalidate()
		modeBubble.window.ShowInactive()
		modeBubble.shown = true
	}
	modeBubble.timer.Reset(time.Until(m.until))
}
