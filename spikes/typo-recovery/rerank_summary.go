package main

import (
	"fmt"
	"path/filepath"
)

type rankCounts struct {
	Top1  int `json:"top1"`
	Top5  int `json:"top5"`
	Top20 int `json:"top20"`
}
type rerankMetric struct {
	N                      int        `json:"n"`
	Baseline               rankCounts `json:"baseline"`
	Merged                 rankCounts `json:"merged"`
	BaselineAI             rankCounts `json:"baseline_ai"`
	MergedAI               rankCounts `json:"merged_ai"`
	PenaltyAI              rankCounts `json:"penalty_ai"`
	LostAI5vsBaseAI        int        `json:"merged_ai_lost5_vs_baseline_ai"`
	LostPenalty5vsBaseAI   int        `json:"penalty_ai_lost5_vs_baseline_ai"`
	LostAI5vsOriginal      int        `json:"merged_ai_lost5_vs_original"`
	LostPenalty5vsOriginal int        `json:"penalty_ai_lost5_vs_original"`
	BaseAITop1Changed      int        `json:"baseline_ai_top1_changed"`
	MergedAITop1Changed    int        `json:"merged_ai_top1_changed"`
	PenaltyTop1Changed     int        `json:"penalty_ai_top1_changed"`
}

func in5(rank int) bool { return rank > 0 && rank <= 5 }
func rerankSummary(root string) error {
	rows, e := lines[rerankRow](filepath.Join(root, "rerank-v1", "rows.jsonl"))
	if e != nil {
		return e
	}
	metrics := map[string]*rerankMetric{}
	timings := map[string][]int64{}
	late := map[string]int{}
	for _, r := range rows {
		key := r.Condition + "/" + r.Split + "/" + r.Kind
		m := metrics[key]
		if m == nil {
			m = &rerankMetric{}
			metrics[key] = m
		}
		m.N++
		for _, v := range []struct {
			rank int
			c    *rankCounts
		}{{r.Baseline, &m.Baseline}, {r.Merged, &m.Merged}, {r.BaselineAI, &m.BaselineAI}, {r.MergedAI, &m.MergedAI}, {r.PenaltyAI, &m.PenaltyAI}} {
			inc(v.rank, &v.c.Top1, &v.c.Top5, &v.c.Top20)
		}
		if in5(r.BaselineAI) && !in5(r.MergedAI) {
			m.LostAI5vsBaseAI++
		}
		if in5(r.BaselineAI) && !in5(r.PenaltyAI) {
			m.LostPenalty5vsBaseAI++
		}
		if in5(r.Baseline) && !in5(r.MergedAI) {
			m.LostAI5vsOriginal++
		}
		if in5(r.Baseline) && !in5(r.PenaltyAI) {
			m.LostPenalty5vsOriginal++
		}
		if r.BaseFirst != r.AIFirst {
			m.BaseAITop1Changed++
		}
		if r.BaseFirst != r.MergedFirst {
			m.MergedAITop1Changed++
		}
		if r.BaseFirst != r.PenaltyFirst {
			m.PenaltyTop1Changed++
		}
		for name, ns := range map[string]int64{"prefill": r.PrefillNS, "baseline_score": r.BaseNS, "merged_score": r.MergedNS, "penalty_score": r.PenaltyNS} {
			k := r.Condition + "/" + name
			timings[k] = append(timings[k], ns)
			if ns > 16000000 {
				late[k]++
			}
		}
	}
	timingSummary := map[string]any{}
	for name, t := range timings {
		timingSummary[name] = map[string]any{"n": len(t), "ns": percentiles(t), "over16ms": late[name]}
	}
	fmt.Printf("Summarized %d scored examples\n", len(rows))
	return writeJSON(filepath.Join(root, "rerank-v1", "summary.json"), map[string]any{"metrics": metrics, "timings": timingSummary, "rows": len(rows), "warning": "Scores are offline unconstrained by the 16ms hold. Latency excludes IPC/policy integration; actual late replies would fall back. A changed top1 is not necessarily incorrect with ambiguous homophones."})
}
