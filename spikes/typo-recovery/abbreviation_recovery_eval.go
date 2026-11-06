package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shixin/internal/ai"
	"shixin/internal/llama"
	"shixin/internal/models"
)

type recoveryRow struct {
	ID           string `json:"id"`
	Split        string `json:"split"`
	Form         string `json:"form"`
	Kind         string `json:"kind"`
	Condition    string `json:"condition"`
	Raw          string `json:"raw"`
	Target       string `json:"target"`
	Exhausted    bool   `json:"exhausted"`
	SearchHit    bool   `json:"search_hit"`
	VerifiedHit  bool   `json:"verified_hit"`
	Rejected     int    `json:"rejected_proposals"`
	Baseline     int    `json:"baseline_rank"`
	Merged       int    `json:"merged_rank"`
	BaseAI       int    `json:"baseline_ai_rank"`
	AugAI        int    `json:"augmented_ai_rank"`
	PenaltyAI    int    `json:"penalty_ai_rank"`
	GuardAI      int    `json:"guarded_ai_rank"`
	BaseNS       int64  `json:"baseline_ns"`
	AugNS        int64  `json:"augmented_ns"`
	PenaltyNS    int64  `json:"penalty_ns"`
	PrefillNS    int64  `json:"prefill_ns"`
	SinglePassAI int    `json:"single_pass_ai_rank"`
}
type recoveryMetric struct {
	N               int        `json:"n"`
	Exhausted       int        `json:"exhausted"`
	SearchHits      int        `json:"search_hits"`
	VerifiedHits    int        `json:"verified_hits"`
	Rejected        int        `json:"rejected_proposals"`
	UnionRecall     int        `json:"union_recall"`
	PoolRecall      int        `json:"pool_recall"`
	Base            rankCounts `json:"baseline"`
	Merged          rankCounts `json:"merged"`
	BaseAI          rankCounts `json:"baseline_ai"`
	AugAI           rankCounts `json:"augmented_ai"`
	PenaltyAI       rankCounts `json:"penalty_ai"`
	GuardAI         rankCounts `json:"guarded_ai"`
	LostAI5         int        `json:"lost_ai5_vs_baseline_ai"`
	LostPenalty5    int        `json:"lost_penalty5_vs_baseline_ai"`
	LostGuard5      int        `json:"lost_guard5_vs_baseline_ai"`
	SinglePassAI    rankCounts `json:"single_pass_ai"`
	LostSinglePass5 int        `json:"lost_single_pass5_vs_baseline_ai"`
}

func preservePage(baseline, aug []candidate) []candidate {
	key := func(c candidate) string { return fmt.Sprintf("%t/%s", c.Full, c.Text) }
	out := append([]candidate{}, baseline[:min(5, len(baseline))]...)
	seen := map[string]bool{}
	for _, c := range out {
		seen[key(c)] = true
	}
	for _, c := range aug {
		if !seen[key(c)] {
			out = append(out, c)
			seen[key(c)] = true
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}
func verifiedProposals(hits []lexicalHit, menus map[string]menu) ([]lexicalHit, int, error) {
	var out []lexicalHit
	rejected := 0
	for _, h := range hits {
		m, ok := menus[h.Code]
		if !ok || !m.Complete {
			return nil, 0, fmt.Errorf("missing verification %s", h.Code)
		}
		if rank(m.Candidates, h.Text) > 0 {
			out = append(out, h)
		} else {
			rejected++
		}
	}
	return out, rejected, nil
}
func abbrRecoveryEvaluate(root, data, libDir, modelPath string) error {
	rows, e := lines[abbrExample](filepath.Join(root, "selected.jsonl"))
	if e != nil {
		return e
	}
	searches, e := lines[lexicalSearch](filepath.Join(root, "search.jsonl"))
	if e != nil {
		return e
	}
	sr := map[string]lexicalSearch{}
	for _, s := range searches {
		sr[s.Raw] = s
	}
	profile, e := os.ReadFile(filepath.Join(root, "profile.id"))
	if e != nil {
		return e
	}
	readMenus := func(path string) (map[string]menu, error) {
		ms, e := lines[menu](path)
		if e != nil {
			return nil, e
		}
		m := map[string]menu{}
		for _, v := range ms {
			if !v.Complete || v.Profile != strings.TrimSpace(string(profile)) {
				return nil, fmt.Errorf("invalid capture %s", v.Code)
			}
			m[v.Code] = v
		}
		return m, nil
	}
	base, e := readMenus(filepath.Join(root, "menus.jsonl"))
	if e != nil {
		return e
	}
	verified, e := readMenus(filepath.Join(root, "verification", "menus.jsonl"))
	if e != nil {
		return e
	}
	ctxRows, e := lines[publicContext](filepath.Join(root, "contexts.jsonl"))
	if e != nil {
		return e
	}
	contexts := map[string]string{}
	for _, c := range ctxRows {
		contexts[c.Target] = c.Context
	}
	modelHash, e := fileHash(modelPath)
	if e != nil {
		return e
	}
	if modelHash != models.ERNIE.SHA256 {
		return fmt.Errorf("model hash mismatch")
	}
	out := filepath.Join(root, "evaluation")
	if e = os.Mkdir(out, 0755); e != nil {
		return e
	}
	priors, e := dictionaryPriors(data)
	if e != nil {
		return e
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lib, e := llama.Load(libDir)
	if e != nil {
		return e
	}
	scorer, e := ai.New(lib, modelPath)
	if e != nil {
		return e
	}
	defer scorer.Close()
	for i := 0; i < 5; i++ {
		if e = scorer.Prefill(0, uint32(i+1), []byte("我们今天一起去")); e != nil {
			return e
		}
		if _, _, e = scoreMenu(scorer, []candidate{{Text: "吃饭", Full: true}, {Text: "学习", Full: true}}, priors, nil, false); e != nil {
			return e
		}
	}
	var results []recoveryRow
	metrics := map[string]*recoveryMetric{}
	revision := uint32(10)
	timing := map[string][]int64{}
	for _, condition := range []string{"empty", "source_context", "stale_context"} {
		for _, r := range rows {
			context := ""
			if condition != "empty" {
				var ok bool
				context, ok = contexts[r.Target]
				if !ok {
					continue
				}
				if condition == "stale_context" {
					context = "系统设置已更新，请重新启动应用。"
				}
			}
			q, ok := sr[r.Raw]
			if !ok {
				return fmt.Errorf("missing search")
			}
			b, ok := base[r.Raw]
			if !ok {
				return fmt.Errorf("missing baseline")
			}
			hits, rejected, e := verifiedProposals(q.Hits, verified)
			if e != nil {
				return e
			}
			aug, costs := lexicalMerge(b.Candidates, hits)
			revision++
			start := time.Now()
			if e = scorer.Prefill(0, revision, []byte(context)); e != nil {
				return e
			}
			prefill := time.Since(start).Nanoseconds()
			ba, bns, e := scoreMenu(scorer, b.Candidates, priors, nil, false)
			if e != nil {
				return e
			}
			// This arm uses only the ordinary scorer call; recovered candidates
			// remain in channel order after the ordinary first page.
			single, _ := lexicalMerge(ba, hits)
			aa, ans, e := scoreMenu(scorer, aug, priors, costs, false)
			if e != nil {
				return e
			}
			pa, pns, e := scoreMenu(scorer, aug, priors, costs, true)
			if e != nil {
				return e
			}
			guard := preservePage(ba, pa)
			searchHit, verifiedHit := false, false
			for _, h := range q.Hits {
				if h.Text == r.Target {
					searchHit = true
				}
			}
			for _, h := range hits {
				if h.Text == r.Target {
					verifiedHit = true
				}
			}
			result := recoveryRow{r.ID, r.Split, r.Form, r.Edit.Kind, condition, r.Raw, r.Target, q.Exhausted, searchHit, verifiedHit, rejected, rank(b.Candidates, r.Target), rank(aug, r.Target), rank(ba, r.Target), rank(aa, r.Target), rank(pa, r.Target), rank(guard, r.Target), bns, ans, pns, prefill, rank(single, r.Target)}
			results = append(results, result)
			key := condition + "/" + r.Split + "/" + r.Form + "/" + r.Edit.Kind
			m := metrics[key]
			if m == nil {
				m = &recoveryMetric{}
				metrics[key] = m
			}
			m.N++
			if q.Exhausted {
				m.Exhausted++
			}
			if searchHit {
				m.SearchHits++
			}
			if verifiedHit {
				m.VerifiedHits++
			}
			m.Rejected += rejected
			if result.Baseline > 0 || verifiedHit {
				m.UnionRecall++
			}
			if result.Merged > 0 {
				m.PoolRecall++
			}
			for _, v := range []struct {
				r int
				c *rankCounts
			}{{result.Baseline, &m.Base}, {result.Merged, &m.Merged}, {result.BaseAI, &m.BaseAI}, {result.AugAI, &m.AugAI}, {result.PenaltyAI, &m.PenaltyAI}, {result.GuardAI, &m.GuardAI}, {result.SinglePassAI, &m.SinglePassAI}} {
				inc(v.r, &v.c.Top1, &v.c.Top5, &v.c.Top20)
			}
			if in5(result.BaseAI) && !in5(result.AugAI) {
				m.LostAI5++
			}
			if in5(result.BaseAI) && !in5(result.PenaltyAI) {
				m.LostPenalty5++
			}
			if in5(result.BaseAI) && !in5(result.GuardAI) {
				m.LostGuard5++
			}
			if in5(result.BaseAI) && !in5(result.SinglePassAI) {
				m.LostSinglePass5++
			}
			timing[condition+"/baseline"] = append(timing[condition+"/baseline"], bns)
			timing[condition+"/augmented"] = append(timing[condition+"/augmented"], ans)
			timing[condition+"/penalty"] = append(timing[condition+"/penalty"], pns)
			timing[condition+"/prefill"] = append(timing[condition+"/prefill"], prefill)
		}
		fmt.Printf("Completed %s: %d rows\n", condition, len(results))
	}
	if e = writeLines(filepath.Join(out, "rows.jsonl"), results); e != nil {
		return e
	}
	times := map[string]map[string]int64{}
	for k, v := range timing {
		times[k] = percentiles(v)
	}
	lookup := []int64{}
	states := []int64{}
	edgeChecks := []int64{}
	allocs, exhausted := 0, 0
	for _, s := range searches {
		lookup = append(lookup, s.NS)
		states = append(states, int64(s.States))
		edgeChecks = append(edgeChecks, int64(s.Edges))
		allocs += s.Allocs
		if s.Exhausted {
			exhausted++
		}
	}
	if e = writeJSON(filepath.Join(out, "summary.json"), map[string]any{"metrics": metrics, "scoring_ns": times, "search_queries_including_prefixes": len(searches), "search_exhausted": exhausted, "search_allocations": allocs, "lookup_ns": percentiles(lookup), "states": percentiles(states), "edge_checks": percentiles(edgeChecks)}); e != nil {
		return e
	}
	hashes := map[string]string{"model": modelHash}
	for _, p := range []string{"selected.jsonl", "menus.jsonl", "contexts.jsonl", "preparation.json", "search.jsonl", "verification/menus.jsonl"} {
		hashes[p], e = fileHash(filepath.Join(root, p))
		if e != nil {
			return e
		}
	}
	return writeJSON(filepath.Join(out, "manifest.json"), map[string]any{"rows": len(results), "hashes": hashes, "pool": "both control and augmented arms use up to 50, with partials frozen; not native 20/mixed export", "merge": "ordinary first five, up to eight unique verified hypotheses, remaining ordinary candidates to cap50", "penalty": "same fixed 2*cost/10 as earlier; zero for baseline and zero-edit abbreviation paths", "guard": "post-scoring diagnostic preserves original-AI first five; no second inference hold proposed", "verification": "proposal text must appear as verified whole-input candidate in canonical-code scratch capture; raw-code graph alignment and learning remain unimplemented", "limitations": "explicit lexicon only; fixed four zero/four typo quotas; approximate prior; synthetic reused test families; no integrated latency, deployment or model training"})
}
