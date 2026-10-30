// Package fonts prepares candidate fonts outside input-event handling.
package fonts

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Families lists the installed families once, when a UI process starts.
func Families() []string {
	var names []string
	for _, f := range ui.FontFamilies() {
		names = append(names, f.Name)
	}
	return names
}

// Resolve returns the installed spelling of family, or the system default
// (empty). Keep the saved preference when moving between machines.
func Resolve(family string, families []string) string {
	for _, name := range families {
		if strings.EqualFold(name, family) {
			return name
		}
	}
	return ""
}

// Warm loads the selected font and representative Latin, CJK, symbol and
// emoji fallback glyphs at both panel sizes and display scales. Call before
// registering the input method, or on the settings watcher goroutine before
// publishing a new appearance; never call from a key callback or UI frame.
func Warm(family string, size float32) {
	const sample = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 ,.;:'!?，。；：！？拾新输入法你好中国中文测试候选拼音繁體輸入漢字⟵→✓🆕😀"
	for _, scale := range []float32{1, 2} {
		ui.Render(func(c *ui.Context) {
			ui.Column(c).Width(1100).Children(func() {
				for _, points := range []float32{size, size * 0.76} {
					ui.Text(c, sample).Font(family).FontSize(points)
				}
			})
		}, 1100, 240, scale)
	}
}
