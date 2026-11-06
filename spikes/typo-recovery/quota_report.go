package main

import (
	"fmt"
	"path/filepath"
	"sort"
)

type quotaMetric struct {
	Rows         int `json:"rows"`
	Exhausted    int `json:"exhausted"`
	SearchHits   int `json:"search_hits"`
	BaselineHits int `json:"baseline_hits"`
	UnionHits    int `json:"union_hits"`
}

func abbrQuotaReport(root, source string) error {
	rows, err := lines[abbrExample](filepath.Join(source, "selected.jsonl"))
	if err != nil {
		return err
	}
	menus, err := lines[menu](filepath.Join(source, "menus.jsonl"))
	if err != nil {
		return err
	}
	baseline := map[string]map[string]bool{}
	for _, m := range menus {
		baseline[m.Code] = map[string]bool{}
		for _, c := range m.Candidates {
			if c.Full {
				baseline[m.Code][c.Text] = true
			}
		}
	}
	modes := []string{"search-0", "search-1", "search-2", "search-3", "search-4", "search-5", "search-6", "search-7", "search-8", "dp-1", "dp-4"}
	out := map[string]any{}
	hashes := map[string]string{}
	for _, mode := range modes {
		path := filepath.Join(root, mode+".jsonl")
		searches, e := lines[lexicalSearch](path)
		if e != nil {
			return e
		}
		qs := map[string]lexicalSearch{}
		ns := make([]int64, 0, len(searches))
		exhausted := 0
		allocs := 0
		for _, q := range searches {
			qs[q.Raw] = q
			ns = append(ns, q.NS)
			if q.Exhausted {
				exhausted++
			}
			allocs += q.Allocs
		}
		sort.Slice(ns, func(i, j int) bool { return ns[i] < ns[j] })
		metrics := map[string]quotaMetric{}
		for _, r := range rows {
			q, ok := qs[r.Raw]
			if !ok {
				return fmt.Errorf("missing query %q", r.Raw)
			}
			hit := false
			for _, h := range q.Hits {
				if h.Text == r.Target {
					hit = true
				}
			}
			for _, key := range []string{r.Split, r.Split + "/" + r.Form + "/" + r.Edit.Kind} {
				m := metrics[key]
				m.Rows++
				if q.Exhausted {
					m.Exhausted++
				}
				if hit {
					m.SearchHits++
				}
				if baseline[r.Raw][r.Target] {
					m.BaselineHits++
				}
				if hit || baseline[r.Raw][r.Target] {
					m.UnionHits++
				}
				metrics[key] = m
			}
		}
		h, e := fileHash(path)
		if e != nil {
			return e
		}
		hashes[mode] = h
		out[mode] = map[string]any{"metrics": metrics, "prefix_queries": len(searches), "prefix_exhausted": exhausted, "search_new_allocations": allocs, "p99_ns": ns[(len(ns)-1)*99/100]}
	}
	return writeJSON(filepath.Join(root, "comparison.json"), map[string]any{"runs": out, "hashes": hashes, "limitations": "search + native baseline union only, before canonical verification, final cap50 and inference; fixed four-word-per-reading packed image; repeatedly examined families; DP charges binary probes and alignment visits against 8192 work units, unlike legacy edge counter"})
}
