package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"shixin/internal/ai"
	"shixin/internal/llama"
	"shixin/internal/models"
	"shixin/internal/rerank"
)

type rerankRow struct {
	ID           string   `json:"id"`
	Split        string   `json:"split"`
	Kind         string   `json:"kind"`
	Condition    string   `json:"condition"`
	Context      string   `json:"context"`
	Raw          string   `json:"raw"`
	Target       string   `json:"target"`
	Baseline     int      `json:"baseline_rank"`
	Merged       int      `json:"merged_rank"`
	BaselineAI   int      `json:"baseline_ai_rank"`
	MergedAI     int      `json:"merged_ai_rank"`
	PenaltyAI    int      `json:"penalty_ai_rank"`
	BaseFirst    string   `json:"baseline_first"`
	AIFirst      string   `json:"baseline_ai_first"`
	MergedFirst  string   `json:"merged_ai_first"`
	PenaltyFirst string   `json:"penalty_ai_first"`
	MergedPage   []string `json:"merged_ai_page"`
	PenaltyPage  []string `json:"penalty_ai_page"`
	PrefillNS    int64    `json:"prefill_ns"`
	BaseNS       int64    `json:"baseline_score_ns"`
	MergedNS     int64    `json:"merged_score_ns"`
	PenaltyNS    int64    `json:"penalty_score_ns"`
}

func dictionaryPriors(data string) (map[string]float64, error) {
	priors := map[string]float64{}
	for _, name := range []string{"8105", "base", "ext", "others", "tencent"} {
		f, e := os.Open(filepath.Join(data, "cn_dicts", name+".dict.yaml"))
		if e != nil {
			return nil, e
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 4096), 1<<20)
		body := false
		for sc.Scan() {
			s := strings.TrimSpace(sc.Text())
			if s == "..." {
				body = true
				continue
			}
			if !body || s == "" || strings.HasPrefix(s, "#") {
				continue
			}
			parts := strings.Split(s, "\t")
			at := 2
			if name == "tencent" {
				at = 1
			}
			if len(parts) <= at {
				continue
			}
			weight, e := strconv.ParseFloat(parts[at], 64)
			if e != nil || weight < 0 {
				continue
			}
			if _, ok := priors[parts[0]]; !ok {
				priors[parts[0]] = math.Log1p(weight)
			}
		}
		e = sc.Err()
		f.Close()
		if e != nil {
			return nil, e
		}
	}
	return priors, nil
}
func correctionCosts(raw menu, hits []hit, menus map[string]menu) map[string]float64 {
	costs := map[string]float64{}
	for _, c := range raw.Candidates {
		if c.Full {
			costs[c.Text] = 0
		}
	}
	for _, h := range hits {
		for _, c := range menus[h.Code].Candidates {
			if !c.Full {
				break
			}
			v := float64(h.Cost) / 10
			if old, ok := costs[c.Text]; !ok || v < old {
				costs[c.Text] = v
			}
		}
	}
	return costs
}
func scoreMenu(s *ai.Scorer, cs []candidate, priors, costs map[string]float64, penalty bool) ([]candidate, int64, error) {
	p := ai.Strengths[rerank.StrengthStandard]
	input := make([]ai.Candidate, len(cs))
	for i, c := range cs {
		w := priors[c.Text]
		if penalty {
			w -= 2 * costs[c.Text] / p.Beta
		}
		flags := uint8(0)
		if !c.Full {
			flags = rerank.EntryFixed
		}
		input[i] = ai.Candidate{Text: []byte(c.Text), Weight: float32(w), Flags: flags}
	}
	var perm [rerank.MaxOptions]uint8
	start := time.Now()
	order, e := s.Score(0, input, p, perm[:])
	elapsed := time.Since(start).Nanoseconds()
	if e != nil {
		return nil, 0, e
	}
	out := make([]candidate, len(order))
	for i, k := range order {
		out[i] = cs[k]
	}
	return out, elapsed, nil
}
func texts(cs []candidate) []string {
	if len(cs) > 5 {
		cs = cs[:5]
	}
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Text)
	}
	return out
}
func rerankExperiment(root, data, libDir, modelPath string) error {
	outDir := filepath.Join(root, "rerank-v1")
	if e := os.Mkdir(outDir, 0755); e != nil {
		return e
	}
	modelSHA, e := fileHash(modelPath)
	if e != nil {
		return e
	}
	if modelSHA != models.ERNIE.SHA256 {
		return fmt.Errorf("model hash mismatch: %s", modelSHA)
	}
	rows, e := lines[example](filepath.Join(root, "selected.jsonl"))
	if e != nil {
		return e
	}
	searches, e := lines[searchResult](filepath.Join(root, "search.jsonl"))
	if e != nil {
		return e
	}
	captures, e := lines[menu](filepath.Join(root, "menus.jsonl"))
	if e != nil {
		return e
	}
	ctxRows, e := lines[publicContext](filepath.Join(root, "contexts.jsonl"))
	if e != nil {
		return e
	}
	pb, e := os.ReadFile(filepath.Join(root, "profile.id"))
	if e != nil {
		return e
	}
	profile := strings.TrimSpace(string(pb))
	menus := map[string]menu{}
	for _, m := range captures {
		if !m.Complete || m.Profile != profile {
			return fmt.Errorf("invalid capture %s", m.Code)
		}
		menus[m.Code] = m
	}
	sr := map[string]searchResult{}
	for _, q := range searches {
		sr[q.Raw] = q
	}
	contexts := map[string]string{}
	for _, c := range ctxRows {
		contexts[c.Target] = c.Context
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
	s, e := ai.New(lib, modelPath)
	if e != nil {
		return e
	}
	defer s.Close()
	// Warm the shipped scorer with a public, non-evaluation menu.
	for i := 0; i < 5; i++ {
		if e = s.Prefill(0, uint32(i+1), []byte("我们今天一起去")); e != nil {
			return e
		}
		if _, _, e = scoreMenu(s, []candidate{{Text: "吃饭", Full: true}, {Text: "学习", Full: true}}, priors, nil, false); e != nil {
			return e
		}
	}
	var results []rerankRow
	revision := uint32(10)
	unknown, total := 0, 0
	conditions := []string{"empty", "source_context", "stale_context"}
	for _, condition := range conditions {
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
			baseline, ok := menus[r.Raw]
			if !ok {
				return fmt.Errorf("missing baseline %s", r.Raw)
			}
			q, ok := sr[r.Raw]
			if !ok {
				return fmt.Errorf("missing search %s", r.Raw)
			}
			aug, e := merge(baseline, q.Hits, menus)
			if e != nil {
				return e
			}
			costs := correctionCosts(baseline, q.Hits, menus)
			for _, c := range aug {
				if c.Full {
					total++
					if _, ok := priors[c.Text]; !ok {
						unknown++
					}
				}
			}
			revision++
			start := time.Now()
			if e = s.Prefill(0, revision, []byte(context)); e != nil {
				return e
			}
			prefill := time.Since(start).Nanoseconds()
			b, bns, e := scoreMenu(s, baseline.Candidates, priors, costs, false)
			if e != nil {
				return e
			}
			a, ans, e := scoreMenu(s, aug, priors, costs, false)
			if e != nil {
				return e
			}
			p, pns, e := scoreMenu(s, aug, priors, costs, true)
			if e != nil {
				return e
			}
			results = append(results, rerankRow{r.ID, r.Split, r.Edit.Kind, condition, context, r.Raw, r.Target, rank(baseline.Candidates, r.Target), rank(aug, r.Target), rank(b, r.Target), rank(a, r.Target), rank(p, r.Target), top(baseline.Candidates), top(b), top(a), top(p), texts(a), texts(p), prefill, bns, ans, pns})
		}
		fmt.Printf("Completed %s; %d cumulative rows\n", condition, len(results))
	}
	if e = writeLines(filepath.Join(outDir, "rows.jsonl"), results); e != nil {
		return e
	}
	// User-facing example fixtures, selected before scoring; diagnostic only.
	fixtures := []struct{ target, context string }{{"讨人", "这个孩子很"}, {"讨人", "他总爱挑别人的毛病，实在太"}, {"桃仁", "医生在处方中写下了中药"}}
	var fixtureResults []map[string]any
	for _, f := range fixtures {
		baseline := menus["taoren"]
		q := sr["taoren"]
		aug, e := merge(baseline, q.Hits, menus)
		if e != nil {
			return e
		}
		revision++
		if e = s.Prefill(0, revision, []byte(f.context)); e != nil {
			return e
		}
		costs := correctionCosts(baseline, q.Hits, menus)
		a, _, e := scoreMenu(s, aug, priors, costs, false)
		if e != nil {
			return e
		}
		p, _, e := scoreMenu(s, aug, priors, costs, true)
		if e != nil {
			return e
		}
		fixtureResults = append(fixtureResults, map[string]any{"raw": "taoren", "target": f.target, "context": f.context, "origin": "hand_authored_diagnostic_not_benchmark", "baseline_rank": rank(baseline.Candidates, f.target), "merged_rank": rank(aug, f.target), "ai_rank": rank(a, f.target), "penalty_ai_rank": rank(p, f.target), "ai_page": texts(a), "penalty_ai_page": texts(p)})
	}
	if e = writeJSON(filepath.Join(outDir, "fixtures.json"), fixtureResults); e != nil {
		return e
	}
	hashes := map[string]string{"model": modelSHA}
	for _, name := range []string{"selected.jsonl", "search.jsonl", "menus.jsonl", "contexts.jsonl"} {
		h, e := fileHash(filepath.Join(root, name))
		if e != nil {
			return e
		}
		hashes[name] = h
	}
	return writeJSON(filepath.Join(outDir, "manifest.json"), map[string]any{"model": "ERNIE-4.5-0.3B-Base q8_0 (pinned shipped model)", "hashes": hashes, "policy": ai.Strengths[rerank.StrengthStandard], "penalty": "fixed 2*edit_cost/10 log-score units; 1.6 for swap, 2.0 for substitution, applied once through Weight/Beta; not tuned", "priors": "log(1+source frequency), first rime-ice source wins; missing text prior=0; NOT native Phrase::weight", "unknown_prior_occurrences": unknown, "movable_candidate_occurrences": total, "partial_candidates": "frozen using EntryFixed; no shorter-prefix scoring", "timing": "offline scorer calls and prefill measured separately; no IPC/16ms hold enforced; no app QoS priority adjustment", "rows": len(results), "limitations": "Same captured bounded menus; not the exact native export ABI/pipeline. Source-derived contexts selected without model feedback; handmade fixtures excluded from aggregate metrics."})
}
