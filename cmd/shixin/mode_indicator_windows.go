package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shixin/internal/ipc"
	"shixin/internal/ui/modeview"
	"shixin/internal/ui/skin"
)

type windowsModeImage struct {
	skin        *skin.Skin
	dark, ascii bool
}

// The host owns and warms its hint window before accepting TSF traffic.
// The DLL only sends confirmed mode flags and a caret, never creates UI.
func (h *windowsHost) newModeWindow(tray *mygo.Tray) {
	h.modeImages = make(map[windowsModeImage]*ui.Bitmap, 4*len(skin.All))
	for i := range skin.All {
		for _, dark := range []bool{false, true} {
			for _, ascii := range []bool{false, true} {
				key := windowsModeImage{&skin.All[i], dark, ascii}
				img := ui.Render(func(c *ui.Context) { modeview.Draw(c, ascii, key.skin.Palette(dark)) }, modeview.Extent, modeview.Extent, 2)
				h.modeImages[key] = ui.NewBitmap(img)
			}
		}
	}
	h.modeWindow = mygo.NewFlyout(mygo.FlyoutOptions{Tray: tray, Hidden: true, Width: modeview.Extent, Height: modeview.Extent, Shadow: false, Content: ui.View(h.modeView)})
	h.modeWindow.SetIgnoreMouseEvents(true)
	h.modeTimer = time.AfterFunc(time.Hour, h.refresh)
	h.modeTimer.Stop()
}

func (h *windowsHost) modeView(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	p := &h.prefs
	key := windowsModeImage{skin.Get(p.Skin), p.Theme == "dark" || p.Theme == "system" && c.Theme().Dark, h.modeASCII}
	ui.Image(c, h.modeImages[key]).Size(modeview.Extent, modeview.Extent)
}

func (h *windowsHost) showMode(visible bool, until time.Time, caret ipc.Caret) {
	if !visible {
		h.modeTimer.Stop()
		h.modeWindow.Hide()
		return
	}
	positionWindowsFlyout(h.modeWindow, caret, modeview.Size, modeview.Size)
	h.modeWindow.Invalidate()
	h.modeWindow.ShowInactive()
	h.modeTimer.Reset(time.Until(until))
}
