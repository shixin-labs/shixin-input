package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"shixin/internal/ipc"
)

func TestRerankSelectionMetrics(t *testing.T) {
	for _, counts := range []*ipc.RerankSelections{nil, {}, {Total: 500, Top1: 320, Top5: 460}} {
		a, _, _ := testApp(t)
		a.page, a.stats = "perf", sampleStats()
		a.stats.Rerank.Selections = counts
		view := ui.NewTester(a.view, 880, 900)
		view.Frame()
		for _, label := range []string{T("s.f.rerank.top1"), T("s.f.rerank.top5")} {
			if !view.HasText(label) {
				t.Fatalf("missing %q", label)
			}
		}
		if counts != nil && counts.Total > 0 && (!view.HasText("64.0") || !view.HasText("92.0")) {
			t.Fatal("selection percentages do not use the selection denominator")
		}
		if (counts == nil || counts.Total == 0) && !view.HasText("–") {
			t.Fatal("unavailable selection rates must show a dash")
		}
	}
}
