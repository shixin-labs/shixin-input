package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type codeRow struct {
	Code string `json:"code"`
}

func shards(root string) error {
	all, e := lines[codeRow](filepath.Join(root, "capture-codes.jsonl"))
	if e != nil {
		return e
	}
	done, e := lines[menu](filepath.Join(root, "menus-partial.jsonl"))
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, m := range done {
		if !m.Complete {
			return fmt.Errorf("incomplete existing record")
		}
		seen[m.Code] = true
	}
	dir := filepath.Join(root, "shards")
	if e = os.Mkdir(dir, 0755); e != nil {
		return e
	}
	parts := [4][]codeRow{}
	n := 0
	for _, c := range all {
		if !seen[c.Code] {
			parts[n%4] = append(parts[n%4], c)
			n++
		}
	}
	for i, part := range parts {
		if e = writeLines(filepath.Join(dir, fmt.Sprintf("codes-%d.jsonl", i)), part); e != nil {
			return e
		}
	}
	return nil
}
func collect(root string) error {
	codes, e := lines[codeRow](filepath.Join(root, "capture-codes.jsonl"))
	if e != nil {
		return e
	}
	want := map[string]bool{}
	for _, c := range codes {
		want[c.Code] = true
	}
	pb, e := os.ReadFile(filepath.Join(root, "profile.id"))
	if e != nil {
		return e
	}
	pid := strings.TrimSpace(string(pb))
	files := []string{filepath.Join(root, "menus-partial.jsonl")}
	for i := 0; i < 4; i++ {
		files = append(files, filepath.Join(root, "shards", fmt.Sprintf("menus-%d.jsonl", i)))
	}
	records := map[string]json.RawMessage{}
	for _, f := range files {
		rs, e := lines[json.RawMessage](f)
		if e != nil {
			return e
		}
		for _, r := range rs {
			var m menu
			if e = json.Unmarshal(r, &m); e != nil {
				return e
			}
			if !m.Complete || m.Profile != pid || !want[m.Code] {
				return fmt.Errorf("invalid capture record %s", m.Code)
			}
			if _, ok := records[m.Code]; ok {
				return fmt.Errorf("duplicate capture %s", m.Code)
			}
			records[m.Code] = r
		}
	}
	if len(records) != len(want) {
		return fmt.Errorf("capture incomplete: %d of %d", len(records), len(want))
	}
	out := []json.RawMessage{}
	for _, c := range codes {
		out = append(out, records[c.Code])
	}
	return writeLines(filepath.Join(root, "menus.jsonl"), out)
}
