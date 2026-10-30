package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/ipc"
	"shixin/internal/models"
	"shixin/internal/rerank"
	"shixin/internal/settings"
)

// modelState is the 智能排序 model's download, main thread only.
type modelState struct {
	dir         string // models.Dir, "" if unknown
	present     bool
	active      bool // downloading
	done, total int64
	err         string
	cancel      context.CancelFunc
	aiState     string // rerank.ReadState: shixin-ai's process state
}

// refreshModel rechecks the model file and shixin-ai's state. Cheap: two
// small file reads.
func (a *app) refreshModel() {
	m := &a.model
	if m.dir == "" {
		m.dir, _ = models.Dir()
	}
	m.present = m.dir != "" && models.ERNIE.Present(m.dir)
	if dir, err := ipc.RuntimeDir(); err == nil {
		m.aiState = rerank.ReadState(dir)
	}
}

// download fetches the model in the background, reporting progress about
// ten times a second.
func (a *app) download() {
	m := &a.model
	if m.active || m.dir == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.active, m.done, m.total, m.err, m.cancel = true, 0, models.ERNIE.Size, "", cancel
	dir := m.dir
	go func() {
		var last time.Time
		err := models.ERNIE.Download(ctx, dir, func(done, total int64) {
			if now := time.Now(); now.Sub(last) > 100*time.Millisecond {
				last = now
				update(a, func() { a.model.done = done })
			}
		})
		update(a, func() {
			m.active, m.cancel = false, nil
			switch {
			case ctx.Err() != nil:
			case err != nil:
				m.err = err.Error()
			}
			a.refreshModel()
		})
	}()
}

// rerankGroup is 智能排序 on the 输入 page (DESIGN §7.2).
func (a *app) rerankGroup(c *ui.Context) {
	s := &a.s
	m := &a.model
	p := colors(c)
	t := c.Theme()
	group(c, T("s.i.rerank"), T("s.i.rerank.note"), func() {
		row(c, T("s.i.rerank"), T("s.i.rerank.desc"), func() {
			segmented(c, &s.Rerank, []string{settings.RerankOff, settings.RerankCautious, settings.RerankStandard}, T("s.off"), T("s.i.rerank.cautious"), T("s.i.rerank.standard")).Label(T("s.i.rerank"))
		})
		size := fmt.Sprintf("%s · %d MB", models.ERNIE.Name, models.ERNIE.Size/1e6)
		row(c, T("s.i.rerank.model"), size, func() {
			switch {
			case m.active:
				pct := 0
				if m.total > 0 {
					pct = int(m.done * 100 / m.total)
				}
				ui.Text(c, fmt.Sprintf(T("s.i.rerank.downloading"), pct)).FontSize(12.5).TextColor(p.muted).FontFeatures("tnum")
				ui.Button(c, T("s.cancel")).OnClick(func() { m.cancel() })
			case m.present:
				pill(c, T("s.i.rerank.downloaded"), t.Success, iconCheck)
				ui.Button(c, T("s.i.rerank.remove")).OnClick(func() {
					models.ERNIE.Remove(m.dir)
					a.refreshModel()
				})
			default:
				if m.err != "" {
					pill(c, T("s.i.rerank.failed"), t.Danger, iconAlert).Tooltip(m.err)
				}
				ui.Button(c, T("s.i.rerank.download")).OnClick(a.download)
			}
		})
		if s.Rerank != settings.RerankOff {
			row(c, T("s.i.rerank.status"), "", func() { a.rerankStatus(c) })
		}
	})
}

// rerankStatus shows whether 智能排序 is working, from shixin-ai's state
// and the engine's stats.
func (a *app) rerankStatus(c *ui.Context) {
	t := c.Theme()
	p := colors(c)
	ready := a.stats != nil && a.stats.Rerank != nil && a.stats.Rerank.Ready
	slow := ready && a.stats.Rerank.Slow
	switch {
	case a.model.aiState == rerank.StateNoAccelerator:
		pill(c, T("s.i.rerank.no_gpu"), t.Danger, iconAlert)
	case !a.model.present || a.model.aiState == rerank.StateNoModel:
		pill(c, T("s.i.rerank.need_model"), t.Warning, iconAlert)
	case slow:
		pill(c, T("s.i.rerank.slow"), t.Warning, iconAlert).Tooltip(T("s.i.rerank.slow.desc"))
	case ready:
		pill(c, T("s.i.rerank.ready"), t.Success, iconCheck)
	case a.model.aiState == rerank.StateRunning:
		pill(c, T("s.i.rerank.starting"), t.Warning, nil)
	default:
		pill(c, T("s.i.rerank.stopped"), p.muted, nil)
	}
}

// rerankPerf is 智能排序 on the 性能 page.
func (a *app) rerankPerf(c *ui.Context, r *ipc.RerankStats) {
	t := c.Theme()
	pct := func(n uint64) string {
		if r.Requests == 0 {
			return "–"
		}
		return fmt.Sprintf("%.1f", float64(n)*100/float64(r.Requests))
	}
	ui.Grid(c).Columns(4).Gap(10).Children(func() {
		a.metric(c, T("s.f.rerank.applied"), pct(r.Applied), "%", t.Text, T("s.f.rerank.applied.hint"))
		a.metric(c, T("s.f.rerank.timeouts"), pct(r.Timeouts), "%", t.Text, T("s.f.rerank.timeouts.hint"))
		for _, q := range []struct {
			label string
			q     float64
		}{{T("s.f.rerank.wait") + " p50", 0.5}, {T("s.f.rerank.wait") + " p99", 0.99}} {
			v, unit := fmtDur(r.Wait.Quantile(q.q))
			a.metric(c, q.label, v, unit, t.Text, T("s.f.rerank.wait.hint"))
		}
	})
}
