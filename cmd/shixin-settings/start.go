package main

import (
	"os"
	"os/exec"
	"path/filepath"
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

// inputSources opens System Settings' input source editor, with + one
// click away.
const inputSources = "x-apple.systempreferences:com.apple.Keyboard-Settings.extension?InputSources"

// frontExe is Shixin.app's front, seen from the settings app in
// Contents/Helpers; "" outside the bundle.
func frontExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	p := filepath.Join(filepath.Dir(exe), "..", "..", "..", "..", "MacOS", "Shixin")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return filepath.Clean(p)
}

// pollAdded asks the front whether 拾新 is in the user's input sources,
// but only while the guide shows: nothing else displays the answer.
func (a *app) pollAdded() {
	front := frontExe()
	if front == "" {
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
	front := frontExe()
	ui.Row(c).Gap(16).AlignItems(ui.Center).Padding(8, 4).Children(func() {
		ui.Image(c, appIcon).Size(56, 56).Shrink(0)
		ui.Column(c).Gap(4).Grow(1).Children(func() {
			ui.Text(c, T("s.start.title")).FontSize(18).FontWeight(700)
			ui.Text(c, T("s.start.subtitle")).FontSize(12.5).TextColor(p.muted)
		})
		if a.added {
			quiet(c, T("s.start.added"), iconCheck)
		} else {
			pill(c, T("s.start.not_added"), t.Warning, iconAlert)
		}
	})
	// macOS asks once: a later request shows the dialog again, but Allow
	// then adds nothing, so there's no "ask again" (DESIGN §5.5).
	group(c, T("s.start.step1"), "", func() {
		row(c, T("s.start.allow"), T("s.start.allow.desc"), nil)
	})
	group(c, T("s.start.step2"), "", func() {
		row(c, T("s.start.add"), T("s.start.add.desc"), func() {
			ui.Button(c, T("s.start.add.open")).OnClick(func() {
				go func() {
					// System Settings may list input sources from a stale
					// cache that lacks 拾新: refresh it first.
					if front != "" {
						exec.Command(front, "-refresh").Run()
					}
					mygo.Shell.OpenExternal(inputSources)
				}()
			})
		})
	})
	group(c, T("s.start.step3"), "", func() {
		row(c, T("s.start.type"), T("s.start.type.desc"), nil)
		row(c, T("s.i.rerank"), T("s.start.rerank.desc"), func() {
			ui.Button(c, T("s.start.rerank.open")).OnClick(func() { a.page = "input" })
		})
	})
}
