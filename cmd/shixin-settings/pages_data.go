package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shixin/internal/settings"
)

// ---------------------------------------------------------------- 词库

func (a *app) dictionary(c *ui.Context) {
	s := &a.s
	p := colors(c)
	t := c.Theme()
	code := strings.ToLower(strings.TrimSpace(a.newCode))
	ph := settings.Phrase{Code: code, Text: strings.TrimSpace(a.newText)}
	invalid := ""
	if a.newCode != "" || a.newText != "" {
		if err := ph.Validate(); err != nil {
			switch {
			case ph.Code == "":
				invalid = T("s.d.need_code")
			case ph.Text == "":
				invalid = T("s.d.need_text")
			default:
				invalid = T("s.d.bad_code")
			}
		}
	}
	add := func() {
		if invalid != "" || ph.Code == "" || ph.Text == "" {
			return
		}
		if !slices.Contains(s.Phrases, ph) {
			s.Phrases = append([]settings.Phrase{ph}, s.Phrases...)
		}
		a.newCode, a.newText = "", ""
	}

	group(c, T("s.d.phrases"), T("s.d.phrases.note"), func() {
		block(c, func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c.Key("code"), &a.newCode).Placeholder(T("s.d.code")).Width(130).Font("monospace")
				if in.Submitted() {
					add()
				}
				if ui.TextInput(c.Key("text"), &a.newText).Placeholder(T("s.d.text")).Grow(1).Submitted() {
					add()
				}
				ui.PrimaryButton(c, T("s.d.add")).Disabled(invalid != "" || ph.Code == "" || ph.Text == "").OnClick(add)
			})
			if invalid != "" {
				ui.Text(c, invalid).FontSize(11.5).TextColor(t.Danger)
			}
		})
		if len(s.Phrases) > 8 {
			block(c, func() {
				ui.SearchField(c, &a.phraseQuery).Placeholder(T("s.d.search"))
			})
		}
		q := strings.ToLower(strings.TrimSpace(a.phraseQuery))
		shown := 0
		for i := 0; i < len(s.Phrases); i++ {
			ph := s.Phrases[i]
			if q != "" && !strings.Contains(ph.Code, q) && !strings.Contains(strings.ToLower(ph.Text), q) {
				continue
			}
			shown++
			ui.Row(c.Key(ph)).Padding(8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, ph.Code).FontSize(12).Font("monospace").Padding(2, 8).Radius(5).Background(p.faint).NoWrap()
				ui.Text(c, ph.Text).FontSize(13).Grow(1).SingleLine().Ellipsis("…")
				iconButton(c, iconTrash, T("s.d.delete")).OnClick(func() {
					s.Phrases = slices.DeleteFunc(s.Phrases, func(x settings.Phrase) bool { return x == ph })
				})
			})
		}
		if shown == 0 {
			block(c, func() {
				msg := T("s.d.empty")
				if q != "" {
					msg = T("s.d.no_match")
				}
				ui.Text(c, msg).FontSize(12.5).TextColor(p.muted).TextAlign(ui.Center).FillWidth().Padding(10, 0)
			})
		}
	})

	group(c, T("s.d.user"), T("s.d.user.note"), func() {
		row(c, T("s.k.forget"), T("s.d.forget.desc"), func() {
			keycap(c, keyLabel(s.Hotkeys["forget"]))
			ui.Button(c, T("s.d.shortcuts")).OnClick(func() { a.page = "shortcuts" })
		})
		row(c, T("s.d.folder"), T("s.d.folder.desc"), func() {
			ui.Button(c, T("s.open")).OnClick(func() { go mygo.Shell.OpenPath(rimeDir()) })
		})
	})
}

// ---------------------------------------------------------------- 应用

// loadApps lists the installed apps, off the main thread.
func (a *app) loadApps() {
	apps, err := mygo.Shell.InstalledApps()
	if err != nil {
		return
	}
	update(a, func() { a.installed = apps })
}

func (a *app) installedApp(id string) (mygo.InstalledApp, bool) {
	for _, x := range a.installed {
		if x.ID == id {
			return x, true
		}
	}
	return mygo.InstalledApp{}, false
}

// appIcon shows an app's icon, loading it in the background the first time
// it is shown. Main thread.
func (a *app) appIcon(c *ui.Context, id string, size float32) {
	if bm := a.icons[id]; bm != nil {
		ui.Image(c, bm).Size(size, size)
		return
	}
	ui.Box(c).Size(size, size).Radius(size*0.22).Background(colors(c).faint).Border(1, colors(c).border)
	x, ok := a.installedApp(id)
	if !ok || x.Path == "" || a.loading[id] {
		return
	}
	a.loading[id] = true
	go func() {
		png, err := mygo.Shell.FileIcon(x.Path, 64)
		if err != nil {
			return
		}
		if bm, err := ui.DecodeBitmap(png); err == nil {
			update(a, func() { a.icons[id] = bm })
		}
	}()
}

func (a *app) addRule(x mygo.InstalledApp, mode string) {
	if slices.ContainsFunc(a.s.Apps, func(r settings.AppRule) bool { return r.ID == x.ID }) {
		return
	}
	a.s.Apps = append(a.s.Apps, settings.AppRule{ID: x.ID, Name: x.Name, Mode: mode})
}

func (a *app) apps(c *ui.Context) {
	s := &a.s
	group(c, "", T("s.p.remember.note"), func() {
		switchRow(c, T("s.p.remember"), T("s.p.remember.desc"), &s.RememberAppMode)
	})

	group(c, T("s.p.rules"), T("s.p.rules.note"), func() {
		for i := range s.Apps {
			r := &s.Apps[i]
			id := r.ID
			a.appRow(c, id, r.Name, func() {
				segmented(c.Key("mode"), &r.Mode, []string{settings.ModeChinese, settings.ModeEnglish}, T("s.mode.chinese"), T("s.mode.english")).Label(T("s.p.mode"))
				iconButton(c, iconTrash, T("s.p.remove")).OnClick(func() {
					s.Apps = slices.DeleteFunc(s.Apps, func(x settings.AppRule) bool { return x.ID == id })
				})
			})
		}
		block(c, func() {
			ui.Row(c).Gap(8).Children(func() {
				addButton(c).OnClick(func() { a.picking, a.pickSpace, a.appQuery = true, false, "" })
				var missing []mygo.InstalledApp
				for _, id := range settings.DevApps {
					if x, ok := a.installedApp(id); ok && s.AppMode(id) == "" {
						missing = append(missing, x)
					}
				}
				if len(missing) > 0 {
					ui.Button(c, "").Children(func() {
						ui.Icon(c, iconSparkles).Size(13, 13)
						ui.Text(c, fmt.Sprintf(T("s.p.add_dev"), len(missing))).SingleLine()
					}).OnClick(func() {
						for _, x := range missing {
							a.addRule(x, settings.ModeEnglish)
						}
					})
				}
			})
		})
	})

	note := T("s.p.nospace.note")
	if !s.AutoSpace {
		note = T("s.p.nospace.off")
	}
	group(c, T("s.p.nospace"), note, func() {
		for _, id := range s.NoAutoSpace {
			name := id
			x, ok := a.installedApp(id)
			if ok {
				name = x.Name
			} else if slices.Contains(settings.DevApps, id) {
				continue // a default for when it's installed
			}
			a.appRow(c, id, name, func() {
				iconButton(c, iconTrash, T("s.p.remove")).OnClick(func() {
					s.NoAutoSpace = slices.DeleteFunc(s.NoAutoSpace, func(x string) bool { return x == id })
				})
			})
		}
		block(c, func() {
			ui.Row(c).Children(func() {
				addButton(c).OnClick(func() { a.picking, a.pickSpace, a.appQuery = true, true, "" })
			})
		})
	})
	a.appPicker(c)
}

// appRow is an app's row on the 应用 page: icon, name and id, then controls.
func (a *app) appRow(c *ui.Context, id, name string, controls func()) {
	p := colors(c)
	ui.Row(c.Key(id)).Padding(9, 14).Gap(12).AlignItems(ui.Center).Children(func() {
		a.appIcon(c, id, 28)
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			if name == "" {
				name = id
			}
			ui.Text(c, name).FontSize(13).SingleLine().Ellipsis("…")
			ui.Text(c, id).FontSize(11).TextColor(p.muted).SingleLine().Ellipsis("…")
		})
		controls()
	})
}

func addButton(c *ui.Context) ui.Element {
	return ui.Button(c, "").Children(func() {
		ui.Icon(c, iconPlus).Size(13, 13)
		ui.Text(c, T("s.p.add")).SingleLine()
	})
}

// appPicker is the dialog listing installed apps to add a rule for.
func (a *app) appPicker(c *ui.Context) {
	p := colors(c)
	t := c.Theme()
	ui.Modal(c, &a.picking, func() {
		ui.Column(c).Width(440).Gap(10).Children(func() {
			ui.Text(c, T("s.p.pick")).FontSize(15).FontWeight(650)
			ui.SearchField(c, &a.appQuery).Placeholder(T("s.p.search")).AutoFocus()
			q := strings.ToLower(strings.TrimSpace(a.appQuery))
			var list []mygo.InstalledApp
			for _, x := range a.installed {
				if a.pickSpace && slices.Contains(a.s.NoAutoSpace, x.ID) || !a.pickSpace && a.s.AppMode(x.ID) != "" {
					continue
				}
				if q == "" || strings.Contains(strings.ToLower(x.Name), q) || strings.Contains(strings.ToLower(x.ID), q) {
					list = append(list, x)
				}
			}
			ui.List(c, nil, len(list), func(i int) {
				x := list[i]
				r := ui.ButtonBase(c.Key(x.ID)).Padding(6, 8).Radius(7).Label(x.Name)
				if r.Hovered() {
					r.Background(t.Accent.Alpha(0.12))
				}
				r.Children(func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						a.appIcon(c, x.ID, 24)
						ui.Text(c, x.Name).FontSize(13).Grow(1).SingleLine().Ellipsis("…")
						ui.Text(c, x.ID).FontSize(11).TextColor(p.muted).SingleLine().MaxWidth(180).Ellipsis("…")
					})
				})
				if r.Clicked() {
					if a.pickSpace {
						a.s.NoAutoSpace = append(a.s.NoAutoSpace, x.ID)
					} else {
						a.addRule(x, settings.ModeEnglish)
					}
					a.picking = false
				}
			}).Height(320).Children(func() {
				if len(list) == 0 {
					ui.Text(c, T("s.p.none_found")).FontSize(12.5).TextColor(p.muted).Padding(16)
				}
			})
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, T("s.cancel")).Clicked() {
					a.picking = false
				}
			})
		})
	})
}
