package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type auditWord struct {
	text  string
	prior float32
}
type auditRow struct {
	ID          string `json:"id"`
	Split       string `json:"split"`
	Form        string `json:"form"`
	Kind        string `json:"kind"`
	Raw         string `json:"raw"`
	Target      string `json:"target"`
	ReadingRank int    `json:"labeled_reading_rank"`
	Exhausted   bool   `json:"exhausted"`
	Recovered   bool   `json:"recovered"`
	Diagnosis   string `json:"diagnosis"`
}

// This audits the labeled reading only. Other readings can still recover a word.
func abbrAudit(root, source string) error {
	rows, err := lines[abbrExample](filepath.Join(root, "selected.jsonl"))
	if err != nil {
		return err
	}
	results, err := lines[lexicalSearch](filepath.Join(root, "search.jsonl"))
	if err != nil {
		return err
	}
	queries := map[string]lexicalSearch{}
	for _, q := range results {
		queries[q.Raw] = q
	}
	lex := map[string][]auditWord{}
	for _, r := range rows {
		lex[strings.Join(r.Syllables, " ")] = nil
	}
	path := filepath.Join(source, "syllable-lexicon.tsv")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		p := strings.Split(scan.Text(), "\t")
		if len(p) != 3 {
			return fmt.Errorf("invalid lexicon row")
		}
		if _, needed := lex[p[0]]; !needed {
			continue
		}
		freq, e := strconv.ParseFloat(p[2], 64)
		if e != nil {
			return e
		}
		lex[p[0]] = append(lex[p[0]], auditWord{p[1], float32(math.Log1p(freq))})
	}
	if err = scan.Err(); err != nil {
		return err
	}
	for _, words := range lex {
		sort.Slice(words, func(i, j int) bool {
			if words[i].prior != words[j].prior {
				return words[i].prior > words[j].prior
			}
			return words[i].text < words[j].text
		})
	}
	out := make([]auditRow, 0, len(rows))
	groups := map[string]map[string]int{}
	for _, r := range rows {
		q, ok := queries[r.Raw]
		if !ok {
			return fmt.Errorf("missing query %q", r.Raw)
		}
		a := auditRow{ID: r.ID, Split: r.Split, Form: r.Form, Kind: r.Edit.Kind, Raw: r.Raw, Target: r.Target, Exhausted: q.Exhausted}
		var prior float32
		for i, w := range lex[strings.Join(r.Syllables, " ")] {
			if w.text == r.Target {
				a.ReadingRank = i + 1
				prior = w.prior
				break
			}
		}
		for _, h := range q.Hits {
			if h.Text == r.Target {
				a.Recovered = true
			}
		}
		switch {
		case a.Recovered:
			a.Diagnosis = "recovered"
		case a.ReadingRank == 0:
			a.Diagnosis = "labeled_reading_absent"
		case a.ReadingRank > 4:
			a.Diagnosis = "labeled_reading_terminal_quota"
		case a.Exhausted:
			a.Diagnosis = "exhausted_with_retained_labeled_target"
		case labeledPathBelowQuota(prior, r.Edit.Kind, q.Hits):
			a.Diagnosis = "completed_labeled_path_below_output_quota"
		default:
			a.Diagnosis = "completed_retained_target_missing"
		}
		out = append(out, a)
		for _, key := range []string{"all", r.Split + "/" + r.Form + "/" + r.Edit.Kind} {
			if groups[key] == nil {
				groups[key] = map[string]int{}
			}
			groups[key][a.Diagnosis]++
		}
	}
	dest := filepath.Join(root, "audit")
	if err = os.Mkdir(dest, 0755); err != nil {
		return err
	}
	if err = writeLines(filepath.Join(dest, "rows.jsonl"), out); err != nil {
		return err
	}
	hashes := map[string]string{}
	for _, p := range []string{path, filepath.Join(root, "selected.jsonl"), filepath.Join(root, "search.jsonl")} {
		h, e := fileHash(p)
		if e != nil {
			return e
		}
		hashes[p] = h
	}
	return writeJSON(filepath.Join(dest, "summary.json"), map[string]any{"groups": groups, "hashes": hashes, "rows": len(out), "limits": "Labeled reading only; absence/quota may overlap exhaustion. Completed retained misses require output-quota or search-correctness investigation, not automatic attribution to ranking."})
}

// Printed search priors have limited precision. Require a strict margin so
// rounded scores and ties are left unresolved rather than called quota losses.
func labeledPathBelowQuota(prior float32, kind string, hits []lexicalHit) bool {
	cost := 0
	switch kind {
	case "clean":
	case "swap":
		cost = 8
	case "neighbor":
		cost = 10
	default:
		return false
	}
	score := .3*float64(prior) - .2*float64(cost)
	better := 0
	for _, h := range hits {
		if (h.Cost == 0) == (cost == 0) && .3*h.Prior-.2*float64(h.Cost) > score+0.0001 {
			better++
		}
	}
	return better >= 4
}
