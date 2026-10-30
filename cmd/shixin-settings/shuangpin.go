package main

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"shixin/internal/settings"
)

func (a *app) shuangpinChart(c *ui.Context) {
	layout := settings.Shuangpin(a.s.Scheme)
	if layout == nil {
		a.chartShown = false
		return
	}
	ui.Modal(c, &a.chartShown, func() {
		p, t := colors(c), c.Theme()
		ui.Column(c).Width(572).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
				ids, names := choiceIDs(settings.Schemes)
				for i, id := range ids {
					if id == a.s.Scheme {
						ui.Text(c, names[i]).FontSize(16).FontWeight(650).Grow(1)
					}
				}
				ui.Button(c, T("s.g.chart.close")).OnClick(func() { a.chartShown = false })
			})
			_, height := c.Size()
			ui.Scroll(c).MaxHeight(height - 140).Children(func() {
				ui.Column(c).Gap(12).Children(func() {
					ui.Row(c).Gap(16).Children(func() {
						ui.Text(c, T("s.g.chart.initial")).FontSize(12).TextColor(t.Accent)
						ui.Text(c, T("s.g.chart.final")).FontSize(12).TextColor(t.Text)
					})
					ui.Column(c).Gap(6).Children(func() {
						for row, keys := range []string{"qwertyuiop", "asdfghjkl;", "zxcvbnm"} {
							ui.Row(c).Gap(5).Padding(0, 0, 0, float32(row)*12).Children(func() {
								for _, key := range layout.Keys {
									if !strings.Contains(keys, key.Key) {
										continue
									}
									ui.Column(c.Key(key.Key)).Width(49).Height(82).Shrink(0).Padding(6, 5).Gap(3).
										Background(p.faint).Border(1, p.border).Radius(6).Children(func() {
										ui.Text(c, strings.ToUpper(key.Key)).FontSize(12).FontWeight(650).TextColor(p.muted)
										ui.Text(c, key.Initial).FontSize(12).TextColor(t.Accent).Height(16)
										ui.Text(c, strings.Join(key.Finals, "\n")).FontSize(11.5).TextColor(t.Text).LineHeight(1.15)
									})
								}
							})
						}
					})
					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, T("s.g.chart.zero")).FontSize(12).FontWeight(600)
						ui.Grid(c).Columns(3).Gap(6).Children(func() {
							for _, spelling := range layout.ZeroInitials {
								ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
									ui.Text(c, spelling.Pinyin).FontSize(12).Width(28)
									keycap(c, strings.Join(spelling.Codes, " / "))
								})
							}
						})
					})
					ui.Text(c, T("s.g.chart.note")).FontSize(11.5).TextColor(p.muted).LineHeight(1.3)
				})
			})
		})
	})
}
