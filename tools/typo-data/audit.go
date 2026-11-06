package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// auditPairs inspects the headerless Duyu CSV without approving or rewriting it.
// Mixed/punctuation rows are retained in raw data; they are not errors per se.
func auditPairs(path string, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h, err := hashFile(path)
	if err != nil {
		return err
	}
	r := csv.NewReader(f)
	r.FieldsPerRecord = 2
	counts := map[string]int{}
	seen := map[string]bool{}
	texts := map[string]bool{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("CSV record %d: %w", counts["rows"]+1, err)
		}
		counts["rows"]++
		text, py := row[0], row[1]
		key := text + "\x00" + py
		if seen[key] {
			counts["duplicate_pairs"]++
		}
		seen[key] = true
		texts[text] = true
		pure := text != ""
		for _, c := range text {
			if !unicode.Is(unicode.Han, c) {
				pure = false
			}
		}
		if pure {
			counts["han_only_rows"]++
		}
		if text == "" || py == "" {
			counts["empty_field_rows"]++
		}
		if pure && len(strings.Fields(py)) != utf8.RuneCountInString(text) {
			counts["han_only_syllable_count_mismatch"]++
		}
	}
	counts["unique_pairs"] = len(seen)
	counts["unique_targets"] = len(texts)
	return json.NewEncoder(out).Encode(struct {
		SHA    string         `json:"sha256"`
		Counts map[string]int `json:"counts"`
	}{h, counts})
}
