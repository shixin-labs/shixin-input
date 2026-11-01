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
	viewport     extent
	scroll       ui.ScrollState
	geometry     panelGeometry
}

type panelGeometry struct {
	font, locale string
	size         float32
	vertical     bool
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

type extent struct {
	w, h float32
	line *candidateLine
}

// candidateLine retains the exact rich line used for both measurement and
// painting. Independent label/text/comment boxes have different leading on
// some fallback fonts, so their flex baseline height cannot be measured as
// one rich line. Painting that retained line also avoids rebuilding joined
// rich-text strings on every typing frame.
type candidateLine struct {
	item        Item
	font        ui.Font
	text, muted [2]ui.Color
	draw        [2]func(*ui.Painter, ui.Rect)
}

func rowSpans(item Item, size float32, text, muted ui.Color) []ui.Span {
	spans := []ui.Span{
		{Text: item.Label, Size: size * 0.76, Color: muted},
		{Text: "\uFFFC", ObjectWidth: size * 0.22},
		{Text: item.Text, Size: size, Color: text},
		{Text: "\uFFFC", ObjectWidth: size * 0.22},
		{Text: item.Comment, Size: size * 0.76, Color: muted},
	}
	if item.Comment == "" {
		spans = spans[:3]
	}
	return spans
}

func (line *candidateLine) painter(text, muted ui.Color, highlighted bool) func(*ui.Painter, ui.Rect) {
	i := 0
	if highlighted {
		i = 1
	}
	if line.draw[i] == nil || line.text[i] != text || line.muted[i] != muted {
		layout := ui.ShapeRichText(rowSpans(line.item, line.font.Size, text, muted), line.font, 0)
		line.text[i], line.muted[i] = text, muted
		line.draw[i] = func(p *ui.Painter, r ui.Rect) { p.TextLayout(layout, r.X, r.Y, text) }
	}
	return line.draw[i]
}

func (st *State) measure(c *ui.Context, item Item, size float32, font string, row bool) extent {
	if font == "" {
		font = c.Theme().Font
	}
	key := measurement{item, font, c.Locale(), size, row}
	if e, ok := st.measured[key]; ok {
		return e
	}
	f := ui.Font{Family: font, Size: size, Lang: key.locale}
	var w, h float32
	if row {
		w, h = ui.ShapeRichText(rowSpans(item, size, ui.Color{}, ui.Color{}), f, 0).Size()
	} else {
		// Context.MeasureText keeps the page's base font as a minimum
		// line box. Shape with the element's own font and size instead.
		w, h = ui.ShapeText(item.Text, f, 0).Size()
	}
	e := extent{w: w, h: h}
	if row {
		e.line = &candidateLine{item: item, font: f}
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
		st.measured[key] = e
		st.measureBytes += n
	}
	return e
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

// naturalSize uses retained font-aware measurement with Draw's sizes, gaps
// and padding, excluding the shadow.
func naturalSize(c *ui.Context, st *State, style *Style) (width, height float32) {
	size := max(style.FontSize, 10)
	small, pad := size*0.76, size*0.35
	for i := range st.Items {
		it := &st.Items[i]
		e := st.measure(c, *it, size, style.FontFamily, true)
		w, h := e.w, e.h
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
		e := st.measure(c, Item{Text: st.Preedit}, small, style.FontFamily, false)
		width, height = max(width, e.w+2*pad), height+e.h+pad*0.6
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
	font := style.FontFamily
	if font == "" {
		font = c.Theme().Font
	}
	geometry := panelGeometry{font: font, locale: c.Locale(), size: size, vertical: style.Vertical}
	reveal := st.reveal || !st.viewportSeen || st.viewport != (extent{w: w, h: h}) || st.geometry != geometry
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
				row := ui.Row(c).Padding(pad*0.45, pad).Radius(size * 0.4)
				if hl {
					row.Background(p.Highlight)
					text, muted = p.OnHigh, p.OnHigh.Alpha(0.78)
				}
				e := st.measure(c, *it, size, style.FontFamily, true)
				paint := e.line.painter(text, muted, hl)
				row.Children(func() {
					ui.Box(c).Size(e.w, e.h).Label(it.Text).Draw(paint)
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
			ui.ScrollBoth(c).TrackScroll(&st.scroll).Size(max(1, w-2*(pad+2)-2), max(1, h-2*(pad+1)-2)).Children(func() {
				ui.Column(c).Width(naturalW - 2*(pad+2) - 2).Gap(pad * 0.6).Children(content)
			})
		})
	} else {
		card.Children(content)
	}
	st.reveal, st.viewportSeen = false, true
	st.viewport = extent{w: w, h: h}
	st.geometry = geometry
	return card
}
