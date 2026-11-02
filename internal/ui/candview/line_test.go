package candview

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Measuring and painting must share native baseline geometry even for fonts
// with tall leading or CJK/emoji fallback. Compare the cached paint path with
// the original whole rich line, including mixed directions and line breaks.
func TestCandidateLineMatchesRichLayout(t *testing.T) {
	for _, family := range []string{"", "Menlo", "Songti SC", "Zapfino", "Gabriola"} {
		for i, item := range []Item{
			{Label: "1", Text: "拾新", Comment: "comment"},
			{Label: "2", Text: "🆕", Comment: "emoji"},
			{Label: "3", Text: "مرحبا", Comment: "hello"},
			{Label: "4", Text: "abc אבג", Comment: "note"},
			{Label: "5", Text: "测试", Comment: "two\nlines"},
		} {
			t.Run(fmt.Sprintf("%s/%d", family, i), func(t *testing.T) {
				font := ui.Font{Family: family, Size: 28, Lang: "zh-Hans"}
				geometry := ui.ShapeRichText(rowSpans(item, font.Size, ui.Color{}, ui.Color{}), font, 0)
				line := newCandidateLine(item, font, geometry)
				w, h := geometry.Size()
				for _, highlighted := range []bool{false, true} {
					text, muted := ui.Hex("#111111"), ui.Hex("#666666")
					if highlighted {
						text, muted = ui.Hex("#ffffff"), ui.Hex("#cccccc")
					}
					original := ui.ShapeRichText(rowSpans(item, font.Size, text, muted), font, 0)
					render := func(draw func(*ui.Painter, ui.Rect)) []byte {
						return ui.Render(func(c *ui.Context) {
							c.Root().Background(ui.Hex("#808080")).Padding(8)
							ui.Box(c).Size(w, h).Draw(draw)
						}, int(math.Ceil(float64(w)))+16, int(math.Ceil(float64(h)))+16, 2).Pix
					}
					want := render(func(p *ui.Painter, r ui.Rect) { p.TextLayout(original, r.X, r.Y, text) })
					got := render(line.painter(text, muted, highlighted))
					if !bytes.Equal(got, want) {
						t.Fatalf("cached painting differs from rich baseline, highlighted=%v fallback=%v", highlighted, line.fallback)
					}
				}
			})
		}
	}
}
