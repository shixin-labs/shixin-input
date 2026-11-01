package main

import (
	"os/exec"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// ---------------------------------------------------------------- 开始使用

// The first-run guide (DESIGN §5.5, §6). Opening Shixin.app installs it
// and shows this page; macOS 26 doesn't let an app add its own input
// source, so the guide walks the user through System Settings and checks
// every two seconds whether 拾新 has been added.

var iconStart = icon(`<circle cx="12" cy="12" r="10"/><path d="m10 8 6 4-6 4z"/>`)

// pollAdded asks the front whether 拾新 is in the user's input sources,
// but only while the guide shows: nothing else displays the answer.
func (a *app) pollAdded() {
	front := plat.front()
	if front == "" || !plat.added {
		return
	}
	was := false
	for ; ; time.Sleep(2 * time.Second) {
		if !a.startShown.Load() {
			continue
		}
		added := exec.Command(front, "-added").Run() == nil
		if added && !was {
			// Just added: macOS's caches may still lack it (DESIGN §5.5).
			exec.Command(front, "-repair").Run()
		}
		was = added
		update(a, func() { a.added = added })
	}
}

func (a *app) start(c *ui.Context) {
	t := c.Theme()
	p := colors(c)
	front := plat.front()
	g := plat.guide
	ui.Row(c).Gap(16).AlignItems(ui.Center).Padding(8, 4).Children(func() {
		ui.Image(c, appIcon).Size(56, 56).Shrink(0)
		ui.Column(c).Gap(4).Grow(1).Children(func() {
			ui.Text(c, T("s.start.title")).FontSize(18).FontWeight(700)
			ui.Text(c, T(g.subtitle)).FontSize(12.5).TextColor(p.muted)
		})
		switch {
		case !plat.added:
		case a.added:
			quiet(c, T("s.start.added"), iconCheck)
		default:
			pill(c, T("s.start.not_added"), t.Warning, iconAlert)
		}
	})
	if plat.consent {
		// macOS asks once: a later request shows the dialog again, but
		// Allow then adds nothing, so there's no "ask again" (DESIGN §5.5).
		group(c, T("s.start.step1"), "", func() {
			row(c, T("s.start.allow"), T("s.start.allow.desc"), nil)
		})
	}
	group(c, T(g.addStep), "", func() {
		row(c, T(g.add), T(g.addDesc), func() {
			ui.Button(c, T(g.addOpen)).OnClick(func() {
				go func() {
					// System Settings may list input sources from a stale
					// cache that lacks 拾新: refresh it first.
					if front != "" && plat.refresh {
						exec.Command(front, "-refresh").Run()
					}
					mygo.Shell.OpenExternal(plat.inputSettings)
				}()
			})
		})
	})
	group(c, T(g.typeStep), "", func() {
		row(c, T("s.start.type"), T(g.typeDesc), nil)
		row(c, T("s.i.rerank"), T("s.start.rerank.desc"), func() {
			ui.Button(c, T("s.start.rerank.open")).OnClick(func() { a.page = "rerank" })
		})
	})
}
