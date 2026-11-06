package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct{ s, code, reason string }{
		{"你好 你好 [ni3 hao3] /hello/", "nihao", ""},
		{"女兒 女儿 [nu:3 er2] /daughter/", "nver", ""},
		{"重慶 重庆 [Chong2 qing4] /Chongqing/", "chongqing", ""},
		{"花兒 花儿 [huar1] /flower/", "", "alignment"},
		{"A股 A股 [A gu3] /stock/", "", "non_han"},
		{"你好 你好 [ni hao] /hello/", "", "spelling"},
		{"garbage", "", "format"},
	}
	for _, tt := range tests {
		p, r := parseLine(tt.s, 42)
		if r != tt.reason || p.code() != tt.code {
			t.Errorf("parse %q: %q %q", tt.s, p.code(), r)
		}
	}
}

func TestEdits(t *testing.T) {
	p := phrase{Text: "你好", Syllables: []string{"ni", "hao"}}
	seen := map[string]bool{}
	for _, e := range mutations(p, "seed") {
		seen[e.Kind] = true
		raw := apply(p.code(), e)
		if e.From != p.code()[e.At:e.At+len(e.From)] {
			t.Fatal("invalid source alignment")
		}
		restored := raw[:e.At] + e.From + raw[e.At+len(e.To):]
		if restored != p.code() {
			t.Fatal("edit is not reversible")
		}
		switch e.Kind {
		case "clean":
			if raw != p.code() {
				t.Fatal("changed clean")
			}
		case "transpose_boundary":
			if e.At != 1 {
				t.Fatal("wrong boundary")
			}
		case "transpose_within":
			if e.At == 1 {
				t.Fatal("crossed boundary")
			}
		case "neighbor":
			if !strings.Contains(neighbors(e.From[0]), e.To) {
				t.Fatal("not a neighbor")
			}
		case "missing":
			if len(raw) != len(p.code())-1 {
				t.Fatal("not deletion")
			}
		case "repeat":
			if len(raw) != len(p.code())+1 {
				t.Fatal("not repetition")
			}
		default:
			t.Fatal(e.Kind)
		}
	}
	if len(seen) != 6 {
		t.Fatal(seen)
	}
	for _, c := range "qwertyuiopasdfghjklzxcvbnm" {
		for _, n := range neighbors(byte(c)) {
			if !strings.ContainsRune(neighbors(byte(n)), c) {
				t.Fatal("asymmetric geometry")
			}
		}
	}
	if !strings.Contains(neighbors('a'), "s") || !strings.Contains(neighbors('a'), "q") || strings.Contains(neighbors('a'), "p") {
		t.Fatal("incorrect geometry")
	}
}

func TestFamilies(t *testing.T) {
	ps := []phrase{
		{Text: "重庆", Traditional: "重慶", Syllables: []string{"chong", "qing"}},
		{Text: "重庆", Traditional: "重慶", Syllables: []string{"zhong", "qing"}},
		{Text: "钟情", Traditional: "鍾情", Syllables: []string{"zhong", "qing"}},
		{Text: "重慶", Traditional: "重慶", Syllables: []string{"chong", "qing"}},
	}
	fs := families(ps)
	if len(fs) != 1 {
		t.Fatalf("reading/traditional/homophone leakage: %v", fs)
	}
}

func TestPrepareDeterministicAndIsolated(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.gz")
	f, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	fixture := "# CC-CEDICT\n# License: CC BY-SA 4.0\n你好 你好 [ni3 hao3] /hello/\n女兒 女儿 [nu:3 er2] /daughter/\n女兒 女儿 [nu:3 er2] /duplicate/\n重慶 重庆 [Chong2 qing4] /Chongqing/\n鍾情 钟情 [zhong1 qing2] /love/\n重慶 重庆 [zhong4 qing4] /reading fixture/\n和平 和平 [he2 ping2] /peace/\n世界 世界 [shi4 jie4] /world/\n蘋果 苹果 [ping2 guo3] /apple/\n拼音 拼音 [pin1 yin1] /pinyin/\n中國 中国 [zhong1 guo2] /China/\n"
	if _, err = z.Write([]byte(fixture)); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err = run(input, a, "seed", 100); err != nil {
		t.Fatal(err)
	}
	if err = run(input, b, "seed", 100); err != nil {
		t.Fatal(err)
	}
	if err = run(input, a, "seed", 100); err == nil {
		t.Fatal("overwrote existing output")
	}
	rawSplits := map[string]string{}
	families := map[string]string{}
	ids := map[string]bool{}
	allCodes := map[string]bool{}
	for _, name := range []string{"train.jsonl", "dev.jsonl", "test.jsonl", "codes.jsonl", "manifest.json"} {
		ab, err := os.ReadFile(filepath.Join(a, name))
		if err != nil {
			t.Fatal(err)
		}
		bb, err := os.ReadFile(filepath.Join(b, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(ab) != string(bb) {
			t.Fatalf("non-reproducible %s", name)
		}
		if name == "manifest.json" {
			continue
		}
		sc := bufio.NewScanner(strings.NewReader(string(ab)))
		for sc.Scan() {
			if name == "codes.jsonl" {
				var code map[string]string
				if err = json.Unmarshal(sc.Bytes(), &code); err != nil {
					t.Fatal(err)
				}
				if len(code) != 1 || code["code"] == "" {
					t.Fatal("answer-bearing capture row")
				}
				if !allCodes[code["code"]] {
					t.Fatal("unmatched capture code")
				}
				delete(allCodes, code["code"])
				continue
			}
			var x example
			if err = json.Unmarshal(sc.Bytes(), &x); err != nil {
				t.Fatal(err)
			}
			if ids[x.ID] {
				t.Fatal("duplicate ID")
			}
			ids[x.ID] = true
			if s, ok := rawSplits[x.Raw]; ok && s != x.Split {
				t.Fatal("raw split leakage")
			}
			rawSplits[x.Raw] = x.Split
			if s, ok := families[x.Family]; ok && s != x.Split {
				t.Fatal("family split leakage")
			}
			families[x.Family] = x.Split
			if x.Context != "" || x.Raw != apply(x.Canonical, x.Edit) {
				t.Fatal("invented context or bad edit")
			}
			allCodes[x.Raw] = true
		}
		if err = sc.Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err := verify(a); err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 || len(allCodes) != 0 {
		t.Fatal("empty data or missing capture codes")
	}
}

func TestAuditHeaderlessCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pairs.csv")
	if err := os.WriteFile(path, []byte("你好,ni3 hao3\n你好,ni3 hao3\n你好,nihao\n\"你好，世界\",\"ni3 hao3 , shi4 jie4\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := auditPairs(path, &b); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal([]byte(b.String()), &r); err != nil {
		t.Fatal(err)
	}
	if r.Counts["rows"] != 4 || r.Counts["duplicate_pairs"] != 1 || r.Counts["han_only_syllable_count_mismatch"] != 1 {
		t.Fatal(r)
	}
}
