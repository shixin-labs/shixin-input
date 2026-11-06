package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Exports dictionary reading/text labels, not typed inputs or new corruption.
func canonicalCachePlan(root, source string) error {
	path := filepath.Join(source, "selected.jsonl")
	rows, err := lines[abbrExample](path)
	if err != nil {
		return err
	}
	all, eligible := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		reading := strings.Join(r.Syllables, " ")
		if reading == "" || r.Target == "" || strings.ContainsAny(reading+r.Target, "\t\n\r") {
			return fmt.Errorf("invalid reading/text label %q", r.ID)
		}
		key := reading + "\t" + r.Target
		all[key] = true
		if len(r.Syllables) == 2 || len(r.Syllables) == 3 {
			eligible[key] = true
		}
	}
	var ordered []string
	for key := range eligible {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	hash, err := fileHash(path)
	if err != nil {
		return err
	}
	if err = os.Mkdir(root, 0755); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "labels.tsv"), []byte(strings.Join(ordered, "\n")+"\n"), 0644); err != nil {
		return err
	}
	return writeJSON(filepath.Join(root, "source.json"), map[string]any{"source": path, "sha256": hash, "unique_reading_text_pairs": len(all), "two_or_three_syllable_pairs": len(eligible), "excluded_other_lengths": len(all) - len(eligible), "note": "Reused synthetic tuning families. Retention under the labeled canonical reading is not spelling-search recall or ranking accuracy."})
}
