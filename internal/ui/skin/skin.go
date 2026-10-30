// Package skin has the colors of the candidate window's built-in skins
// (DESIGN §6, 外观). Their ids and names are in settings.Skins.
package skin

import "github.com/egoist/mygo/ui"

// Palette is a skin's colors in one appearance.
type Palette struct {
	Background ui.Color
	Border     ui.Color
	Text       ui.Color // candidates
	Muted      ui.Color // preedit, labels, comments
	Highlight  ui.Color // the highlighted candidate's pill
	OnHigh     ui.Color // text on Highlight
}

// Skin is a skin in both appearances.
type Skin struct {
	ID          string
	Light, Dark Palette
}

// Palette returns the skin's palette for dark or light.
func (s *Skin) Palette(dark bool) *Palette {
	if dark {
		return &s.Dark
	}
	return &s.Light
}

func pal(bg, border, text, muted, high, onHigh string) Palette {
	return Palette{ui.Hex(bg), ui.Hex(border), ui.Hex(text), ui.Hex(muted), ui.Hex(high), ui.Hex(onHigh)}
}

// All lists the skins in settings.Skins order.
var All = []Skin{
	{"shixin",
		pal("#ffffff", "#e3e6ec", "#1d2129", "#8a909c", "#3268f0", "#ffffff"),
		pal("#24262b", "#383b42", "#eceef2", "#8b909a", "#4f83ff", "#ffffff")},
	{"mist",
		pal("#f3f5f9", "#dde2ea", "#2a303b", "#8e96a5", "#6b7fd7", "#ffffff"),
		pal("#262a33", "#3a3f4b", "#e3e7ee", "#8a92a1", "#8796f0", "#10131a")},
	{"ink",
		pal("#fbfaf7", "#e4e1d9", "#141414", "#8c8880", "#141414", "#fbfaf7"),
		pal("#141414", "#2e2e2e", "#efede8", "#8c8880", "#efede8", "#141414")},
	{"jade",
		pal("#f1f7f4", "#d5e6de", "#1d2b26", "#7f978d", "#2f8f76", "#ffffff"),
		pal("#18231f", "#2b3b35", "#dcebe4", "#7f978d", "#45b292", "#0d1714")},
	{"sakura",
		pal("#fff6f8", "#f5dbe2", "#33222a", "#a88a95", "#e0577f", "#ffffff"),
		pal("#2a1e23", "#433038", "#f4e3e9", "#a88a95", "#f07198", "#1f1216")},
	{"amber",
		pal("#fffaf1", "#f1e3c8", "#2e2416", "#a39275", "#d9861a", "#ffffff"),
		pal("#262016", "#3e3424", "#f3e8d4", "#a39275", "#f0a23a", "#1d160b")},
}

// Get returns the skin with id, or the default skin.
func Get(id string) *Skin {
	for i := range All {
		if All[i].ID == id {
			return &All[i]
		}
	}
	return &All[0]
}
