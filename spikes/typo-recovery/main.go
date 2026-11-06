// Offline experiment orchestration. Product code must not import this package.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type example struct {
	ID        string `json:"id"`
	Family    string `json:"family_id"`
	Split     string `json:"split"`
	Target    string `json:"target"`
	Canonical string `json:"canonical"`
	Raw       string `json:"raw"`
	Edit      struct {
		Kind string `json:"kind"`
	} `json:"edit"`
}
type hit struct {
	Code  string  `json:"code"`
	Cost  int     `json:"cost"`
	Prior float64 `json:"prior"`
}
type searchResult struct {
	Raw       string `json:"raw"`
	States    int    `json:"states"`
	Exhausted bool   `json:"exhausted"`
	Exact     bool   `json:"exact"`
	Total     int    `json:"total"`
	NS        int64  `json:"ns"`
	Allocs    int    `json:"allocs"`
	Hits      []hit  `json:"hits"`
}
type candidate struct {
	Text string `json:"text"`
	Full bool   `json:"full_span"`
	Rank int    `json:"rank"`
}
type menu struct {
	Code       string      `json:"code"`
	Profile    string      `json:"profile_id"`
	Complete   bool        `json:"complete"`
	Candidates []candidate `json:"candidates"`
}

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func fileHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func lines[T any](path string) ([]T, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var rows []T
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 1<<20)
	for s.Scan() {
		var r T
		if e = json.Unmarshal(s.Bytes(), &r); e != nil {
			return nil, e
		}
		rows = append(rows, r)
	}
	return rows, s.Err()
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func writeLines[T any](path string, rows []T) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, r := range rows {
		if e = enc.Encode(r); e != nil {
			return e
		}
	}
	return w.Flush()
}
func keys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
func letters(s string) bool {
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

func prepare(root, corpus, data string, n int) error {
	if n < 1 {
		return errors.New("families must be positive")
	}
	if e := os.Mkdir(root, 0755); e != nil {
		return e
	}
	var selected []example
	counts := map[string]int{}
	sourceHashes := map[string]string{}
	for _, split := range []string{"dev", "test"} {
		path := filepath.Join(corpus, split+".jsonl")
		rows, e := lines[example](path)
		if e != nil {
			return e
		}
		sha, e := fileHash(path)
		if e != nil {
			return e
		}
		sourceHashes[split] = sha
		families := map[string]bool{}
		for _, r := range rows {
			families[r.Family] = true
		}
		ks := keys(families)
		sort.Slice(ks, func(i, j int) bool { return hash("recovery-v1/"+ks[i]) < hash("recovery-v1/"+ks[j]) })
		if len(ks) > n {
			ks = ks[:n]
		}
		keep := map[string]bool{}
		for _, k := range ks {
			keep[k] = true
		}
		for _, r := range rows {
			if keep[r.Family] {
				selected = append(selected, r)
				counts[split+"_"+r.Edit.Kind]++
			}
		}
	}
	if e := writeLines(filepath.Join(root, "selected.jsonl"), selected); e != nil {
		return e
	}
	raw := map[string]bool{}
	prefixes := map[string]bool{}
	for _, r := range selected {
		raw[r.Raw] = true
		for i := 1; i <= len(r.Raw); i++ {
			prefixes[r.Raw[:i]] = true
		}
	}
	if e := os.WriteFile(filepath.Join(root, "queries.txt"), []byte(strings.Join(keys(prefixes), "\n")+"\n"), 0644); e != nil {
		return e
	}
	// Read only explicitly annotated entries from the shipped dictionary sources.
	lex := map[string]float64{}
	for _, name := range []string{"8105", "base", "ext", "others", "tencent"} {
		path := filepath.Join(data, "cn_dicts", name+".dict.yaml")
		sha, e := fileHash(path)
		if e != nil {
			return e
		}
		sourceHashes[name] = sha
		f, e := os.Open(path)
		if e != nil {
			return e
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
			counts["dictionary_rows"]++
			if len(parts) < 2 {
				counts["dictionary_skipped"]++
				continue
			}
			code := strings.ReplaceAll(parts[1], " ", "")
			if !letters(code) {
				counts["dictionary_skipped"]++
				continue
			}
			weight := 1.0
			if len(parts) > 2 {
				if v, e := strconv.ParseFloat(parts[2], 64); e == nil {
					weight = v
				}
			}
			if old, ok := lex[code]; !ok || weight > old {
				lex[code] = weight
			}
		}
		e = sc.Err()
		f.Close()
		if e != nil {
			return e
		}
	}
	var b strings.Builder
	for _, code := range keys(lex) {
		fmt.Fprintf(&b, "%s\t%.10g\n", code, lex[code])
	}
	if e := os.WriteFile(filepath.Join(root, "lexicon.tsv"), []byte(b.String()), 0644); e != nil {
		return e
	}
	counts["lexicon_codes"] = len(lex)
	counts["query_prefixes"] = len(prefixes)
	counts["final_raw_codes"] = len(raw)
	return writeJSON(filepath.Join(root, "preparation.json"), map[string]any{"version": 1, "sampling": "SHA256(recovery-v1/family), lowest N per dev/test split; no tuning", "families_per_split": n, "source_sha256": sourceHashes, "counts": counts})
}
func plan(root string) error {
	rows, e := lines[example](filepath.Join(root, "selected.jsonl"))
	if e != nil {
		return e
	}
	sr, e := lines[searchResult](filepath.Join(root, "search.jsonl"))
	if e != nil {
		return e
	}
	byRaw := map[string]searchResult{}
	for _, r := range sr {
		byRaw[r.Raw] = r
	}
	codes := map[string]bool{}
	for _, r := range rows {
		q, ok := byRaw[r.Raw]
		if !ok {
			return fmt.Errorf("missing search %s", r.Raw)
		}
		codes[r.Raw] = true
		for _, h := range q.Hits {
			codes[h.Code] = true
		}
	}
	type codeRow struct {
		Code string `json:"code"`
	}
	out := []codeRow{}
	for _, c := range keys(codes) {
		out = append(out, codeRow{c})
	}
	return writeLines(filepath.Join(root, "capture-codes.jsonl"), out)
}
func profile(root, base string) error {
	m := map[string]string{}
	for _, name := range []string{"generation", "rime-data", "lib", "bin", "prebuilt", "share"} {
		dir := filepath.Join(base, name)
		e := filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			sha, e := fileHash(path)
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(base, path)
			if e != nil {
				return e
			}
			m[rel] = sha
			return nil
		})
		if e != nil {
			return e
		}
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(root, "runtime-files.json"), m); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(root, "profile.id"), []byte(hash(string(b))+"\n"), 0644)
}
func rank(cands []candidate, target string) int {
	for i, c := range cands {
		if c.Full && c.Text == target {
			return i + 1
		}
	}
	return 0
}

// Keep the displayed baseline #1. Admit at most four full-span correction
// candidates, ordered by channel cost, code prior, and within-code Rime rank.
// This is an offline text merge, NOT native candidate/span integration.
func merge(raw menu, hits []hit, menus map[string]menu) ([]candidate, error) {
	var correction []candidate
	seen := map[string]bool{}
	for _, c := range raw.Candidates {
		if c.Full {
			seen[c.Text] = true
		}
	}
	for _, h := range hits {
		m, ok := menus[h.Code]
		if !ok || !m.Complete {
			return nil, fmt.Errorf("missing/incomplete corrected menu %s", h.Code)
		}
		for _, c := range m.Candidates {
			if !c.Full {
				break
			}
			if !seen[c.Text] {
				seen[c.Text] = true
				correction = append(correction, c)
				if len(correction) == 4 {
					break
				}
			}
		}
		if len(correction) == 4 {
			break
		}
	}
	var out []candidate
	if len(raw.Candidates) > 0 {
		out = append(out, raw.Candidates[0])
	}
	out = append(out, correction...)
	if len(raw.Candidates) > 1 {
		out = append(out, raw.Candidates[1:]...)
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out, nil
}

type metric struct {
	N              int `json:"n"`
	Baseline1      int `json:"baseline_top1"`
	Baseline5      int `json:"baseline_top5"`
	Baseline20     int `json:"baseline_top20"`
	Augmented1     int `json:"augmented_top1"`
	Augmented5     int `json:"augmented_top5"`
	Augmented20    int `json:"augmented_top20"`
	Canonical20    int `json:"canonical_top20"`
	SpellingFound  int `json:"canonical_spelling_found"`
	LexiconCovered int `json:"canonical_in_search_lexicon"`
	PoolRecall     int `json:"uncapped_union_recall"`
	Exhausted      int `json:"exhausted"`
	Top1Changed    int `json:"top1_changed"`
	PageChanged    int `json:"page1_changed"`
	Lost20         int `json:"lost_from_top20"`
	Lost5          int `json:"lost_from_top5"`
}

func inc(r int, a, b, c *int) {
	if r == 1 {
		*a++
	}
	if r > 0 && r <= 5 {
		*b++
	}
	if r > 0 && r <= 20 {
		*c++
	}
}
func page(c []candidate) string {
	if len(c) > 5 {
		c = c[:5]
	}
	var b strings.Builder
	for _, x := range c {
		b.WriteString(x.Text)
		b.WriteByte(0)
	}
	return b.String()
}
func top(c []candidate) string {
	if len(c) == 0 {
		return ""
	}
	return c[0].Text
}
func percentiles(v []int64) map[string]int64 {
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	m := map[string]int64{}
	if len(v) == 0 {
		return m
	}
	for name, p := range map[string]float64{"p50": .5, "p95": .95, "p99": .99, "p999": .999, "max": 1} {
		i := int(float64(len(v)) * p)
		if i >= len(v) {
			i = len(v) - 1
		}
		m[name] = v[i]
	}
	return m
}
func reportResults(root string) error {
	rows, e := lines[example](filepath.Join(root, "selected.jsonl"))
	if e != nil {
		return e
	}
	sr, e := lines[searchResult](filepath.Join(root, "search.jsonl"))
	if e != nil {
		return e
	}
	ms, e := lines[menu](filepath.Join(root, "menus.jsonl"))
	if e != nil {
		return e
	}
	profileBytes, e := os.ReadFile(filepath.Join(root, "profile.id"))
	if e != nil {
		return e
	}
	pid := strings.TrimSpace(string(profileBytes))
	byRaw := map[string]searchResult{}
	var ns, states []int64
	allocs, exhausted, truncated := 0, 0, 0
	for _, r := range sr {
		byRaw[r.Raw] = r
		ns = append(ns, r.NS)
		states = append(states, int64(r.States))
		allocs += r.Allocs
		if r.Exhausted {
			exhausted++
		}
		if r.Total > 8 {
			truncated++
		}
	}
	menus := map[string]menu{}
	for _, m := range ms {
		if !m.Complete || m.Profile != pid {
			return errors.New("incomplete or wrong-profile capture")
		}
		if _, ok := menus[m.Code]; ok {
			return errors.New("duplicate capture")
		}
		menus[m.Code] = m
	}
	metrics := map[string]*metric{}
	var failures []map[string]any
	for _, r := range rows {
		q, ok := byRaw[r.Raw]
		if !ok {
			return fmt.Errorf("missing query %s", r.Raw)
		}
		baseline, ok := menus[r.Raw]
		if !ok {
			return fmt.Errorf("missing baseline %s", r.Raw)
		}
		canonical, ok := menus[r.Canonical]
		if !ok {
			return fmt.Errorf("missing clean control %s", r.Canonical)
		}
		aug, e := merge(baseline, q.Hits, menus)
		if e != nil {
			return e
		}
		key := r.Split + "/" + r.Edit.Kind
		m := metrics[key]
		if m == nil {
			m = &metric{}
			metrics[key] = m
		}
		m.N++
		br, ar, cr := rank(baseline.Candidates, r.Target), rank(aug, r.Target), rank(canonical.Candidates, r.Target)
		inc(br, &m.Baseline1, &m.Baseline5, &m.Baseline20)
		inc(ar, &m.Augmented1, &m.Augmented5, &m.Augmented20)
		if cr > 0 && cr <= 20 {
			m.Canonical20++
		}
		found := r.Raw == r.Canonical && q.Exact
		for _, h := range q.Hits {
			if h.Code == r.Canonical {
				found = true
			}
		}
		if found {
			m.SpellingFound++
		}
		if canonicalSearch, ok := byRaw[r.Canonical]; ok && canonicalSearch.Exact {
			m.LexiconCovered++
		}
		pool := br > 0 && br <= 20
		for _, h := range q.Hits {
			for _, c := range menus[h.Code].Candidates {
				if !c.Full {
					break
				}
				if c.Text == r.Target {
					pool = true
				}
			}
		}
		if pool {
			m.PoolRecall++
		}
		if q.Exhausted {
			m.Exhausted++
		}
		if top(baseline.Candidates) != top(aug) {
			m.Top1Changed++
		}
		if page(baseline.Candidates) != page(aug) {
			m.PageChanged++
		}
		if br > 0 && br <= 20 && (ar == 0 || ar > 20) {
			m.Lost20++
		}
		if br > 0 && br <= 5 && (ar == 0 || ar > 5) {
			m.Lost5++
		}
		if (ar == 0 || ar > 20) && len(failures) < 100 {
			failures = append(failures, map[string]any{"id": r.ID, "split": r.Split, "kind": r.Edit.Kind, "raw": r.Raw, "canonical": r.Canonical, "target": r.Target, "canonical_rank": cr, "spelling_found": found, "exhausted": q.Exhausted})
		}
	}
	result := map[string]any{"metrics": metrics, "query_count_including_prefixes": len(sr), "capture_count": len(ms), "search_latency_ns": percentiles(ns), "search_states": percentiles(states), "search_allocations": allocs, "exhausted_queries": exhausted, "truncated_code_sets": truncated, "notes": []string{"Synthetic dictionary corpus; no real typo accuracy claim.", "Whole-code lexical search; no librime graph integration.", "Latency is standalone warmed lookup only, excluding Rime, IPC, merge and display.", "Canonical controls measure a 20-candidate verified-acceptance ceiling, not dictionary presence.", "Conservative offline merge preserves raw top1, inserts up to four corrected entries, caps total at 20."}, "failures_first100": failures}
	return writeJSON(filepath.Join(root, "results.json"), result)
}
func main() {
	sourceRun := flag.String("source-run", "build/typo-recovery/run-v1", "parent pilot for abbreviation selection and contexts")
	sourceCSV := flag.String("context-source", "build/typo-data/raw/duyu/pinyin2hanzi.csv", "public paired-text source for contexts")
	llamaDir := flag.String("llama", "build/typo-recovery/rerank-runtime/lib", "llama library directory")
	modelPath := flag.String("model", "build/typo-recovery/rerank-runtime/model.gguf", "pinned ERNIE model")
	mode := flag.String("mode", "prepare", "prepare, plan, profile, shard, collect, report, contexts, rerank, rerank-summary, abbr-prepare, abbr-evaluate, abbr-recovery-prepare, abbr-recovery-plan, abbr-recovery-evaluate native-evaluate or native-lanes")
	root := flag.String("root", "", "experiment directory (new for prepare)")
	corpus := flag.String("corpus", "build/typo-data/cedict-v1", "corpus directory")
	data := flag.String("data", "build/typo-recovery/rime-data", "rime-ice snapshot")
	base := flag.String("runtime", "build/typo-recovery", "runtime snapshot")
	n := flag.Int("families", 100, "families per dev and test split")
	flag.Parse()
	var e error
	switch *mode {
	case "canonical-cache-plan":
		e = canonicalCachePlan(*root, *sourceRun)
	case "native-lanes":
		e = nativeLanes(*root, *sourceRun)
	case "native-evaluate":
		e = nativeEvaluate(*root, *sourceRun)
	case "abbr-quota-report":
		e = abbrQuotaReport(*root, *sourceRun)
	case "abbr-audit":
		e = abbrAudit(*root, *sourceRun)
	case "abbr-recovery-evaluate":
		e = abbrRecoveryEvaluate(*root, *data, *llamaDir, *modelPath)
	case "abbr-recovery-plan":
		e = abbrRecoveryPlan(*root, *sourceRun)
	case "abbr-recovery-prepare":
		e = abbrRecoveryPrepare(*root, *sourceRun, *data)
	case "abbr-prepare":
		e = abbrPrepare(*root, *sourceRun, *corpus, *n)
	case "abbr-evaluate":
		e = abbrEvaluate(*root, *data, *llamaDir, *modelPath)
	case "rerank-summary":
		e = rerankSummary(*root)
	case "contexts":
		e = prepareContexts(*root, *sourceCSV)
	case "rerank":
		e = rerankExperiment(*root, *data, *llamaDir, *modelPath)
	case "prepare":
		e = prepare(*root, *corpus, *data, *n)
	case "plan":
		e = plan(*root)
	case "profile":
		e = profile(*root, *base)
	case "shard":
		e = shards(*root)
	case "collect":
		e = collect(*root)
	case "report":
		e = reportResults(*root)
	default:
		e = errors.New("invalid mode")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
