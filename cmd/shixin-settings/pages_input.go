package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
	"shixin/internal/settings"
	"shixin/internal/ui/candview"
	"shixin/internal/ui/fonts"
	"shixin/internal/ui/skin"
)

var T = i18n.T

func choiceIDs(cs []settings.Choice) (ids, names []string) {
	for _, c := range cs {
		ids = append(ids, c.ID)
		if i18n.Current() == i18n.English {
			names = append(names, c.NameEn)
		} else {
			names = append(names, c.Name)
		}
	}
	return ids, names
}

// ---------------------------------------------------------------- 常用

func (a *app) general(c *ui.Context) {
	s := &a.s
	group(c, T("s.g.typing"), "", func() {
		row(c, T("settings.scheme"), T("s.g.scheme.desc"), func() {
			ids, names := choiceIDs(settings.Schemes)
			selectID(c, &s.Scheme, ids, names).Width(190)
		})
		if settings.Shuangpin(s.Scheme) != nil {
			row(c, T("s.g.chart"), T("s.g.chart.desc"), func() {
				ui.Button(c, T("s.g.chart.open")).OnClick(func() { a.chartShown = true })
			})
			switchRow(c, T("s.g.full_pinyin"), T("s.g.full_pinyin.desc"), &s.FullPinyinFallback)
		}
		row(c, T("settings.page_size"), "", func() {
			labels := make([]string, 0, 8)
			ids := make([]string, 0, 8)
			for n := 3; n <= 10; n++ {
				labels = append(labels, strconv.Itoa(n))
				ids = append(ids, strconv.Itoa(n))
			}
			v := strconv.Itoa(s.PageSize)
			segmented(c, &v, ids, labels...).Label(T("settings.page_size"))
			s.PageSize, _ = strconv.Atoi(v)
		})
		row(c, T("s.g.layout"), "", func() {
			segmented(c, &s.Layout, []string{"horizontal", "vertical"}, T("s.g.layout.horizontal"), T("s.g.layout.vertical")).Label(T("s.g.layout"))
		})
	})
	group(c, T("s.g.switching"), T("s.g.switching.note"), func() {
		row(c, T("settings.shift"), T("s.g.shift.desc"), func() {
			ids := []string{"commit_code", "commit_text", "clear", "off"}
			names := make([]string, len(ids))
			for i, id := range ids {
				names[i] = T("settings.shift." + id)
			}
			selectID(c, &s.Shift, ids, names).Width(250)
		})
		row(c, T("s.g.initial"), T("s.g.initial.desc"), func() {
			segmented(c, &s.InitialMode, []string{settings.ModeChinese, settings.ModeEnglish}, T("s.mode.chinese"), T("s.mode.english")).Label(T("s.g.initial"))
		})
		row(c, T("s.g.punctuation"), T("s.g.punctuation.desc"), func() {
			segmented(c, &s.Punctuation, []string{settings.ModeChinese, settings.ModeEnglish}, T("s.g.punctuation.chinese"), T("s.g.punctuation.english")).Label(T("s.g.punctuation"))
		})
	})
	a.shuangpinChart(c)
}

// ---------------------------------------------------------------- 外观

// previewState is the sample the appearance preview shows.
var previewState = candview.State{
	Preedit: "shi xin",
	Items: []candview.Item{
		{Label: "1", Text: "拾新"}, {Label: "2", Text: "实心"}, {Label: "3", Text: "是"},
		{Label: "4", Text: "时"}, {Label: "5", Text: "🆕", Comment: "new"},
	},
}

func (a *app) dark(c *ui.Context) bool {
	switch a.s.Theme {
	case "light":
		return false
	case "dark":
		return true
	}
	return c.Theme().Dark
}

func (a *app) appearance(c *ui.Context) {
	s := &a.s
	p := colors(c)
	dark := a.dark(c)
	// The live preview: the real candidate panel over a desktop-like
	// backdrop, below a line of text being typed.
	style := &candview.Style{Skin: skin.Get(s.Skin), Dark: dark, FontFamily: fonts.Resolve(s.FontFamily, a.fonts), FontSize: float32(s.FontSize), Vertical: s.Layout == "vertical"}
	n := min(s.PageSize, len(previewState.Items))
	st := previewState
	st.Items = st.Items[:n]
	backdrop := ui.Column(c).MinHeight(170).Radius(12).Border(1, p.border).Clip().Padding(22, 26).Gap(6)
	backdrop.Background(p.card)
	backdrop.Children(func() {
		text := ui.Hex("#1d2129")
		if dark {
			text = ui.Hex("#e8eaf0")
		}
		ui.Row(c).AlignItems(ui.Baseline).Gap(4).Children(func() {
			ui.Text(c, T("s.a.sample")).FontSize(15).TextColor(text)
			ui.Text(c, "shi xin").FontSize(15).TextColor(text).Underline()
		})
		candview.Draw(c, &st, style, nil)
	})

	group(c, T("s.a.display"), "", func() {
		row(c, T("s.a.font"), T("s.a.font.desc"), func() {
			selected := s.FontFamily
			if selected == "" {
				selected = T("s.a.font.system")
			}
			options := append([]string{T("s.a.font.system")}, a.fonts...)
			if s.FontFamily != "" && fonts.Resolve(s.FontFamily, a.fonts) == "" {
				options = append(options, s.FontFamily)
			}
			ui.Combobox(c, &selected, options).Label(T("s.a.font.choose")).Width(210)
			s.FontFamily = selected
			if selected == T("s.a.font.system") {
				s.FontFamily = ""
			}
		})
		if s.FontFamily != "" && fonts.Resolve(s.FontFamily, a.fonts) == "" {
			block(c, func() { ui.Text(c, T("s.a.font.missing")).FontSize(11.5).TextColor(p.muted) })
		}
		row(c, T("s.a.theme"), T("s.a.theme.desc"), func() {
			segmented(c, &s.Theme, []string{"system", "light", "dark"}, T("s.a.theme.system"), T("s.a.theme.light"), T("s.a.theme.dark")).Label(T("s.a.theme"))
		})
		row(c, T("s.a.font_size"), "", func() {
			v := float64(s.FontSize)
			ui.Slider(c, &v, 12, 28).Width(180).Label(T("s.a.font_size"))
			s.FontSize = int(v + 0.5)
			ui.Text(c, strconv.Itoa(s.FontSize)+" pt").FontSize(12).TextColor(p.muted).Width(40).TextAlign(ui.End)
		})
	})
	group(c, T("s.a.skin"), "", func() {
		block(c, func() {
			ui.Grid(c).Columns(3).Gap(12).Children(func() {
				for i := range skin.All {
					a.skinTile(c, &skin.All[i], dark)
				}
			})
		})
	})
}

func (a *app) skinTile(c *ui.Context, sk *skin.Skin, dark bool) {
	t := c.Theme()
	pal := sk.Palette(dark)
	selected := a.s.Skin == sk.ID
	_, names := choiceIDs(settings.Skins)
	name := names[slices.IndexFunc(settings.Skins, func(x settings.Choice) bool { return x.ID == sk.ID })]
	b := ui.ButtonBase(c.Key(sk.ID)).Padding(4).Radius(12).Label(name)
	if selected {
		b.Border(2, t.Accent)
	} else {
		b.Border(2, ui.Transparent)
	}
	b.Children(func() {
		ui.Column(c).Gap(7).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Height(64).Radius(9).Background(pal.Background).Border(1, pal.Border).Padding(10, 10).Gap(6).Children(func() {
				ui.Box(c).Width(34).Height(5).Radius(3).Background(pal.Muted.Alpha(0.5))
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "拾新").FontSize(12).Padding(2, 6).Radius(5).Background(pal.Highlight).TextColor(pal.OnHigh)
					ui.Text(c, "实心").FontSize(12).TextColor(pal.Text)
					ui.Text(c, "是").FontSize(12).TextColor(pal.Text)
				})
			})
			label := ui.Text(c, name).FontSize(12).AlignSelf(ui.Center)
			if selected {
				label.FontWeight(600).TextColor(t.Accent)
			}
		})
	})
	if b.Clicked() {
		a.s.Skin = sk.ID
	}
}

// ---------------------------------------------------------------- 输入

func (a *app) input(c *ui.Context) {
	s := &a.s
	group(c, T("settings.fuzzy"), T("s.i.fuzzy.note"), func() {
		block(c, func() {
			ui.Row(c).Wrap().Gap(8).Children(func() {
				for _, f := range settings.FuzzyPairs {
					on := slices.Contains(s.Fuzzy, f.ID)
					if chip(c, f.ID, strings.ReplaceAll(f.ID, "=", " ⇄ "), on, false).Clicked() {
						if on {
							s.Fuzzy = slices.DeleteFunc(s.Fuzzy, func(x string) bool { return x == f.ID })
						} else {
							s.Fuzzy = append(s.Fuzzy, f.ID)
						}
					}
				}
			})
		})
	})

	group(c, T("s.i.keys"), "", func() {
		row(c, T("s.i.select_char"), T("s.i.select_char.desc"), func() {
			ids := []string{"brackets", "minus_equal", "comma_period", "off"}
			names := []string{"[  ]", "-  =", ",  .", T("s.off")}
			selectID(c, &s.SelectChar, ids, names).Width(120)
		})
		row(c, T("s.i.page_keys"), pageKeysDesc(s), func() {
			for _, kp := range settings.KeyPairs {
				on := slices.Contains(s.PageKeys, kp.ID)
				taken := kp.ID == s.SelectChar
				name := kp.Name
				if kp.ID == "tab" {
					name = "⇥ Tab"
				}
				if taken {
					name += " · " + T("s.i.select_char")
				}
				ch := chip(c, "page-"+kp.ID, name, on, taken)
				if taken {
					ch.Tooltip(T("s.i.page_keys.taken"))
				}
				if ch.Clicked() && !taken {
					if on {
						s.PageKeys = slices.DeleteFunc(s.PageKeys, func(k string) bool { return k == kp.ID })
					} else {
						s.PageKeys = append(s.PageKeys, kp.ID)
					}
				}
			}
		})
	})

	group(c, T("s.i.candidates"), "", func() {
		switchRow(c, T("s.i.mixed"), T("s.i.mixed.desc"), &s.MixedEnglish)
		switchRow(c, T("s.i.emoji"), T("s.i.emoji.desc"), &s.Emoji)
		switchRow(c, T("s.i.traditional"), T("s.i.traditional.desc"), &s.Traditional)
	})

	a.rerankGroup(c)

	group(c, T("s.i.spacing"), T("s.i.auto_space.note"), func() {
		switchRow(c, T("s.i.auto_space"), T("s.i.auto_space.desc"), &s.AutoSpace)
	})

	// The codes librime-shixin's translators answer (DESIGN §3.8), so they
	// can be found; they aren't configurable.
	codes := dateCodes(s.Scheme)
	group(c, T("s.i.quick"), T("s.i.quick.note"), func() {
		for _, q := range []struct{ id, code, desc string }{
			{"date", codes["date"], fmt.Sprintf(T("s.i.quick.date.desc"), codes["datezh"], codes["dateen"])},
			{"time", codes["time"], fmt.Sprintf(T("s.i.quick.time.desc"), codes["datetime"], codes["timestamp"])},
			{"week", codes["week"], T("s.i.quick.week.desc")},
			{"number", "R123", T("s.i.quick.number.desc")},
			{"unicode", "U62fe", T("s.i.quick.unicode.desc")},
			{"calc", "cC1+2*3", T("s.i.quick.calc.desc")},
		} {
			row(c, T("s.i.quick."+q.id), q.desc, func() { keycap(c, q.code) })
		}
	})
}

// dateCodes returns rime-ice's date_translator codes for a scheme: its full
// pinyin uses initials (rq), its double pinyin schemas whole words (date),
// since two letters are a syllable there.
func dateCodes(scheme string) map[string]string {
	codes := map[string]string{}
	for _, k := range []string{"date", "time", "week", "datetime", "timestamp", "datezh", "dateen"} {
		codes[k] = k
	}
	if scheme == "rime_ice" {
		for k, v := range map[string]string{"date": "rq", "time": "sj", "week": "xq", "datetime": "dt", "timestamp": "ts", "datezh": "rqzh", "dateen": "rqen"} {
			codes[k] = v
		}
	}
	return codes
}

// ---------------------------------------------------------------- 快捷键

// keyLabel shows a Rime key name the way macOS menus do.
func keyLabel(k string) string {
	if k == "" {
		return T("s.none")
	}
	names := map[string]string{
		"period": ".", "comma": ",", "slash": "/", "grave": "`", "space": "Space",
		"minus": "-", "equal": "=", "bracketleft": "[", "bracketright": "]", "Tab": "⇥",
		"semicolon": ";", "apostrophe": "'",
		"BackSpace": "⌫", "Delete": "⌦",
	}
	parts := strings.Split(k, "+")
	for i, part := range parts {
		switch part {
		case "Control":
			parts[i] = "⌃"
		case "Shift":
			parts[i] = "⇧"
		case "Alt":
			parts[i] = "⌥"
		case "Super":
			parts[i] = "⌘"
		default:
			if n, ok := names[part]; ok {
				parts[i] = n
			}
		}
	}
	return strings.Join(parts, " ")
}

func (a *app) shortcuts(c *ui.Context) {
	s := &a.s
	group(c, T("s.k.toggles"), T("s.k.toggles.note"), func() {
		for _, act := range settings.HotkeyActions {
			if act.ID != "forget" {
				a.shortcut(c, act)
			}
		}
	})
	group(c, T("s.k.candidates"), T("s.k.candidates.note"), func() {
		for _, act := range settings.HotkeyActions {
			if act.ID == "forget" {
				a.shortcut(c, act)
			}
		}
		row(c, T("s.k.candidate_keys"), T("s.k.candidate_keys.desc"), func() {
			ids, names := []string{"off"}, []string{T("s.off")}
			for _, pair := range settings.CandidateKeyPairs {
				ids = append(ids, pair.ID)
				names = append(names, keyLabel(pair.Keys[0])+" / "+keyLabel(pair.Keys[1]))
			}
			selectID(c.Key("candidate-keys"), &s.CandidateKeys, ids, names).Width(180)
		})
	})
	group(c, T("s.k.fixed"), "", func() {
		row(c, T("s.k.shift"), T("settings.shift."+s.Shift), func() { keycap(c, "⇧ Shift") })
		row(c, T("s.k.select"), "", func() { keycap(c, "1 – 0"); keycap(c, "Space") })
		if keys, ok := settings.KeyPair(s.SelectChar); ok {
			row(c, T("s.i.select_char"), "", func() { keycap(c, keyLabel(keys[0])); keycap(c, keyLabel(keys[1])) })
		}
		row(c, T("s.i.page_keys"), "", func() {
			keycap(c, "PgUp")
			keycap(c, "PgDn")
			for _, id := range s.PageKeys {
				keys, _ := settings.KeyPair(id)
				keycap(c, keyLabel(keys[0])+" "+keyLabel(keys[1]))
			}
		})
	})
}

func (a *app) shortcut(c *ui.Context, act settings.HotkeyAction) {
	row(c, T("s.k."+act.ID), T("s.k."+act.ID+".desc"), func() {
		names := make([]string, len(act.Choices))
		for i, k := range act.Choices {
			names[i] = keyLabel(k)
		}
		v := a.s.Hotkeys[act.ID]
		selectID(c.Key(act.ID), &v, act.Choices, names).Width(130).Label(T("s.k."+act.ID) + " " + keyLabel(v))
		a.s.Hotkeys[act.ID] = v
	})
}

// pageKeysDesc says which keys turn pages, so an empty selection reads as
// off rather than as four unpicked chips.
func pageKeysDesc(s *settings.Settings) string {
	var next, prev []string
	for _, kp := range settings.KeyPairs {
		if !slices.Contains(s.PageKeys, kp.ID) || kp.ID == s.SelectChar {
			continue
		}
		p, n, _ := strings.Cut(kp.Name, " ")
		if kp.ID == "tab" {
			p, n = "Shift+Tab", "Tab"
		}
		prev, next = append(prev, strings.TrimSpace(p)), append(next, strings.TrimSpace(n))
	}
	if len(next) == 0 {
		return T("s.i.page_keys.none")
	}
	return fmt.Sprintf(T("s.i.page_keys.on"), strings.Join(next, " "), strings.Join(prev, " "))
}
