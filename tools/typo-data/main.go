// Command typo-data prepares an offline synthetic pinyin benchmark from CC-CEDICT.
// It never opens Rime, live dictionaries, settings, or typing logs.
package main

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const version = "cedict-keyboard-v1"
const sourceURL = "https://www.mdbg.net/chinese/export/cedict/cedict_1_0_ts_utf-8_mdbg.txt.gz"
const licenseURL = "https://creativecommons.org/licenses/by-sa/4.0/"

type phrase struct {
	Text        string   `json:"target"`
	Traditional string   `json:"traditional"`
	Syllables   []string `json:"syllables"`
	Line        int      `json:"source_line"`
}

func (p phrase) code() string { return strings.Join(p.Syllables, "") }

type edit struct {
	Kind string `json:"kind"`
	At   int    `json:"canonical_byte_offset"`
	From string `json:"from"`
	To   string `json:"to"`
}
type example struct {
	ID     string `json:"id"`
	Family string `json:"family_id"`
	Split  string `json:"split"`
	Source string `json:"source"`
	phrase
	Canonical string `json:"canonical"`
	Raw       string `json:"raw"`
	Edit      edit   `json:"edit"`
	Origin    string `json:"origin"`
	Context   string `json:"context"`
	Collision bool   `json:"lexicon_code_collision"`
}
type report struct {
	Version      string            `json:"generator_version"`
	Seed         string            `json:"seed"`
	SourceURL    string            `json:"source_url"`
	SourceSHA    string            `json:"source_sha256"`
	SourceHeader []string          `json:"source_header"`
	License      string            `json:"license"`
	Attribution  string            `json:"attribution"`
	Limit        int               `json:"max_families"`
	Counts       map[string]int    `json:"counts"`
	Files        map[string]string `json:"output_sha256"`
}

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var lineRE = regexp.MustCompile(`^(\S+) (\S+) \[([^\]]+)\] /`)
var syllableRE = regexp.MustCompile(`^[a-z]+[1-5]$`)

// Reject unsupported records rather than guessing pronunciations or alignment.
func parseLine(s string, line int) (phrase, string) {
	m := lineRE.FindStringSubmatch(s)
	if m == nil {
		return phrase{}, "format"
	}
	n := utf8.RuneCountInString(m[2])
	if n < 2 || n > 10 {
		return phrase{}, "length"
	}
	for _, r := range m[1] + m[2] {
		if !unicode.Is(unicode.Han, r) {
			return phrase{}, "non_han"
		}
	}
	syllables := strings.Fields(strings.ToLower(strings.ReplaceAll(m[3], "u:", "v")))
	if len(syllables) != n {
		return phrase{}, "alignment"
	}
	for i, s := range syllables {
		if !syllableRE.MatchString(s) {
			return phrase{}, "spelling"
		}
		syllables[i] = s[:len(s)-1]
	}
	p := phrase{m[2], m[1], syllables, line}
	if len(p.code()) < 4 || len(p.code()) > 32 {
		return phrase{}, "code_length"
	}
	return p, ""
}

func readSource(path string) ([]phrase, []string, map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, nil, err
	}
	defer z.Close()
	counts := map[string]int{}
	var header []string
	var out []phrase
	seen := map[string]bool{}
	sc := bufio.NewScanner(z)
	sc.Buffer(make([]byte, 4096), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(s, "#") {
			header = append(header, s)
			continue
		}
		if s == "" {
			continue
		}
		counts["source_records"]++
		p, reason := parseLine(s, line)
		if reason != "" {
			counts["rejected_"+reason]++
			continue
		}
		key := p.Text + "\x00" + p.Traditional + "\x00" + p.code()
		if seen[key] {
			counts["duplicate_records"]++
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, nil, err
	}
	if len(out) == 0 {
		return nil, nil, nil, errors.New("no eligible CC-CEDICT records")
	}
	counts["eligible_records"] = len(out)
	return out, header, counts, nil
}

// Connected components keep shared Hanzi, traditional variants, and every
// reading of the same untoned code in one split, before any corruption.
func families(ps []phrase) map[string][]phrase {
	parent := map[string]string{}
	var root func(string) string
	root = func(s string) string {
		p, ok := parent[s]
		if !ok {
			parent[s] = s
			return s
		}
		if p != s {
			parent[s] = root(p)
		}
		return parent[s]
	}
	join := func(a, b string) {
		a, b = root(a), root(b)
		if a > b {
			a, b = b, a
		}
		parent[b] = a
	}
	for _, p := range ps {
		join("t:"+p.Text, "t:"+p.Traditional)
		join("t:"+p.Text, "p:"+p.code())
	}
	result := map[string][]phrase{}
	for _, p := range ps {
		k := root("t:" + p.Text)
		result[k] = append(result[k], p)
	}
	return result
}
func splitFor(seed, family string) string {
	h := sha256.Sum256([]byte(seed + "/split/" + family))
	v := (int(h[0])<<8 | int(h[1])) % 100
	if v < 80 {
		return "train"
	}
	if v < 90 {
		return "dev"
	}
	return "test"
}
func neighbors(c byte) string {
	rows := []string{"qwertyuiop", "asdfghjkl", "zxcvbnm"}
	offset := []float64{0, .25, .75}
	type point struct{ x, y float64 }
	pts := map[byte]point{}
	for y, row := range rows {
		for x := range row {
			pts[row[x]] = point{float64(x) + offset[y], float64(y)}
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
func apply(code string, e edit) string { return code[:e.At] + e.To + code[e.At+len(e.From):] }
func mutations(p phrase, seed string) []edit {
	code := p.code()
	boundaries := map[int]bool{}
	n := 0
	for _, s := range p.Syllables {
		n += len(s)
		boundaries[n] = true
	}
	choices := map[string][]edit{}
	add := func(e edit) { choices[e.Kind] = append(choices[e.Kind], e) }
	for i := range code {
		if i+1 < len(code) && code[i] != code[i+1] {
			kind := "transpose_within"
			if boundaries[i+1] {
				kind = "transpose_boundary"
			}
			add(edit{kind, i, code[i : i+2], string([]byte{code[i+1], code[i]})})
		}
		for _, c := range []byte(neighbors(code[i])) {
			add(edit{"neighbor", i, code[i : i+1], string(c)})
		}
		add(edit{"missing", i, code[i : i+1], ""})
		add(edit{"repeat", i, code[i : i+1], code[i:i+1] + code[i:i+1]})
	}
	out := []edit{{"clean", 0, "", ""}}
	for _, kind := range []string{"transpose_within", "transpose_boundary", "neighbor", "missing", "repeat"} {
		cs := choices[kind]
		if len(cs) == 0 {
			continue
		}
		sort.Slice(cs, func(i, j int) bool {
			return digest(fmt.Sprint(seed, p.Text, code, cs[i])) < digest(fmt.Sprint(seed, p.Text, code, cs[j]))
		})
		out = append(out, cs[0])
	}
	return out
}

func run(input, output, seed string, limit int) (err error) {
	if input == "" || output == "" || seed == "" || limit < 1 {
		return errors.New("input, new output directory, nonempty seed and positive max-families required")
	}
	ps, header, counts, err := readSource(input)
	if err != nil {
		return err
	}
	sha, err := hashFile(input)
	if err != nil {
		return err
	}
	fs := families(ps)
	counts["eligible_families"] = len(fs)
	keys := make([]string, 0, len(fs))
	for k := range fs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return digest(seed+"/sample/"+keys[i]) < digest(seed+"/sample/"+keys[j]) })
	if len(keys) > limit {
		keys = keys[:limit]
	}
	counts["selected_families"] = len(keys)
	// The output is a new directory; manifest.json is the completion marker.
	if err = os.Mkdir(output, 0755); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(output)
		}
	}()
	lexicon := map[string]bool{}
	for _, p := range ps {
		lexicon[p.code()] = true
	}
	rawSplit := map[string]string{}
	for _, k := range keys {
		for _, p := range fs[k] {
			rawSplit[p.code()] = splitFor(seed, k)
		}
	}
	handles := map[string]*os.File{}
	encoders := map[string]*json.Encoder{}
	for _, s := range []string{"train", "dev", "test", "codes"} {
		f, e := os.Create(filepath.Join(output, s+".jsonl"))
		if e != nil {
			return e
		}
		handles[s] = f
		defer f.Close()
		encoders[s] = json.NewEncoder(f)
	}
	codes := map[string]bool{}
	for _, k := range keys {
		split := splitFor(seed, k)
		counts[split+"_families"]++
		// Stable across dictionary ordering except source-line provenance.
		sort.Slice(fs[k], func(i, j int) bool {
			a, b := fs[k][i], fs[k][j]
			return a.Text+"/"+a.code()+"/"+a.Traditional < b.Text+"/"+b.code()+"/"+b.Traditional
		})
		seen := map[string]bool{}
		for _, p := range fs[k] {
			// Traditional variants with the same simplified text/reading are one example.
			pk := p.Text + "/" + p.code()
			if seen[pk] {
				continue
			}
			seen[pk] = true
			for _, e := range mutations(p, seed) {
				raw := apply(p.code(), e)
				if len(raw) < 1 || len(raw) > 32 {
					counts["excluded_capture_length"]++
					continue
				}
				if other, ok := rawSplit[raw]; ok && other != split {
					counts["excluded_cross_split_raw_collision"]++
					continue
				}
				rawSplit[raw] = split
				origin := "synthetic"
				if e.Kind == "clean" {
					origin = "dictionary_clean"
				}
				x := example{ID: digest(version + "/" + seed + "/" + pk + "/" + e.Kind + "/" + raw), Family: digest(k), Split: split, Source: "cc-cedict", phrase: p, Canonical: p.code(), Raw: raw, Edit: e, Origin: origin, Context: "", Collision: e.Kind != "clean" && lexicon[raw]}
				if err = encoders[split].Encode(x); err != nil {
					return err
				}
				counts[split+"_"+e.Kind]++
				counts["examples"]++
				if x.Collision {
					counts["lexicon_code_collisions"]++
				}
				codes[raw] = true
			}
		}
	}
	sorted := make([]string, 0, len(codes))
	for code := range codes {
		sorted = append(sorted, code)
	}
	sort.Strings(sorted)
	for _, code := range sorted {
		if err = encoders["codes"].Encode(struct {
			Code string `json:"code"`
		}{code}); err != nil {
			return err
		}
	}
	counts["capture_codes"] = len(sorted)
	outputHashes := map[string]string{}
	for name, f := range handles {
		if err = f.Close(); err != nil {
			return err
		}
		h, e := hashFile(filepath.Join(output, name+".jsonl"))
		if e != nil {
			return e
		}
		outputHashes[name+".jsonl"] = h
	}
	r := report{version, seed, sourceURL, sha, header, licenseURL, "CC-CEDICT community and MDBG; see preserved source_header. Derived benchmark: CC BY-SA 4.0. Synthetic edits by Shixin contributors.", limit, counts, outputHashes}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err = os.WriteFile(filepath.Join(output, "manifest.json"), b, 0644); err != nil {
		return err
	}
	complete = true
	fmt.Printf("Prepared %d examples from %d families; %d unique capture codes\n", counts["examples"], len(keys), len(codes))
	return nil
}
func main() {
	verifyDir := flag.String("verify", "", "verify a prepared directory against its manifest")
	audit := flag.String("audit-pairs", "", "audit headerless two-column Chinese-pinyin CSV only")
	input := flag.String("input", "", "downloaded CC-CEDICT v1 .txt.gz")
	output := flag.String("output", "", "new output directory (parent must exist)")
	seed := flag.String("seed", "shixin-typos-2026-11-v1", "deterministic split and edit seed")
	limit := flag.Int("max-families", 20000, "maximum connected phrase/reading families")
	flag.Parse()
	if *verifyDir != "" {
		if err := verify(*verifyDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Verified corpus hashes, edits, IDs, split isolation and capture coverage")
		return
	}
	if *audit != "" {
		if err := auditPairs(*audit, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(*input, *output, *seed, *limit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
