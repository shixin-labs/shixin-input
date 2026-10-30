package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The look follows macOS System Settings: pages of grouped cards on a
// slightly tinted pane, each card a list of rows with the label on the left
// and the control on the right.

type palette struct {
	pane, card, border, muted, faint ui.Color
}

func colors(c *ui.Context) palette {
	t := c.Theme()
	if t.Dark {
		return palette{
			pane:   ui.Hex("#1e1f22"),
			card:   ui.Hex("#2a2b2f"),
			border: ui.RGBA(255, 255, 255, 0.07),
			muted:  t.TextMuted,
			faint:  ui.RGBA(255, 255, 255, 0.05),
		}
	}
	return palette{
		pane:   ui.Hex("#f4f5f7"),
		card:   ui.Hex("#ffffff"),
		border: ui.RGBA(0, 0, 0, 0.07),
		muted:  t.TextMuted,
		faint:  ui.RGBA(0, 0, 0, 0.035),
	}
}

// group is a card of rows under an optional title, with an optional note
// below it.
func group(c *ui.Context, title, note string, rows func()) {
	p := colors(c)
	ui.Column(c).Gap(6).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.muted).Padding(0, 4)
		}
		ui.Column(c).Background(p.card).Radius(10).Border(1, p.border).
			Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.03)).Gap(1).Dividers(1, p.border).Clip().Children(rows)
		if note != "" {
			ui.Text(c, note).FontSize(11.5).TextColor(p.muted).Padding(0, 4).LineHeight(1.35)
		}
	})
}

// row is a labeled setting with its control on the right.
func row(c *ui.Context, label, desc string, control func()) ui.Element {
	p := colors(c)
	r := ui.Row(c).Padding(11, 14).Gap(16).AlignItems(ui.Center).MinHeight(44)
	r.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13)
			if desc != "" {
				ui.Text(c, desc).FontSize(11.5).TextColor(p.muted).LineHeight(1.3)
			}
		})
		if control != nil {
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(control)
		}
	})
	return r
}

// switchRow is a row whose switch a click anywhere on the row turns over.
func switchRow(c *ui.Context, label, desc string, on *bool) {
	r := ui.ButtonBase(c.Key(label)).Label(label).Role(ui.RoleSwitch).Checked(*on).Justify(ui.Start).AlignItems(ui.Stretch)
	if r.Clicked() {
		*on = !*on
	}
	r.Children(func() {
		row(c, label, desc, func() {
			sw := ui.SwitchBase(c, on).Width(38).Height(22).Radius(11).Padding(2).PassThrough()
			t := c.Theme()
			if *on {
				sw.Background(t.Accent)
			} else {
				sw.Background(t.Text.Alpha(0.14))
			}
			x := sw.Animate("knob", b2f(*on), 160*time.Millisecond)
			sw.Children(func() {
				ui.Box(c).Size(18, 18).Radius(9).Background(ui.RGB(255, 255, 255)).
					Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.25)).Margin(0, 0, 0, x*16)
			})
		}).Grow(1)
	})
}

// chip is a toggle drawn as a rounded pill, accent-filled while on.
func chip(c *ui.Context, key any, text string, on, disabled bool) ui.Element {
	t := c.Theme()
	p := colors(c)
	b := ui.ButtonBase(c.Key(key)).Padding(5, 12).Radius(999).Label(text).Checked(on).Disabled(disabled)
	switch {
	case on:
		b.Background(t.Accent).TextColor(t.AccentText)
	case disabled:
		b.Background(p.faint).TextColor(p.muted.Alpha(0.6))
	default:
		b.Background(p.faint).Border(1, p.border)
		if b.Hovered() {
			b.Background(p.faint.Mix(t.Accent, 0.08))
		}
	}
	b.Children(func() { ui.Text(c, text).FontSize(12.5).Font("monospace").NoWrap() })
	return b
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// block is a full-width card row for custom content.
func block(c *ui.Context, fn func()) ui.Element {
	return ui.Column(c).Padding(14).Gap(10).Children(fn)
}

// tile is a sidebar icon: a white glyph on a rounded colored square.
func tile(c *ui.Context, ic *ui.SVG, color ui.Color, size float32) {
	ui.Box(c).Size(size, size).Radius(size*0.26).Center().
		Gradient(color.Mix(ui.RGB(255, 255, 255), 0.18), color, 180).
		Children(func() {
			ui.Icon(c, ic).Size(size*0.62, size*0.62).TextColor(ui.RGB(255, 255, 255))
		})
}

// keycap shows a key or shortcut as a small raised cap.
func keycap(c *ui.Context, s string) {
	p := colors(c)
	ui.Text(c, s).FontSize(12).Font("monospace").Padding(2, 7).Radius(5).
		Background(p.faint).Border(1, p.border).NoWrap()
}

// pill is a small status label.
func pill(c *ui.Context, text string, fg ui.Color, ic *ui.SVG) ui.Element {
	return ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(3, 10).Radius(999).Background(fg.Alpha(0.12)).Children(func() {
		if ic != nil {
			ui.Icon(c, ic).Size(12, 12).TextColor(fg)
		}
		ui.Text(c, text).FontSize(11.5).FontWeight(500).TextColor(fg).NoWrap()
	})
}

// iconButton is a borderless button showing an icon, named by label.
func iconButton(c *ui.Context, ic *ui.SVG, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(28, 28).Radius(6).Center().TextColor(t.TextMuted).Label(label).Tooltip(label)
	if b.Hovered() {
		b.Background(colors(c).faint).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, ic).Size(15, 15) })
	return b
}

// segmented binds a segmented control to a string value among ids.
func segmented(c *ui.Context, value *string, ids []string, labels ...string) ui.Element {
	i := 0
	for j, id := range ids {
		if id == *value {
			i = j
		}
	}
	e := ui.Segmented(c, &i, labels...)
	*value = ids[i]
	return e
}

// selectID binds a drop-down to a string value among ids, showing names.
func selectID(c *ui.Context, value *string, ids, names []string) ui.Element {
	sel := names[0]
	for j, id := range ids {
		if id == *value {
			sel = names[j]
		}
	}
	e := ui.Select(c, &sel, names)
	for j, n := range names {
		if n == sel {
			*value = ids[j]
		}
	}
	return e
}
