package main

import (
	"fmt"
	"path/filepath"
)

type nativeCandidate struct {
	Text    string `json:"text"`
	Comment string `json:"comment"`
	Rank    int    `json:"rank"`
	Full    bool   `json:"full_span"`
	Start   int    `json:"span_start"`
	End     int    `json:"span_end"`
}

type nativeMenu struct {
	Code       string            `json:"code"`
	Complete   bool              `json:"complete"`
	Candidates []nativeCandidate `json:"candidates"`
}

func nativeTargetRank(m nativeMenu, target string) int {
	for _, c := range m.Candidates {
		if c.Text == target && c.Full {
			return c.Rank
		}
	}
	return 0
}

func nativePrefixRetained(before, after nativeMenu) bool {
	n := min(5, len(before.Candidates))
	if len(after.Candidates) < n {
		return false
	}
	for i := 0; i < n; i++ {
		if before.Candidates[i] != after.Candidates[i] {
			return false
		}
	}
	return true
}

type nativeMetric struct {
	Examples      int `json:"examples"`
	Ordinary5     int `json:"ordinary_top5"`
	Protected5    int `json:"protected_top5"`
	Ordinary13    int `json:"ordinary_top13"`
	Protected13   int `json:"protected_top13"`
	Gains         int `json:"gains_top13"`
	Losses        int `json:"losses_top13"`
	PrefixChanges int `json:"ordinary_prefix_changes"`
}

// Native captures verify actual whole-input commits in private warm sessions.
// No canonical-code replay or model score is substituted for native recall.
func nativeEvaluate(root, source string) error {
	rows, err := lines[abbrExample](filepath.Join(source, "selected.jsonl"))
	if err != nil {
		return err
	}
	load := func(name string) (map[string]nativeMenu, error) {
		menus, e := lines[nativeMenu](filepath.Join(root, name+".jsonl"))
		if e != nil {
			return nil, e
		}
		out := make(map[string]nativeMenu)
		for _, m := range menus {
			if _, duplicate := out[m.Code]; duplicate || !m.Complete {
				return nil, fmt.Errorf("%s: duplicate or incomplete capture %q", name, m.Code)
			}
			for i, c := range m.Candidates {
				if c.Rank != i+1 {
					return nil, fmt.Errorf("%s: invalid ranks for %q", name, m.Code)
				}
			}
			out[m.Code] = m
		}
		return out, nil
	}
	ordinary, err := load("ordinary")
	if err != nil {
		return err
	}
	protected, err := load("protected")
	if err != nil {
		return err
	}
	metrics := make(map[string]*nativeMetric)
	failures := []map[string]any{}
	for _, r := range rows {
		a, ok := ordinary[r.Raw]
		if !ok {
			return fmt.Errorf("missing ordinary code %q", r.Raw)
		}
		b, ok := protected[r.Raw]
		if !ok {
			return fmt.Errorf("missing protected code %q", r.Raw)
		}
		ar, br := nativeTargetRank(a, r.Target), nativeTargetRank(b, r.Target)
		before, after := ar > 0 && ar <= 13, br > 0 && br <= 13
		retained := nativePrefixRetained(a, b)
		for _, key := range []string{"all", r.Split + "/" + r.Form + "/" + r.Edit.Kind} {
			m := metrics[key]
			if m == nil {
				m = &nativeMetric{}
				metrics[key] = m
			}
			m.Examples++
			if ar > 0 && ar <= 5 {
				m.Ordinary5++
			}
			if br > 0 && br <= 5 {
				m.Protected5++
			}
			if before {
				m.Ordinary13++
			}
			if after {
				m.Protected13++
			}
			if !before && after {
				m.Gains++
			}
			if before && !after {
				m.Losses++
			}
			if !retained {
				m.PrefixChanges++
			}
		}
		if !after || !retained {
			failures = append(failures, map[string]any{"id": r.ID, "raw": r.Raw, "target": r.Target, "form": r.Form, "split": r.Split, "kind": r.Edit.Kind, "ordinary_rank": ar, "protected_rank": br, "prefix_retained": retained})
		}
	}
	hashes := make(map[string]string)
	for _, path := range []string{filepath.Join(source, "selected.jsonl"), filepath.Join(root, "ordinary.jsonl"), filepath.Join(root, "protected.jsonl")} {
		h, e := fileHash(path)
		if e != nil {
			return e
		}
		hashes[path] = h
	}
	return writeJSON(filepath.Join(root, "summary.json"), map[string]any{"metrics": metrics, "ordinary_codes": len(ordinary), "protected_codes": len(protected), "sha256": hashes, "failures": failures, "notes": []string{"Synthetic dictionary examples; not real-user typo accuracy.", "AI off, warm scratch schemas, no learning. Fixed five-candidate first page and thirteen-candidate capture.", "Full-span recall requires the capture tool to confirm a whole-input commit.", "Prefix comparison includes text, comment, rank and verified span; protected later-page recall may regress."}})
}
