package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func c(text string, full bool) candidate { return candidate{Text: text, Full: full} }
func TestMergeProtectsTopAndBoundsRecovery(t *testing.T) {
	raw := menu{Candidates: []candidate{c("raw", false), c("exact", true), c("rest", true)}}
	menus := map[string]menu{"fixed": {Complete: true, Candidates: []candidate{c("exact", true), c("one", true), c("two", true), c("three", true), c("four", true), c("five", true)}}}
	got, e := merge(raw, []hit{{Code: "fixed"}}, menus)
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != 7 || got[0].Text != "raw" || got[4].Text != "four" || got[5].Text != "exact" {
		t.Fatal(got)
	}
	if rank(got, "raw") != 0 || rank(got, "one") != 2 {
		t.Fatal("accepted partial span or wrong rank")
	}
}
func TestMergeStopsAtUnverifiedSpanAndRejectsMissing(t *testing.T) {
	menus := map[string]menu{"fixed": {Complete: true, Candidates: []candidate{c("one", true), c("partial", false), c("later", true)}}}
	got, e := merge(menu{}, []hit{{Code: "fixed"}}, menus)
	if e != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, e)
	}
	if _, e = merge(menu{}, []hit{{Code: "missing"}}, menus); e == nil {
		t.Fatal("accepted missing capture")
	}
}
func TestPreparationDoesNotIndexLabels(t *testing.T) {
	d := t.TempDir()
	corpus := filepath.Join(d, "corpus")
	data := filepath.Join(d, "data")
	os.MkdirAll(corpus, 0700)
	os.MkdirAll(filepath.Join(data, "cn_dicts"), 0700)
	for _, split := range []string{"dev", "test"} {
		if e := os.WriteFile(filepath.Join(corpus, split+".jsonl"), []byte(`{"id":"x","family_id":"f","split":"`+split+`","target":"不应加入","canonical":"secretgold","raw":"secretgolf","edit":{"kind":"neighbor"}}`+"\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, name := range []string{"8105", "base", "ext", "others", "tencent"} {
		s := "---\nname: fixture\n...\n你好\tni hao\t100\n未注音\t100\n"
		if e := os.WriteFile(filepath.Join(data, "cn_dicts", name+".dict.yaml"), []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	out := filepath.Join(d, "run")
	if e := prepare(out, corpus, data, 1); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(out, "lexicon.tsv"))
	if e != nil {
		t.Fatal(e)
	}
	if string(b) != "nihao\t100\n" {
		t.Fatal(string(b))
	}
	if e = prepare(out, corpus, data, 1); e == nil {
		t.Fatal("overwrote experiment")
	}
}
func TestPercentilesAndRanks(t *testing.T) {
	got := percentiles([]int64{4, 1, 3, 2})
	if got["p50"] != 3 || got["max"] != 4 {
		t.Fatal(got)
	}
	var a, b, c int
	inc(1, &a, &b, &c)
	inc(7, &a, &b, &c)
	if a != 1 || b != 1 || c != 2 {
		t.Fatal(a, b, c)
	}
}

func TestCollectRequiresEveryCodeAndPreservesRecords(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "shards"), 0700)
	os.WriteFile(filepath.Join(root, "profile.id"), []byte("profile\n"), 0600)
	if e := writeLines(filepath.Join(root, "capture-codes.jsonl"), []codeRow{{"nihao"}, {"pinyin"}}); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "menus-partial.jsonl"), []byte(""), 0600)
	for i := 0; i < 4; i++ {
		os.WriteFile(filepath.Join(root, "shards", fmt.Sprintf("menus-%d.jsonl", i)), []byte(""), 0600)
	}
	if e := collect(root); e == nil {
		t.Fatal("accepted incomplete shards")
	}
	raw := []byte("{\"code\":\"nihao\",\"profile_id\":\"profile\",\"complete\":true,\"extra_provenance\":\"preserve\"}\n{\"code\":\"pinyin\",\"profile_id\":\"profile\",\"complete\":true}\n")
	os.WriteFile(filepath.Join(root, "shards", "menus-0.jsonl"), raw, 0600)
	if e := collect(root); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(filepath.Join(root, "menus.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(got), "extra_provenance") {
		t.Fatal("discarded capture metadata")
	}
}
