package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalPlanDeduplicatesReadingsAcrossTypedForms(t *testing.T) {
	source := t.TempDir()
	root := filepath.Join(t.TempDir(), "plan")
	rows := `{"target":"你好","syllables":["ni","hao"],"typed_form":"nihao"}
{"target":"你好","syllables":["ni","hao"],"typed_form":"nh"}
{"target":"你好","syllables":["ni","hao"],"typed_form":"nihoa"}
{"target":"好","syllables":["hao"]}
{"target":"好","syllables":["hao"]}
{"target":"纯净物","syllables":["chun","jing","wu"]}
`
	if err := os.WriteFile(filepath.Join(source, "selected.jsonl"), []byte(rows), 0600); err != nil {
		t.Fatal(err)
	}
	if err := canonicalCachePlan(root, source); err != nil {
		t.Fatal(err)
	}
	labels, err := os.ReadFile(filepath.Join(root, "labels.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if string(labels) != "chun jing wu\t纯净物\nni hao\t你好\n" {
		t.Fatalf("labels: %q", labels)
	}
	data, err := os.ReadFile(filepath.Join(root, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var counts map[string]any
	if err := json.Unmarshal(data, &counts); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{"unique_reading_text_pairs": 3, "two_or_three_syllable_pairs": 2, "excluded_other_lengths": 1} {
		if counts[key] != want {
			t.Fatalf("%s: got %v want %v", key, counts[key], want)
		}
	}
	if err := canonicalCachePlan(root, source); err == nil {
		t.Fatal("overwrote existing plan")
	}
}
