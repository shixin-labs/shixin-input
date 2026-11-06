package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAbbreviationFormsPreserveSyllableAlignment(t *testing.T) {
	cases := []struct {
		syllables []string
		want      map[string]string
	}{
		{[]string{"ni", "hao"}, map[string]string{"full": "nihao", "initials": "nh", "mixed_even": "nhao", "mixed_odd": "nih"}},
		{[]string{"zhong", "guo", "ren"}, map[string]string{"full": "zhongguoren", "initials": "zgr", "digraph_initials": "zhgr", "mixed_even": "zguor", "mixed_odd": "zhonggren"}},
	}
	for _, tc := range cases {
		forms := abbreviationForms(tc.syllables)
		if len(forms) != len(tc.want) {
			t.Fatalf("forms: %v", forms)
		}
		for _, f := range forms {
			if strings.Join(f.parts, "") != tc.want[f.name] || len(f.parts) != len(tc.syllables) {
				t.Fatalf("invalid %v", f)
			}
		}
	}
	if !strings.Contains(abbrNeighbors('a'), "s") || !strings.Contains(abbrNeighbors('i'), "j") {
		t.Fatal("missing expected neighbors")
	}
}
func TestAbbreviationWindowStopsAtPartialAndUsesRawCode(t *testing.T) {
	cs := make([]candidate, 50)
	for i := range cs {
		cs[i].Full = true
	}
	if abbreviationWindow("nh", cs) != 50 || abbreviationWindow("nih", cs) != 20 || abbreviationWindow("ah", cs) != 20 {
		t.Fatal("wrong raw-code cap")
	}
	cs[3].Full = false
	if abbreviationWindow("nh", cs) != 3 {
		t.Fatal("crossed partial span")
	}
	cs[0].Full = false
	if abbreviationWindow("nh", cs) != 0 {
		t.Fatal("reranked unknown prefix span")
	}
}
func TestAbbreviationPreparationIsReversibleAndSplitSafe(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "parent")
	corpus := filepath.Join(dir, "corpus")
	os.Mkdir(source, 0755)
	os.Mkdir(corpus, 0755)
	type input struct {
		example
		Syllables []string `json:"syllables"`
	}
	var parents []example
	for i, split := range []string{"dev", "test"} {
		r := example{ID: split, Family: split, Split: split, Target: split, Canonical: "nihao", Raw: "nihao"}
		r.Edit.Kind = "clean"
		parents = append(parents, r)
		syllables := []string{"ni", "hao"}
		if i == 1 {
			syllables = []string{"nan", "hai"}
			r.Canonical = "nanhai"
			r.Raw = "nanhai"
		}
		if e := writeLines(filepath.Join(corpus, split+".jsonl"), []input{{r, syllables}}); e != nil {
			t.Fatal(e)
		}
	}
	writeLines(filepath.Join(source, "selected.jsonl"), parents)
	os.WriteFile(filepath.Join(source, "profile.id"), []byte("fixture\n"), 0644)
	os.WriteFile(filepath.Join(source, "contexts.jsonl"), nil, 0644)
	hashes := []string{}
	for _, run := range []string{"a", "b"} {
		root := filepath.Join(dir, run)
		if e := abbrPrepare(root, source, corpus, 1); e != nil {
			t.Fatal(e)
		}
		rows, e := lines[abbrExample](filepath.Join(root, "selected.jsonl"))
		if e != nil {
			t.Fatal(e)
		}
		if len(rows) == 0 {
			t.Fatal("empty fixture")
		}
		seen := map[string]string{}
		for _, r := range rows {
			if old, ok := seen[r.Raw]; ok && old != r.Split {
				t.Fatal("cross-split raw code")
			}
			seen[r.Raw] = r.Split
			if r.Raw == "nh" {
				t.Fatal("shared clean initials not excluded")
			}
			if strings.Join(r.Parts, "") != r.Typed {
				t.Fatal("lost typed alignment")
			}
			if r.Edit.Kind == "clean" {
				if r.Raw != r.Typed || r.From != "" {
					t.Fatal("clean abbreviation charged an edit")
				}
			} else if r.Raw[:r.At]+r.From+r.Raw[r.At+len(r.To):] != r.Typed {
				t.Fatal("edit not reversible")
			}
		}
		h, e := fileHash(filepath.Join(root, "selected.jsonl"))
		if e != nil {
			t.Fatal(e)
		}
		hashes = append(hashes, h)
	}
	if hashes[0] != hashes[1] {
		t.Fatal("nondeterministic preparation")
	}
}
