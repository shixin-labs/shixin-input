// Package modeview draws the transient input-mode bubble. The macOS front
// pre-renders both modes in every skin/appearance at startup, keeping font
// loading and glyph rasterization off the key path.
package modeview

import (
	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
	"shixin/internal/ui/skin"
)

const (
	// Size is the visible card's width and height in points.
	Size = 44
	// Margin leaves room for the shadow outside the visible card.
	Margin = 12
	// Extent includes the card and its shadow margins.
	Extent = Size + 2*Margin
)

// Draw builds a fixed-size bubble using the candidate skin's palette.
func Draw(c *ui.Context, ascii bool, p *skin.Palette) {
	label, bg, fg := i18n.T("mode.chinese"), p.Highlight, p.OnHigh
	if ascii {
		label, bg, fg = i18n.T("mode.english"), p.Background, p.Text
	}
	c.Root().Background(ui.Transparent).Padding(Margin)
	ui.Box(c).Size(Size, Size).Radius(12).Background(bg).Border(1, p.Border).
		Shadow(0, 3, 10, 0, ui.RGBA(0, 0, 0, 0.18)).Center().Label(i18n.T("mode.indicator")).Children(func() {
		ui.Text(c, label).FontSize(22).FontWeight(600).NoWrap().TextColor(fg)
	})
}
