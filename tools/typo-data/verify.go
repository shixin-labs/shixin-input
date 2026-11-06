package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// verify checks the complete prepared artifact, independently of generation.
func verify(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var m report
	if err = json.Unmarshal(b, &m); err != nil {
		return err
	}
	if m.Version != version {
		return fmt.Errorf("unsupported generator %q", m.Version)
	}
	codes := map[string]bool{}
	rawSplit := map[string]string{}
	familySplit := map[string]string{}
	ids := map[string]bool{}
	total := 0
	for _, split := range []string{"train", "dev", "test", "codes"} {
		path := filepath.Join(dir, split+".jsonl")
		h, err := hashFile(path)
		if err != nil {
			return err
		}
		if h != m.Files[split+".jsonl"] {
			return fmt.Errorf("checksum mismatch: %s", split)
		}
		err = func() error {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if split == "codes" {
					var row map[string]string
					if err = json.Unmarshal(sc.Bytes(), &row); err != nil {
						return err
					}
					if len(row) != 1 || !codes[row["code"]] {
						return fmt.Errorf("invalid/duplicate capture code")
					}
					delete(codes, row["code"])
					continue
				}
				var x example
				if err = json.Unmarshal(sc.Bytes(), &x); err != nil {
					return err
				}
				if x.Split != split || x.Canonical != strings.Join(x.Syllables, "") || x.Context != "" || x.ID == "" || x.Family == "" {
					return fmt.Errorf("invalid row metadata")
				}
				if ids[x.ID] {
					return fmt.Errorf("duplicate example ID")
				}
				ids[x.ID] = true
				e := x.Edit
				if e.At < 0 || e.At+len(e.From) > len(x.Canonical) || x.Canonical[e.At:e.At+len(e.From)] != e.From {
					return fmt.Errorf("invalid alignment")
				}
				if apply(x.Canonical, e) != x.Raw {
					return fmt.Errorf("edit does not produce raw input")
				}
				expected := "synthetic"
				if e.Kind == "clean" {
					expected = "dictionary_clean"
					if x.Raw != x.Canonical {
						return fmt.Errorf("changed clean row")
					}
				}
				if x.Origin != expected {
					return fmt.Errorf("invalid origin")
				}
				if s, ok := rawSplit[x.Raw]; ok && s != split {
					return fmt.Errorf("raw code leaks across splits")
				}
				rawSplit[x.Raw] = split
				if s, ok := familySplit[x.Family]; ok && s != split {
					return fmt.Errorf("family leaks across splits")
				}
				familySplit[x.Family] = split
				if len(x.Raw) < 1 || len(x.Raw) > 32 {
					return fmt.Errorf("unsupported capture length")
				}
				for _, c := range x.Raw {
					if c < 'a' || c > 'z' {
						return fmt.Errorf("unsupported capture code")
					}
				}
				codes[x.Raw] = true
				total++
			}
			return sc.Err()
		}()
		if err != nil {
			return fmt.Errorf("%s: %w", split, err)
		}
	}
	if len(codes) != 0 || total != m.Counts["examples"] || len(rawSplit) != m.Counts["capture_codes"] {
		return fmt.Errorf("manifest counts or capture coverage mismatch")
	}
	return nil
}
