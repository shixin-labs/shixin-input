package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type lexicalHit struct {
	Text  string  `json:"text"`
	Code  string  `json:"code"`
	Cost  int     `json:"cost"`
	Prior float64 `json:"prior"`
}
type lexicalSearch struct {
	Raw       string       `json:"raw"`
	States    int          `json:"states"`
	Edges     int          `json:"edges"`
	PeakQueue int          `json:"peak_queue"`
	Exhausted bool         `json:"exhausted"`
	NS        int64        `json:"ns"`
	Allocs    int          `json:"allocs"`
	Hits      []lexicalHit `json:"hits"`
}

func abbrRecoveryPrepare(root, source, data string) error {
	if e := os.Mkdir(root, 0755); e != nil {
		return e
	}
	hashes := map[string]string{}
	for _, name := range []string{"selected.jsonl", "menus.jsonl", "contexts.jsonl", "profile.id"} {
		b, e := os.ReadFile(filepath.Join(source, name))
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(root, name), b, 0644); e != nil {
			return e
		}
		hashes[name] = hash(string(b))
	}
	rows, e := lines[abbrExample](filepath.Join(root, "selected.jsonl"))
	if e != nil {
		return e
	}
	queries := map[string]bool{}
	for _, r := range rows {
		for i := 1; i <= len(r.Raw); i++ {
			queries[r.Raw[:i]] = true
		}
	}
	if e = os.WriteFile(filepath.Join(root, "queries.txt"), []byte(strings.Join(keys(queries), "\n")+"\n"), 0644); e != nil {
		return e
	}
	lex := map[string]float64{}
	skipped := 0
	accepted := 0
	for _, name := range []string{"8105", "base", "ext", "others", "tencent"} {
		path := filepath.Join(data, "cn_dicts", name+".dict.yaml")
		hashes[name], e = fileHash(path)
		if e != nil {
			return e
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		scan := bufio.NewScanner(f)
		scan.Buffer(make([]byte, 4096), 1<<20)
		body := false
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if line == "..." {
				body = true
				continue
			}
			if !body || line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			p := strings.Split(line, "\t")
			if len(p) < 2 {
				skipped++
				continue
			}
			syllables := strings.Fields(p[1])
			valid := len(syllables) > 0 && letters(strings.Join(syllables, ""))
			for _, s := range syllables {
				if !letters(s) || len(s) > 8 {
					valid = false
				}
			}
			for _, c := range p[0] {
				if !unicode.Is(unicode.Han, c) {
					valid = false
				}
			}
			if !valid || p[0] == "" {
				skipped++
				continue
			}
			weight := 1.
			if len(p) > 2 {
				if v, err := strconv.ParseFloat(p[2], 64); err == nil && v >= 0 {
					weight = v
				}
			}
			key := strings.Join(syllables, " ") + "\t" + p[0]
			if old, ok := lex[key]; !ok || weight > old {
				lex[key] = weight
			}
			accepted++
		}
		err := scan.Err()
		f.Close()
		if err != nil {
			return err
		}
	}
	f, e := os.OpenFile(filepath.Join(root, "syllable-lexicon.tsv"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	w := bufio.NewWriter(f)
	for _, key := range keys(lex) {
		if _, e = fmt.Fprintf(w, "%s\t%.10g\n", key, lex[key]); e != nil {
			f.Close()
			return e
		}
	}
	if e = w.Flush(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	hashes["syllable-lexicon.tsv"], e = fileHash(filepath.Join(root, "syllable-lexicon.tsv"))
	if e != nil {
		return e
	}
	return writeJSON(filepath.Join(root, "preparation.json"), map[string]any{"source_hashes": hashes, "rows": len(rows), "queries_including_prefixes": len(queries), "dictionary_rows_accepted": accepted, "dictionary_rows_skipped": skipped, "unique_reading_text_pairs": len(lex), "index_policy": "explicit segmented rime-ice readings only; Han-only text; <=32 full-spelling letters, syllables <=8; no gold insertion, no Tencent reading inference", "search_policy": "one swap or neighbor on full/initial/digraph typed forms; 2048 expanded states,8192 edge checks,1024 queue,4096 dedup slots; four zero-edit plus four corrected text candidates; overflow discards batch"})
}

// Dictionary-supported proposals are offline hypotheses, NOT native candidates.
// Preserve all five ordinary entries, then add up to eight unique hypotheses.
func lexicalMerge(baseline []candidate, hits []lexicalHit) ([]candidate, map[string]float64) {
	seen := map[string]bool{}
	costs := map[string]float64{}
	for _, c := range baseline {
		if c.Full {
			seen[c.Text] = true
			costs[c.Text] = 0
		}
	}
	var additions []candidate
	for _, h := range hits {
		cost := float64(h.Cost) / 10
		if old, ok := costs[h.Text]; !ok || cost < old {
			costs[h.Text] = cost
		}
		if !seen[h.Text] {
			additions = append(additions, candidate{Text: h.Text, Full: true})
			seen[h.Text] = true
		}
	}
	cut := min(5, len(baseline))
	out := append([]candidate{}, baseline[:cut]...)
	out = append(out, additions...)
	out = append(out, baseline[cut:]...)
	if len(out) > 50 {
		out = out[:50]
	}
	return out, costs
}

func abbrRecoveryPlan(root, priorRun string) error {
	rows, e := lines[abbrExample](filepath.Join(root, "selected.jsonl"))
	if e != nil {
		return e
	}
	wantRaw := map[string]bool{}
	for _, r := range rows {
		wantRaw[r.Raw] = true
	}
	searches, e := lines[lexicalSearch](filepath.Join(root, "search.jsonl"))
	if e != nil {
		return e
	}
	codes := map[string]bool{}
	for _, s := range searches {
		if wantRaw[s.Raw] {
			for _, h := range s.Hits {
				codes[h.Code] = true
			}
		}
	}
	profile, e := os.ReadFile(filepath.Join(root, "profile.id"))
	if e != nil {
		return e
	}
	cached := map[string]menu{}
	for _, path := range []string{filepath.Join(priorRun, "menus.jsonl"), filepath.Join(root, "menus.jsonl")} {
		ms, e := lines[menu](path)
		if e != nil {
			return e
		}
		for _, m := range ms {
			if m.Complete && m.Profile == strings.TrimSpace(string(profile)) && codes[m.Code] {
				if old, ok := cached[m.Code]; !ok || len(m.Candidates) > len(old.Candidates) {
					cached[m.Code] = m
				}
			}
		}
	}
	dir := filepath.Join(root, "verification")
	if e = os.Mkdir(dir, 0755); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "profile.id"), profile, 0644); e != nil {
		return e
	}
	var wanted []codeRow
	var done []menu
	for _, code := range keys(codes) {
		wanted = append(wanted, codeRow{code})
		if m, ok := cached[code]; ok {
			done = append(done, m)
		}
	}
	if e = writeLines(filepath.Join(dir, "capture-codes.jsonl"), wanted); e != nil {
		return e
	}
	if e = writeLines(filepath.Join(dir, "menus-partial.jsonl"), done); e != nil {
		return e
	}
	fmt.Printf("%d proposal codes; %d reusable captures; %d new captures\n", len(wanted), len(done), len(wanted)-len(done))
	return shards(dir)
}
