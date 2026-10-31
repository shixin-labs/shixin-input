package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shixin/internal/logx"
	"shixin/internal/perf"
)

// ---------------------------------------------------------------- 性能

// fmtDur shows a latency in µs below a millisecond, else in ms.
func fmtDur(d time.Duration) (string, string) {
	if d < time.Millisecond {
		return fmt.Sprint(d.Microseconds()), "µs"
	}
	return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond)), "ms"
}

// fmtUptime shows a duration in days, hours and minutes.
func fmtUptime(d time.Duration) string {
	days, h, m := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0:
		return fmt.Sprintf(T("s.f.dhm"), days, h, m)
	case h > 0:
		return fmt.Sprintf(T("s.f.hm"), h, m)
	}
	return fmt.Sprintf(T("s.f.m"), m)
}

// tildePath abbreviates the home directory as ~.
func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// budgetColor colors a latency against its budget (DESIGN §3.7).
func budgetColor(c *ui.Context, d, budget time.Duration) ui.Color {
	t := c.Theme()
	switch {
	case d <= budget/2:
		return t.Success
	case d <= budget:
		return t.Warning
	}
	return t.Danger
}

func (a *app) metric(c *ui.Context, label, value, unit string, color ui.Color, hint string) {
	p := colors(c)
	ui.Column(c).Background(p.card).Radius(10).Border(1, p.border).Padding(12, 14).Gap(4).Tooltip(hint).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(p.muted).SingleLine()
		ui.Row(c).AlignItems(ui.Baseline).Gap(3).Children(func() {
			ui.Text(c, value).FontSize(22).FontWeight(650).TextColor(color).FontFeatures("tnum")
			ui.Text(c, unit).FontSize(12).TextColor(p.muted)
		})
	})
}

func (a *app) perf(c *ui.Context) {
	p := colors(c)
	t := c.Theme()
	st := a.stats
	if st == nil {
		group(c, "", "", func() {
			ui.Column(c).Padding(36, 20).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconPerf).Size(28, 28).TextColor(p.muted)
				ui.Text(c, T("s.f.offline")).FontSize(14).FontWeight(600)
				ui.Text(c, T("s.f.offline.desc")).FontSize(12).TextColor(p.muted).TextAlign(ui.Center)
			})
		})
		return
	}
	h := &st.KeyTime
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(t.Success)
		ui.Text(c, T("s.f.live")).FontSize(12).TextColor(p.muted).Grow(1)
		ui.Button(c, T("s.f.reset")).OnClick(func() { a.resetHist.Store(true) })
	})
	ui.Grid(c).Columns(4).Gap(10).Children(func() {
		a.metric(c, T("s.f.keys"), fmt.Sprint(st.Keys), "", t.Text, "")
		for _, q := range []struct {
			label  string
			q      float64
			budget time.Duration
		}{{"p50", 0.5, 300 * time.Microsecond}, {"p99", 0.99, 2 * time.Millisecond}, {"p99.9", 0.999, 8 * time.Millisecond}} {
			v, unit := fmtDur(h.Quantile(q.q))
			color := t.Text
			if h.Count() > 0 {
				color = budgetColor(c, h.Quantile(q.q), q.budget)
			}
			b, bu := fmtDur(q.budget)
			a.metric(c, q.label, v, unit, color, fmt.Sprintf(T("s.f.budget"), b+" "+bu))
		}
	})
	group(c, T("s.f.histogram"), T("s.f.histogram.note"), func() {
		block(c, func() { histogram(c, h) })
	})
	if r := st.Rerank; r != nil && (r.On || r.Requests > 0) {
		ui.Text(c, T("s.i.rerank")).FontSize(12).FontWeight(600).TextColor(p.muted).Padding(0, 4)
		a.rerankPerf(c, r)
	}
	group(c, T("s.f.engine"), "", func() {
		row(c, "librime", "", func() { ui.Text(c, string(st.Librime)).FontSize(12.5).TextColor(p.muted) })
		row(c, T("s.f.native"), T("s.f.native.desc"), func() {
			if st.Native {
				pill(c, T("s.f.loaded"), t.Success, iconCheck)
			} else {
				pill(c, T("s.f.missing"), t.Warning, iconAlert)
			}
		})
		row(c, T("s.f.schemas"), "", func() {
			ui.Text(c, strings.ReplaceAll(string(st.Schemas), ",", ", ")).FontSize(12.5).TextColor(p.muted)
		})
		row(c, T("s.f.startup"), "", func() {
			ui.Text(c, (time.Duration(st.StartupMS) * time.Millisecond).String()).FontSize(12.5).TextColor(p.muted)
		})
		row(c, T("s.f.uptime"), "", func() {
			ui.Text(c, fmtUptime(time.Duration(st.UptimeS)*time.Second)).FontSize(12.5).TextColor(p.muted)
		})
		row(c, T("s.f.dropped"), T("s.f.dropped.desc"), func() {
			color := p.muted
			if st.Dropped > 0 {
				color = t.Danger
			}
			ui.Text(c, fmt.Sprint(st.Dropped)).FontSize(12.5).TextColor(color)
		})
	})
}

// histogram draws the per-key latency distribution as bars on a log
// scale, one per histogram bucket from the fastest to the slowest key.
func histogram(c *ui.Context, h *perf.Hist) {
	p := colors(c)
	t := c.Theme()
	bs := h.Buckets()
	if len(bs) == 0 {
		ui.Text(c, T("s.f.no_keys")).FontSize(12.5).TextColor(p.muted).Padding(20, 0).TextAlign(ui.Center).FillWidth()
		return
	}
	var peak uint32
	for _, b := range bs {
		peak = max(peak, b.Count)
	}
	const height = 110
	ui.Row(c).Height(height).Gap(2).AlignItems(ui.End).Children(func() {
		for _, b := range bs {
			frac := float32(math.Log1p(float64(b.Count)) / math.Log1p(float64(peak)))
			v, unit := fmtDur(b.Lo)
			ui.Box(c).Grow(1).Height(max(2, frac*height)).Radius(3, 3, 0, 0).
				Background(budgetColor(c, b.Lo, 2*time.Millisecond).Mix(t.Accent, 0.35)).
				Tooltip(fmt.Sprintf("≥ %s %s · %d", v, unit, b.Count))
		}
	})
	ui.Row(c).Children(func() {
		lo, lu := fmtDur(bs[0].Lo)
		hi, hu := fmtDur(h.Max())
		ui.Text(c, lo+" "+lu).FontSize(11).TextColor(p.muted).Grow(1)
		ui.Text(c, hi+" "+hu).FontSize(11).TextColor(p.muted)
	})
}

// ---------------------------------------------------------------- 关于

func (a *app) about(c *ui.Context) {
	p := colors(c)
	t := c.Theme()
	ui.Row(c).Gap(16).AlignItems(ui.Center).Padding(8, 4).Children(func() {
		ui.Image(c, appIcon).Size(72, 72).Shrink(0)
		ui.Column(c).Gap(3).Children(func() {
			ui.Text(c, T("s.b.name")).FontSize(20).FontWeight(700)
			ui.Text(c, fmt.Sprintf(T("menu.version"), version)).FontSize(12.5).TextColor(p.muted)
			ui.Text(c, T("s.b.tagline")).FontSize(12.5).TextColor(p.muted)
		})
	})
	a.updateGroup(c)
	group(c, T("s.b.data"), "", func() {
		row(c, T("s.d.folder"), tildePath(rimeDir()), func() {
			ui.Button(c, T("s.open")).OnClick(func() { go mygo.Shell.OpenPath(rimeDir()) })
		})
		row(c, T("s.b.logs"), tildePath(logx.Dir()), func() {
			ui.Button(c, T("s.open")).OnClick(func() { go mygo.Shell.OpenPath(logx.Dir()) })
		})
	})
	if dir := licensesDir(); dir != "" {
		group(c, T("s.b.licenses"), "", func() {
			row(c, T("s.b.licenses"), T("s.b.licenses.desc"), func() {
				ui.Button(c, T("s.open")).OnClick(func() { go mygo.Shell.OpenPath(dir) })
			})
		})
	}
	group(c, T("s.b.maintenance"), "", func() {
		row(c, T("s.b.reset"), T("s.b.reset.desc"), func() {
			ui.Button(c, T("s.b.reset.button")).TextColor(t.Danger).OnClick(func() { a.confirmReset = true })
		})
	})
	group(c, T("s.b.privacy"), "", func() {
		block(c, func() {
			ui.Text(c, T("s.b.privacy.text")).FontSize(12.5).LineHeight(1.45)
		})
	})
	if ui.AlertDialog(c, &a.confirmReset, T("s.b.reset.confirm"), T("s.b.reset.confirm.desc"), T("s.cancel"), T("s.b.reset.button")) == 1 {
		a.resetDefaults()
	}
}

// licensesDir is Shixin.app's Contents/Resources/licenses, seen from the
// settings app in Contents/Helpers; "" outside the bundle.
func licensesDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Join(filepath.Dir(exe), "..", "..", "..", "..", "Resources", "licenses")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return filepath.Clean(dir)
}
