package main

import (
	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
)

const (
	titleBarH = 52
	sidebarW  = 216
)

// page is an entry of the sidebar.
type page struct {
	id    string
	icon  *ui.SVG
	color ui.Color
	build func(a *app, c *ui.Context)
}

var pages = []page{
	{"start", iconStart, ui.Hex("#34c759"), (*app).start},
	{"general", iconGeneral, ui.Hex("#8e8e93"), (*app).general},
	{"appearance", iconAppear, ui.Hex("#5e5ce6"), (*app).appearance},
	{"input", iconInput, ui.Hex("#0a84ff"), (*app).input},
	{"shortcuts", iconKeys, ui.Hex("#ff9f0a"), (*app).shortcuts},
	{"dictionary", iconDict, ui.Hex("#28b4c8"), (*app).dictionary},
	{"apps", iconApps, ui.Hex("#30c45a"), (*app).apps},
	{"perf", iconPerf, ui.Hex("#ff375f"), (*app).perf},
	{"about", iconAbout, ui.Hex("#636366"), (*app).about},
}

func (a *app) current() *page {
	for i := range pages {
		if pages[i].id == a.page {
			return &pages[i]
		}
	}
	return &pages[0]
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	p := colors(c)
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(sidebarW).Shrink(0)
		if !vibrant {
			side.Background(t.Background.Mix(t.Text, 0.04))
		}
		side.Children(func() { a.sidebar(c) })
		ui.Box(c).Width(1).Background(p.border)
		ui.Column(c).Grow(1).MinWidth(0).Background(p.pane).Children(func() { a.pane(c) })
	})
	a.statsShown.Store(a.page == "perf" || a.page == "input")
	a.sync(c)
}

func (a *app) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Box(c).Height(titleBarH).Shrink(0).DragWindow()
	ui.Column(c).Grow(1).Padding(0, 10).Gap(2).Children(func() {
		for i := range pages {
			pg := &pages[i]
			selected := pg.id == a.page
			item := ui.ButtonBase(c.Key(pg.id)).Padding(5, 8).Radius(7).Justify(ui.Start).Label(i18n.T("s.page." + pg.id))
			switch {
			case selected:
				item.Background(t.Accent.Alpha(0.16))
			case item.Hovered():
				item.Background(t.Text.Alpha(0.05))
			}
			item.Children(func() {
				ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
					tile(c, pg.icon, pg.color, 22)
					label := ui.Text(c, i18n.T("s.page."+pg.id)).FontSize(13).SingleLine()
					if selected {
						label.FontWeight(600)
					}
				})
			})
			if item.Clicked() {
				a.page = pg.id
			}
		}
	})
	ui.Row(c).Padding(12, 18).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, i18n.T("app.name")).FontSize(11.5).FontWeight(600).TextColor(t.TextMuted)
		ui.Text(c, version).FontSize(11).TextColor(t.TextMuted.Alpha(0.7)).SingleLine()
	})
}

func (a *app) pane(c *ui.Context) {
	bar := c.TitleBar()
	pg := a.current()
	a.startShown.Store(pg.id == "start")
	ui.Row(c).Height(titleBarH).Shrink(0).AlignItems(ui.Center).Padding(0, bar.Right+20, 0, 28).Gap(12).DragWindow().Children(func() {
		ui.Text(c, i18n.T("s.page."+pg.id)).FontSize(15).FontWeight(650).SingleLine().Grow(1)
		if pg.id != "start" { // it has no settings, and its own added/not added pill
			a.statusPill(c)
		}
	})
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(4, 28, 32, 28).Gap(22).MaxWidth(680).FillWidth().Margin(0, ui.Auto).Children(func() {
			pg.build(a, c)
		})
	})
}

func (a *app) statusPill(c *ui.Context) {
	t := c.Theme()
	switch a.status {
	case statusPending:
		pill(c, i18n.T("s.status.pending"), t.TextMuted, nil)
	case statusApplying:
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(12, 12)
			pill(c, i18n.T("s.status.applying"), t.Accent, nil)
		})
	case statusError:
		pill(c, i18n.T("s.status.error"), t.Danger, iconAlert).Tooltip(a.err)
	default:
		pill(c, i18n.T("s.status.saved"), t.Success, iconCheck)
	}
}
