package main

// This pilot measures existing retrieval and ranking only. It does not expand
// the recovery index with gold spellings or change production abbreviation rules.
import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"shixin/internal/ai"
	"shixin/internal/llama"
	"shixin/internal/models"
)

type abbrExample struct {
	example
	Syllables []string `json:"syllables"`
	Form      string   `json:"form"`
	Typed     string   `json:"typed_form"`
	Parts     []string `json:"typed_syllables"`
	At        int      `json:"edit_byte_offset"`
	From      string   `json:"edit_from"`
	To        string   `json:"edit_to"`
}

type abbrForm struct {
	name  string
	parts []string
}

func abbreviationForms(syllables []string) []abbrForm {
	forms := []abbrForm{}
	seen := map[string]bool{}
	for _, name := range []string{"full", "initials", "digraph_initials", "mixed_even", "mixed_odd"} {
		parts := make([]string, len(syllables))
		for i, s := range syllables {
			parts[i] = s
			if name == "initials" || name == "digraph_initials" || name == "mixed_even" && i%2 == 0 || name == "mixed_odd" && i%2 == 1 {
				parts[i] = s[:1]
				if name == "digraph_initials" && len(s) >= 2 && (strings.HasPrefix(s, "zh") || strings.HasPrefix(s, "ch") || strings.HasPrefix(s, "sh")) {
					parts[i] = s[:2]
				}
			}
		}
		code := strings.Join(parts, "")
		if !seen[code] {
			forms = append(forms, abbrForm{name, parts})
			seen[code] = true
		}
	}
	return forms
}
func abbrNeighbors(c byte) string {
	rows := []string{"qwertyuiop", "asdfghjkl", "zxcvbnm"}
	offsets := []float64{0, .25, .75}
	type point struct{ x, y float64 }
	pts := map[byte]point{}
	for y, row := range rows {
		for x := range row {
			pts[row[x]] = point{float64(x) + offsets[y], float64(y)}
		}
	}
	p, ok := pts[c]
	if !ok {
		return ""
	}
	var out []byte
	for k, q := range pts {
		if k != c && math.Hypot(p.x-q.x, p.y-q.y) <= 1.3 {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return string(out)
}
func abbrPrepare(root, source, corpus string, count int) error {
	if count < 1 {
		return fmt.Errorf("families must be positive")
	}
	parents, e := lines[example](filepath.Join(source, "selected.jsonl"))
	if e != nil {
		return e
	}
	eligible := map[string]map[string]bool{"dev": {}, "test": {}}
	for _, r := range parents {
		eligible[r.Split][r.Family] = true
	}
	allowed := map[string]bool{}
	for _, split := range []string{"dev", "test"} {
		fs := keys(eligible[split])
		sort.Slice(fs, func(i, j int) bool { return hash("abbr-v1/"+fs[i]) < hash("abbr-v1/"+fs[j]) })
		for i, f := range fs {
			if i < count {
				allowed[f] = true
			}
		}
	}
	var rows []abbrExample
	hashes := map[string]string{}
	for _, split := range []string{"dev", "test"} {
		path := filepath.Join(corpus, split+".jsonl")
		input, e := lines[struct {
			example
			Syllables []string `json:"syllables"`
		}](path)
		if e != nil {
			return e
		}
		hashes[split], e = fileHash(path)
		if e != nil {
			return e
		}
		for _, r := range input {
			if r.Edit.Kind != "clean" || !allowed[r.Family] || len(r.Syllables) < 2 {
				continue
			}
			for _, form := range abbreviationForms(r.Syllables) {
				typed := strings.Join(form.parts, "")
				base := abbrExample{example: r.example, Syllables: r.Syllables, Form: form.name, Typed: typed, Parts: form.parts, At: -1}
				base.Raw = typed
				base.Edit.Kind = "clean"
				base.ID = hash("abbr-v1/" + r.ID + "/" + form.name + "/clean")
				rows = append(rows, base)
				// Choose one deterministic operation of each type, without consulting menus.
				for _, kind := range []string{"swap", "neighbor"} {
					var choices []abbrExample
					for at := range typed {
						changes := []string{}
						if kind == "swap" && at+1 < len(typed) && typed[at] != typed[at+1] {
							changes = append(changes, string([]byte{typed[at+1], typed[at]}))
						}
						if kind == "neighbor" {
							for _, c := range abbrNeighbors(typed[at]) {
								changes = append(changes, string(c))
							}
						}
						for _, to := range changes {
							n := 1
							if kind == "swap" {
								n = 2
							}
							v := base
							v.At = at
							v.From = typed[at : at+n]
							v.To = to
							v.Raw = typed[:at] + to + typed[at+n:]
							v.Edit.Kind = kind
							v.ID = hash("abbr-v1/" + r.ID + "/" + form.name + "/" + kind + fmt.Sprint(at) + to)
							choices = append(choices, v)
						}
					}
					sort.Slice(choices, func(i, j int) bool { return choices[i].ID < choices[j].ID })
					if len(choices) > 0 {
						rows = append(rows, choices[0])
					}
				}
			}
		}
	}
	// Shared raw codes can have multiple valid labels. Keep same-split ambiguity,
	// exclude cross-split raw codes from both sets; never redistribute families.
	owners := map[string]string{}
	conflict := map[string]bool{}
	for _, r := range rows {
		if prev, ok := owners[r.Raw]; ok && prev != r.Split {
			conflict[r.Raw] = true
		}
		owners[r.Raw] = r.Split
	}
	kept := []abbrExample{}
	codes := map[string]bool{}
	counts := map[string]int{}
	removed := 0
	for _, r := range rows {
		if conflict[r.Raw] {
			removed++
			continue
		}
		kept = append(kept, r)
		codes[r.Raw] = true
		counts[r.Split+"/"+r.Form+"/"+r.Edit.Kind]++
	}
	if e = os.Mkdir(root, 0755); e != nil {
		return e
	}
	if e = writeLines(filepath.Join(root, "selected.jsonl"), kept); e != nil {
		return e
	}
	var queries []codeRow
	for _, code := range keys(codes) {
		queries = append(queries, codeRow{code})
	}
	if e = writeLines(filepath.Join(root, "capture-codes.jsonl"), queries); e != nil {
		return e
	}
	for _, name := range []string{"profile.id", "contexts.jsonl"} {
		b, e := os.ReadFile(filepath.Join(source, name))
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(root, name), b, 0644); e != nil {
			return e
		}
		hashes[name] = hash(string(b))
	}
	hashes["parent_selected"], e = fileHash(filepath.Join(source, "selected.jsonl"))
	if e != nil {
		return e
	}
	fmt.Printf("Prepared %d rows, %d unique codes; excluded %d cross-split rows\n", len(kept), len(queries), removed)
	return writeJSON(filepath.Join(root, "preparation.json"), map[string]any{"version": "abbr-v1", "families_requested_per_split": count, "rows": len(kept), "codes": len(queries), "counts": counts, "excluded_cross_split_rows": removed, "source_hashes": hashes, "selection": "smallest SHA256(abbr-v1/family) within previous pilot dev/test; no context or score selection", "forms": "full, first-letter initials, zh/ch/sh digraph initials, two alternating full/initial masks; duplicate forms removed within reading", "mutations": "one minimum-hash adjacent swap and QWERTY neighbor per form; operations on typed form, not canonical full spelling", "scope": "existing native retrieval + rerank; no new correction candidates; no training"})
}

type abbrResult struct {
	ID         string `json:"id"`
	Split      string `json:"split"`
	Form       string `json:"form"`
	Kind       string `json:"kind"`
	Condition  string `json:"condition"`
	Raw        string `json:"raw"`
	Target     string `json:"target"`
	Baseline   int    `json:"baseline_rank"`
	AI         int    `json:"ai_rank"`
	Pool       int    `json:"pool_size"`
	ScoreNS    int64  `json:"score_ns"`
	PrefillNS  int64  `json:"prefill_ns"`
	WindowAI   int    `json:"window_ai_rank"`
	WindowSize int    `json:"window_size"`
	WindowNS   int64  `json:"window_score_ns"`
}
type abbrMetric struct {
	N            int        `json:"n"`
	Base         rankCounts `json:"baseline"`
	AI           rankCounts `json:"ai"`
	Recall50     int        `json:"recall50"`
	Lost5        int        `json:"lost5"`
	Gained5      int        `json:"gained5"`
	Late         int        `json:"score_over_16ms"`
	WindowAI     rankCounts `json:"window_ai"`
	WindowRecall int        `json:"window_recall"`
	WindowLost5  int        `json:"window_lost5"`
}

// Mirrors only the raw-code cap and full-span contiguous-prefix rule, not the
// native type/weight/segment ABI. A partial first candidate is left unchanged.
func abbreviationWindow(raw string, cs []candidate) int {
	limit := 50
	if strings.ContainsAny(raw, "aeiouv") {
		limit = 20
	}
	n := 0
	for n < len(cs) && n < limit && cs[n].Full {
		n++
	}
	return n
}

func abbrEvaluate(root, data, libDir, modelPath string) error {
	rows, e := lines[abbrExample](filepath.Join(root, "selected.jsonl"))
	if e != nil {
		return e
	}
	captures, e := lines[menu](filepath.Join(root, "menus.jsonl"))
	if e != nil {
		return e
	}
	pb, e := os.ReadFile(filepath.Join(root, "profile.id"))
	if e != nil {
		return e
	}
	menus := map[string]menu{}
	for _, m := range captures {
		if !m.Complete || m.Profile != strings.TrimSpace(string(pb)) || len(m.Candidates) > 50 {
			return fmt.Errorf("invalid menu %s", m.Code)
		}
		if _, ok := menus[m.Code]; ok {
			return fmt.Errorf("duplicate menu")
		}
		menus[m.Code] = m
	}
	for _, r := range rows {
		if _, ok := menus[r.Raw]; !ok {
			return fmt.Errorf("missing menu %s", r.Raw)
		}
	}
	ctxs, e := lines[publicContext](filepath.Join(root, "contexts.jsonl"))
	if e != nil {
		return e
	}
	contexts := map[string]string{}
	for _, c := range ctxs {
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
	var results []abbrResult
	metrics := map[string]*abbrMetric{}
	times := map[string][]int64{}
	unknown, total := 0, 0
	revision := uint32(10)
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
			cs := menus[r.Raw].Candidates
			for _, c := range cs {
				if c.Full {
					total++
					if _, ok := priors[c.Text]; !ok {
						unknown++
					}
				}
			}
			revision++
			start := time.Now()
			if e = scorer.Prefill(0, revision, []byte(context)); e != nil {
				return e
			}
			prefill := time.Since(start).Nanoseconds()
			ranked, ns, e := scoreMenu(scorer, cs, priors, nil, false)
			if e != nil {
				return e
			}
			b, a := rank(cs, r.Target), rank(ranked, r.Target)
			window := abbreviationWindow(r.Raw, cs)
			w, wns, err := scoreMenu(scorer, cs[:window], priors, nil, false)
			if err != nil {
				return err
			}
			wr := rank(append(w, cs[window:]...), r.Target)
			results = append(results, abbrResult{r.ID, r.Split, r.Form, r.Edit.Kind, condition, r.Raw, r.Target, b, a, len(cs), ns, prefill, wr, window, wns})
			key := condition + "/" + r.Split + "/" + r.Form + "/" + r.Edit.Kind
			m := metrics[key]
			if m == nil {
				m = &abbrMetric{}
				metrics[key] = m
			}
			m.N++
			inc(b, &m.Base.Top1, &m.Base.Top5, &m.Base.Top20)
			inc(a, &m.AI.Top1, &m.AI.Top5, &m.AI.Top20)
			inc(wr, &m.WindowAI.Top1, &m.WindowAI.Top5, &m.WindowAI.Top20)
			if b > 0 && b <= window {
				m.WindowRecall++
			}
			if in5(b) && !in5(wr) {
				m.WindowLost5++
			}
			if b > 0 {
				m.Recall50++
			}
			if in5(b) && !in5(a) {
				m.Lost5++
			}
			if !in5(b) && in5(a) {
				m.Gained5++
			}
			if ns > 16000000 {
				m.Late++
			}
			times[condition+"/score"] = append(times[condition+"/score"], ns)
			times[condition+"/window_score"] = append(times[condition+"/window_score"], wns)
			times[condition+"/prefill"] = append(times[condition+"/prefill"], prefill)
		}
		fmt.Printf("Completed %s: %d rows\n", condition, len(results))
	}
	if e = writeLines(filepath.Join(out, "rows.jsonl"), results); e != nil {
		return e
	}
	timing := map[string]map[string]int64{}
	for k, v := range times {
		timing[k] = percentiles(v)
	}
	if e = writeJSON(filepath.Join(out, "summary.json"), map[string]any{"metrics": metrics, "timing_ns": timing}); e != nil {
		return e
	}
	hashes := map[string]string{"model": modelHash}
	for _, name := range []string{"selected.jsonl", "menus.jsonl", "contexts.jsonl", "preparation.json"} {
		hashes[name], e = fileHash(filepath.Join(root, name))
		if e != nil {
			return e
		}
	}
	return writeJSON(filepath.Join(out, "manifest.json"), map[string]any{"rows": len(results), "hashes": hashes, "unknown_prior_occurrences": unknown, "movable_candidate_occurrences": total, "pool": "diagnostic AI: up to 50 for every form, partial entries frozen; window AI: contiguous full-span prefix, cap 50 without a/e/i/o/u/v else 20, tail unchanged; neither reproduces native type/weight ABI", "priors": "same approximate source log1p frequencies as prior experiment, not native weights", "limitations": "existing retrieval only; no typo expansion, no error penalty, no user learning; no live deadline; source contexts reused by target; subset selected independently of context"})
}
