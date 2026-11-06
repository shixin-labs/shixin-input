// Offline policy experiment. Never imported by the input engine.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type hypothesis struct {
	Code  string  `json:"code"`
	Cost  int     `json:"cost"`
	Score float64 `json:"score"`
}
type plan struct {
	Raw       string       `json:"raw"`
	Hits      []hypothesis `json:"hits"`
	Work      int          `json:"work"`
	Exhausted bool         `json:"exhausted"`
	NS        int64        `json:"ns"`
}
type example struct {
	ID, Raw, Target, Form string
	Typed                 string `json:"typed_form"`
	Edit                  struct{ Kind string }
}
type candidate struct {
	Text string
	Full bool `json:"full_span"`
}
type menu struct {
	Code       string
	Complete   bool
	Candidates []candidate
}
type planner struct {
	Forms       map[string]float64
	Abbreviated map[string]bool
	Neighbors   [26]string
}

func read[T any](path string) ([]T, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 1<<20)
	var out []T
	for s.Scan() {
		var row T
		if e = json.Unmarshal(s.Bytes(), &row); e != nil {
			return nil, e
		}
		out = append(out, row)
	}
	return out, s.Err()
}
func write(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}
func writeRows[T any](path string, rows []T) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if e = enc.Encode(r); e != nil {
			return e
		}
	}
	return nil
}
func hashFile(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func valid(s string) bool {
	if len(s) == 0 || len(s) > 32 {
		return false
	}
	for _, c := range []byte(s) {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}
func loadPlanner(path string) (*planner, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	frequency := map[string]float64{}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		p := strings.Split(scan.Text(), "\t")
		if len(p) != 3 {
			return nil, fmt.Errorf("invalid lexicon row")
		}
		w, e := strconv.ParseFloat(p[2], 64)
		if e != nil || w < 0 || math.IsNaN(w) || math.IsInf(w, 0) {
			return nil, fmt.Errorf("invalid prior")
		}
		for _, s := range strings.Fields(p[0]) {
			if !valid(s) || len(s) > 6 {
				return nil, fmt.Errorf("invalid syllable %q", s)
			}
			frequency[s] += 1 + w
		}
	}
	if e = scan.Err(); e != nil {
		return nil, e
	}
	if len(frequency) == 0 {
		return nil, fmt.Errorf("empty lexicon")
	}
	p := &planner{Forms: map[string]float64{}, Abbreviated: map[string]bool{}}
	total := 0.
	for _, w := range frequency {
		total += w
	}
	put := func(s string, score float64, abbreviated bool) {
		if old, ok := p.Forms[s]; !ok || score > old {
			p.Forms[s] = score
			p.Abbreviated[s] = abbreviated
		}
	}
	for s, w := range frequency {
		score := math.Log(w / total)
		put(s, score, false)
		put(s[:1], score-2, true)
		if len(s) > 2 && (strings.HasPrefix(s, "zh") || strings.HasPrefix(s, "ch") || strings.HasPrefix(s, "sh")) {
			put(s[:2], score-2, true)
		}
	}
	var x, y [26]float64
	for row, keys := range []string{"qwertyuiop", "asdfghjkl", "zxcvbnm"} {
		for col, c := range []byte(keys) {
			x[c-'a'] = float64(col) + []float64{0, .25, .75}[row]
			y[c-'a'] = float64(row)
		}
	}
	for a := 0; a < 26; a++ {
		for b := 0; b < 26; b++ {
			if a != b && math.Hypot(x[a]-x[b], y[a]-y[b]) <= 1.3 {
				p.Neighbors[a] += string(byte(b) + 'a')
			}
		}
	}
	return p, nil
}
func better(a, b hypothesis) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.Cost != b.Cost {
		return a.Cost < b.Cost
	}
	return a.Code < b.Code
}
func (p *planner) query(raw string, cap int, budget time.Duration) plan {
	start := time.Now()
	r := plan{Raw: raw}
	if !valid(raw) {
		return r
	}
	var best [16]hypothesis
	n := 0
	spend := func() bool {
		if r.Work >= cap {
			r.Exhausted = true
			return false
		}
		r.Work++
		if budget > 0 && r.Work&63 == 0 && time.Since(start) >= budget {
			r.Exhausted = true
			return false
		}
		return true
	}
	score := func(code string) (float64, bool) {
		var dp [33]float64
		for i := 1; i <= len(code); i++ {
			dp[i] = math.Inf(-1)
		}
		for i := 0; i < len(code); i++ {
			if math.IsInf(dp[i], -1) {
				continue
			}
			for j := i + 1; j <= len(code) && j <= i+6; j++ {
				if !spend() {
					return 0, false
				}
				if prior, ok := p.Forms[code[i:j]]; ok && dp[i]+prior > dp[j] {
					dp[j] = dp[i] + prior
				}
			}
		}
		return dp[len(code)], !math.IsInf(dp[len(code)], -1)
	}
	add := func(code string, cost int) {
		v, ok := score(code)
		if !ok {
			return
		}
		h := hypothesis{code, cost, v - float64(cost)/4}
		i := 0
		for i < n && !better(h, best[i]) {
			i++
		}
		if i == 16 {
			return
		}
		for j := min(n, 15); j > i; j-- {
			best[j] = best[j-1]
		}
		best[i] = h
		n = min(n+1, 16)
	}
	buf := []byte(raw)
	for i := 0; i+1 < len(buf) && !r.Exhausted; i++ {
		if buf[i] == buf[i+1] {
			continue
		}
		buf[i], buf[i+1] = buf[i+1], buf[i]
		add(string(buf), 8)
		buf[i], buf[i+1] = buf[i+1], buf[i]
	}
	for i, c := range []byte(raw) {
		if r.Exhausted {
			break
		}
		for _, other := range []byte(p.Neighbors[c-'a']) {
			buf[i] = other
			add(string(buf), 10)
			if r.Exhausted {
				break
			}
		}
		buf[i] = c
	}
	r.NS = time.Since(start).Nanoseconds()
	if budget > 0 && time.Duration(r.NS) >= budget {
		r.Exhausted = true
	}
	if !r.Exhausted {
		r.Hits = append(r.Hits, best[:n]...)
	}
	return r
}
func prepare(root, source, lexicon string) error {
	if e := os.Mkdir(root, 0700); e != nil {
		return e
	}
	rows, e := read[example](source)
	if e != nil {
		return e
	}
	p, e := loadPlanner(lexicon)
	if e != nil {
		return e
	}
	rawSet := map[string]bool{}
	for _, r := range rows {
		rawSet[r.Raw] = true
	}
	var rawCodes []string
	for s := range rawSet {
		rawCodes = append(rawCodes, s)
	}
	sort.Strings(rawCodes)
	var plans []plan
	codes := map[string]bool{}
	exhausted := 0
	for _, raw := range rawCodes {
		r := p.query(raw, 32768, 900*time.Microsecond)
		plans = append(plans, r)
		codes[raw] = true
		if r.Exhausted {
			exhausted++
		}
		for _, h := range r.Hits {
			codes[h.Code] = true
		}
	}
	if e = writeRows(filepath.Join(root, "plans.jsonl"), plans); e != nil {
		return e
	}
	var sorted []string
	for s := range codes {
		sorted = append(sorted, s)
	}
	sort.Strings(sorted)
	type code struct {
		Code string `json:"code"`
	}
	var capture []code
	for _, s := range sorted {
		capture = append(capture, code{s})
	}
	if e = writeRows(filepath.Join(root, "codes.jsonl"), capture); e != nil {
		return e
	}
	a, e := hashFile(source)
	if e != nil {
		return e
	}
	b, e := hashFile(lexicon)
	if e != nil {
		return e
	}
	m := map[string]any{"labels": len(rows), "raw_codes": len(plans), "capture_codes": len(capture), "exhausted": exhausted, "source_sha256": a, "lexicon_sha256": b, "form_count": len(p.Forms), "hypothesis_cap": 16, "work_cap": 32768, "planner_deadline_ns": 900000, "score": "best sum log(syllable frequency/total), abbreviation penalty 2; minus edit cost/4; swap 8, neighbor 10; no gold labels used"}
	return write(filepath.Join(root, "preparation.json"), m)
}

type metric struct {
	N, Baseline, Available, CorrectionOnly, LostToPer20, HypothesisTarget int
	Candidates                                                            int
}

func candidatePool(p plan, menus map[string]menu, k, per int) (map[string]bool, error) {
	result := map[string]bool{}
	for _, h := range p.Hits[:min(k, len(p.Hits))] {
		m, ok := menus[h.Code]
		if !ok {
			return nil, fmt.Errorf("missing capture %s", h.Code)
		}
		for _, c := range m.Candidates[:min(per, len(m.Candidates))] {
			if c.Full {
				result[c.Text] = true
			}
		}
	}
	return result, nil
}

func report(root, source string) error {
	var preparation struct {
		Source string `json:"source_sha256"`
	}
	b, e := os.ReadFile(filepath.Join(root, "preparation.json"))
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &preparation); e != nil {
		return e
	}
	h, e := hashFile(source)
	if e != nil {
		return e
	}
	if h != preparation.Source {
		return fmt.Errorf("source hash changed")
	}
	labels, e := read[example](source)
	if e != nil {
		return e
	}
	plans, e := read[plan](filepath.Join(root, "plans.jsonl"))
	if e != nil {
		return e
	}
	if len(plans) == 0 {
		return fmt.Errorf("no plans")
	}
	menus, e := read[menu](filepath.Join(root, "menus.jsonl"))
	if e != nil {
		return e
	}
	pm := map[string]plan{}
	mm := map[string]menu{}
	for _, p := range plans {
		pm[p.Raw] = p
	}
	for _, m := range menus {
		if !m.Complete {
			return fmt.Errorf("incomplete capture %s", m.Code)
		}
		if _, exists := mm[m.Code]; exists {
			return fmt.Errorf("duplicate capture %s", m.Code)
		}
		mm[m.Code] = m
	}
	wanted, e := read[struct{ Code string }](filepath.Join(root, "codes.jsonl"))
	if e != nil {
		return e
	}
	if len(wanted) != len(mm) {
		return fmt.Errorf("capture count mismatch: %d of %d", len(mm), len(wanted))
	}
	for _, row := range wanted {
		if _, ok := mm[row.Code]; !ok {
			return fmt.Errorf("missing capture %s", row.Code)
		}
	}
	checks := map[string]int{}
	for _, name := range []string{"ipc-crosscheck.jsonl", "fresh-session-crosscheck.jsonl"} {
		rows, err := read[menu](filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, row := range rows {
			actual, ok := mm[row.Code]
			if !ok || !slices.Equal(actual.Candidates, row.Candidates) {
				return fmt.Errorf("crosscheck mismatch %s: %s", name, row.Code)
			}
		}
		checks[name] = len(rows)
	}
	if e := write(filepath.Join(root, "crosschecks.json"), checks); e != nil {
		return e
	}
	pool := func(raw string, k, per int) (map[string]bool, error) {
		p, ok := pm[raw]
		if !ok {
			return nil, fmt.Errorf("missing plan %s", raw)
		}
		return candidatePool(p, mm, k, per)
	}
	metrics := map[string]*metric{}
	var losses []map[string]any
	for _, r := range labels {
		base, ok := mm[r.Raw]
		if !ok {
			return fmt.Errorf("missing raw capture")
		}
		baseline := false
		for _, c := range base.Candidates[:min(10, len(base.Candidates))] {
			if c.Full && c.Text == r.Target {
				baseline = true
			}
		}
		for _, k := range []int{2, 4, 8, 16} {
			p10, e := pool(r.Raw, k, 10)
			if e != nil {
				return e
			}
			p20, e := pool(r.Raw, k, 20)
			if e != nil {
				return e
			}
			hyp := false
			for _, h := range pm[r.Raw].Hits[:min(k, len(pm[r.Raw].Hits))] {
				if h.Code == r.Typed {
					hyp = true
				}
			}
			for _, cohort := range []string{"all", r.Edit.Kind, r.Form + "/" + r.Edit.Kind} {
				key := fmt.Sprintf("%d/%s", k, cohort)
				m := metrics[key]
				if m == nil {
					m = &metric{}
					metrics[key] = m
				}
				m.N++
				if baseline {
					m.Baseline++
				}
				if baseline || p10[r.Target] {
					m.Available++
				}
				if p10[r.Target] {
					m.CorrectionOnly++
				}
				if !baseline && !p10[r.Target] && p20[r.Target] {
					m.LostToPer20++
				}
				if hyp {
					m.HypothesisTarget++
				}
				m.Candidates += len(p10)
			}
			if !baseline && !p10[r.Target] && p20[r.Target] {
				losses = append(losses, map[string]any{"k": k, "raw": r.Raw, "target": r.Target, "kind": r.Edit.Kind, "form": r.Form})
			}
		}
	}
	var durations []int64
	maxWork := 0
	for _, p := range plans {
		durations = append(durations, p.NS)
		maxWork = max(maxWork, p.Work)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return write(filepath.Join(root, "summary.json"), map[string]any{"metrics": metrics, "losses_vs_20": losses, "planner_ns": map[string]int64{"p50": durations[len(durations)/2], "p99": durations[(len(durations)-1)*99/100], "max": durations[len(durations)-1]}, "max_planner_work": maxWork, "notes": []string{"Offline spelling-prior heuristic; no target-conditioned ranking and no AI.", "Per-spelling cap applies to raw native menu prefix; only verified full-span entries count. No refill beyond cap.", "Ordinary first ten kept separate and unchanged by construction, not a product UI test.", "Planner timing excludes native retrieval, replay/probes, IPC and ranking. No shared end-to-end deadline claim.", "Candidate pools can grow to 10*K before deduplication; availability is not top-1 accuracy.", "Reused synthetic development corpus; not held-out real typo accuracy."}})
}
func main() {
	mode := flag.String("mode", "prepare", "prepare or report")
	root := flag.String("root", "", "new experiment directory")
	source := flag.String("source", "build/typo-recovery/constrained-v1/selected.jsonl", "labels")
	lex := flag.String("lexicon", "build/typo-recovery/abbr-recovery-v1/syllable-lexicon.tsv", "public lexicon")
	flag.Parse()
	var e error
	if *mode == "prepare" {
		e = prepare(*root, *source, *lex)
	} else if *mode == "report" {
		e = report(*root, *source)
	} else if *mode == "capture" {
		e = captureNative(*root)
	} else if *mode == "export-model" {
		var p *planner
		p, e = loadPlanner(*lex)
		if e == nil {
			var keys []string
			for code := range p.Forms {
				keys = append(keys, code)
			}
			sort.Strings(keys)
			var b strings.Builder
			for _, code := range keys {
				abbr := 0
				if p.Abbreviated[code] {
					abbr = 1
				}
				fmt.Fprintf(&b, "%s\t%.17g\t%d\n", code, p.Forms[code], abbr)
			}
			e = os.WriteFile(filepath.Join(*root, "forms.tsv"), []byte(b.String()), 0600)
		}
	} else {
		e = fmt.Errorf("unknown mode")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
