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
	// Size is the visible card's width and height in points: about a line
	// of text, so it reads as a hint beside the caret.
	Size = 26
	// Margin leaves room for the shadow outside the visible card.
	Margin = 12
	// Extent includes the card and its shadow margins.
	Extent = Size + 2*Margin
)

// Draw builds a fixed-size bubble on the candidate skin's background: 中 in
// its highlight color, 英 in its text color.
func Draw(c *ui.Context, ascii bool, p *skin.Palette) {
	label, fg := i18n.T("mode.chinese"), p.Highlight
	if ascii {
		label, fg = i18n.T("mode.english"), p.Text
	}
	c.Root().Background(ui.Transparent).Padding(Margin)
	ui.Box(c).Size(Size, Size).Radius(7).Background(p.Background).Border(1, p.Border).
		Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.16)).Center().Label(i18n.T("mode.indicator")).Children(func() {
		ui.Text(c, label).FontSize(14).FontWeight(600).NoWrap().TextColor(fg)
	})
}
