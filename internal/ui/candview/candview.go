// Package candview draws the candidate panel (DESIGN §3.6). The macOS front
// draws it in the candidate window, and the settings app draws the same
// view as its live skin preview.
package candview

import (
	"github.com/egoist/mygo/ui"

	"shixin/internal/ui/skin"
)

// Item is one candidate.
type Item struct {
	Label, Text, Comment string
}

// State is what the panel shows. Do not copy it after its first Apply or Draw.
type State struct {
	Preedit      string
	Items        []Item
	Highlight    int
	measured     map[measurement]extent
	measureBytes int
	strings      *stringCache
	clicks       []func()
	onSelect     func(int)
	reveal       bool
	viewportSeen bool
}

// Measurements are retained across pages and font changes, with the same
// limits as the owned-string cache. Shaping a new string may allocate on a
// UI frame; revisiting warmed text must not allocate.
type measurement struct {
	item         Item
	font, locale string
	size         float32
	row          bool
}

type extent struct{ w, h float32 }

func (st *State) measure(c *ui.Context, item Item, size float32, font string, row bool) (float32, float32) {
	if font == "" {
		font = c.Theme().Font
	}
	key := measurement{item, font, c.Locale(), size, row}
	if e, ok := st.measured[key]; ok {
		return e.w, e.h
	}
	f := ui.Font{Family: font, Size: size, Lang: key.locale}
	var w, h float32
	if row {
		// Baseline alignment takes the largest ascent AND descent across
		// the label, candidate and comment, including fallback fonts. A
		// single shaped line with these sizes has exactly that height.
		w, h = ui.ShapeRichText([]ui.Span{
			{Text: item.Label, Size: size * 0.76},
			{Text: item.Text, Size: size},
			{Text: item.Comment, Size: size * 0.76},
		}, f, 0).Size()
	} else {
		// Context.MeasureText keeps the page's base font as a minimum
		// line box. Shape with the element's own font and size instead.
		w, h = ui.ShapeText(item.Text, f, 0).Size()
	}
	n := len(item.Label) + len(item.Text) + len(item.Comment) + len(font) + len(key.locale)
	if n <= stringCacheBytes {
		if st.measured == nil {
			st.measured = make(map[measurement]extent)
		}
		if len(st.measured) >= stringCacheEntries || st.measureBytes+n > stringCacheBytes {
			clear(st.measured)
			st.measureBytes = 0
		}
		st.measured[key] = extent{w, h}
		st.measureBytes += n
	}
	return w, h
}

// Style is how the panel looks.
type Style struct {
	Skin                *skin.Skin
	Dark                bool
	FontFamily          string  // empty inherits the system UI font
	FontSize            float32 // candidate text, in points
	Vertical            bool
	MaxWidth, MaxHeight float32 // optional card bounds, in DIPs; overflow scrolls
}

// Labels are the default select-key labels.
var Labels = [10]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"}

// Measure returns the visible card's size, excluding its shadow. Text uses
// retained font-aware measurement, with the same sizes, gaps and padding as Draw.
func naturalSize(c *ui.Context, st *State, style *Style) (width, height float32) {
	size := max(style.FontSize, 10)
	small, pad := size*0.76, size*0.35
	for i := range st.Items {
		it := &st.Items[i]
		w, _ := st.measure(c, Item{Text: it.Label}, small, style.FontFamily, false)
		tw, _ := st.measure(c, Item{Text: it.Text}, size, style.FontFamily, false)
		w += tw + size*0.22
		if it.Comment != "" {
			cw, _ := st.measure(c, Item{Text: it.Comment}, small, style.FontFamily, false)
			w += cw + size*0.22
		}
		_, h := st.measure(c, *it, size, style.FontFamily, true)
		w, h = w+2*pad, h+2*pad*0.45
		if style.Vertical {
			width, height = max(width, w), height+h
			if i > 0 {
				height++
			}
		} else {
			width, height = width+w, max(height, h)
			if i > 0 {
				width += 2
			}
		}
	}
	if st.Preedit != "" {
		w, h := st.measure(c, Item{Text: st.Preedit}, small, style.FontFamily, false)
		width, height = max(width, w+2*pad), height+h+pad*0.6
	}
	return width + 2*(pad+2) + 2, height + 2*(pad+1) + 2
}

// Measure returns the visible card size, bounded by the available viewport.
func Measure(c *ui.Context, st *State, style *Style) (width, height float32) {
	width, height = naturalSize(c, st, style)
	if style.MaxWidth > 0 {
		width = min(width, style.MaxWidth)
	}
	if style.MaxHeight > 0 {
		height = min(height, style.MaxHeight)
	}
	return
}

// Draw builds the panel into c: a rounded card sized to its content, at
// the top left of its parent. onSelect, if not nil, is called with the
// index of a clicked candidate.
func Draw(c *ui.Context, st *State, style *Style, onSelect func(int)) ui.Element {
	st.onSelect = onSelect
	p := style.Skin.Palette(style.Dark)
	size := max(style.FontSize, 10)
	small := size * 0.76
	pad := size * 0.35
	naturalW, naturalH := naturalSize(c, st, style)
	w, h := naturalW, naturalH
	if style.MaxWidth > 0 {
		w = min(w, style.MaxWidth)
	}
	if style.MaxHeight > 0 {
		h = min(h, style.MaxHeight)
	}
	overflow := w < naturalW || h < naturalH
	reveal := st.reveal || !st.viewportSeen
	card := ui.Column(c).AlignSelf(ui.Start).Background(p.Background).Radius(size*0.6).Border(1, p.Border).
		Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.16)).Padding(pad+1, pad+2).Gap(pad * 0.6)
	if style.FontFamily != "" {
		card.Font(style.FontFamily)
	}
	content := func() {
		if st.Preedit != "" {
			ui.Text(c, st.Preedit).FontSize(small).TextColor(p.Muted).NoWrap().Padding(0, pad)
		}
		var list ui.Element
		if style.Vertical {
			list = ui.Column(c).Gap(1)
		} else {
			list = ui.Row(c).Gap(2)
		}
		list.Children(func() {
			for i := range st.Items {
				it := &st.Items[i]
				hl := i == st.Highlight
				text, muted := p.Text, p.Muted
				row := ui.Row(c).Gap(size*0.22).AlignItems(ui.Baseline).Padding(pad*0.45, pad).Radius(size * 0.4)
				if hl {
					row.Background(p.Highlight)
					text, muted = p.OnHigh, p.OnHigh.Alpha(0.78)
				}
				row.Children(func() {
					ui.Text(c, it.Label).FontSize(small).TextColor(muted).NoWrap()
					ui.Text(c, it.Text).FontSize(size).TextColor(text).NoWrap()
					if it.Comment != "" {
						ui.Text(c, it.Comment).FontSize(small).TextColor(muted).NoWrap()
					}
				})
				if onSelect != nil {
					row.OnClick(st.selectionHandler(i))
				}
				if overflow && hl && reveal {
					row.ScrollIntoView()
				}
			}
		})
	}
	if overflow {
		card.Size(w, h).Children(func() {
			ui.ScrollBoth(c).Size(max(1, w-2*(pad+2)-2), max(1, h-2*(pad+1)-2)).Children(func() {
				ui.Column(c).Width(naturalW - 2*(pad+2) - 2).Gap(pad * 0.6).Children(content)
			})
		})
	} else {
		card.Children(content)
	}
	st.reveal, st.viewportSeen = false, true
	return card
}
