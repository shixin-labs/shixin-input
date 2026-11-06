package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type publicContext struct {
	Target        string `json:"target"`
	Context       string `json:"context"`
	SourceLine    int    `json:"source_line"`
	SentenceHash  string `json:"sentence_sha256"`
	SelectionHash string `json:"selection_hash"`
}

func contextPrefix(sentence, target string) (string, bool) {
	if !strings.ContainsAny(sentence, "，。！？；") || utf8.RuneCountInString(sentence) < 12 || utf8.RuneCountInString(sentence) > 200 {
		return "", false
	}
	at := strings.Index(sentence, target)
	if at <= 0 {
		return "", false
	}
	prefix := []rune(sentence[:at])
	if len(prefix) > 64 {
		prefix = prefix[len(prefix)-64:]
	}
	han := 0
	for _, r := range prefix {
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	s := string(prefix)
	return s, han >= 6 && !strings.Contains(s, target)
}
func prepareContexts(root, csvPath string) error {
	rows, e := lines[example](filepath.Join(root, "selected.jsonl"))
	if e != nil {
		return e
	}
	targets := map[string]bool{}
	for _, r := range rows {
		targets[r.Target] = true
	}
	names := keys(targets)
	f, e := os.Open(csvPath)
	if e != nil {
		return e
	}
	defer f.Close()
	rd := csv.NewReader(f)
	rd.FieldsPerRecord = 2
	best := map[string]publicContext{}
	line := 0
	for {
		row, e := rd.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		line++
		sentence := row[0]
		if !strings.ContainsAny(sentence, "，。！？；") {
			continue
		}
		for _, target := range names {
			prefix, ok := contextPrefix(sentence, target)
			if !ok {
				continue
			}
			selection := hash("context-v1/" + target + "/" + sentence)
			if old, exists := best[target]; !exists || selection < old.SelectionHash {
				best[target] = publicContext{target, prefix, line, hash(sentence), selection}
			}
		}
	}
	out := []publicContext{}
	for _, target := range keys(best) {
		out = append(out, best[target])
	}
	if e = writeLines(filepath.Join(root, "contexts.jsonl"), out); e != nil {
		return e
	}
	sha, e := fileHash(csvPath)
	if e != nil {
		return e
	}
	fmt.Printf("Found source-derived prefixes for %d of %d targets\n", len(best), len(targets))
	return writeJSON(filepath.Join(root, "contexts-manifest.json"), map[string]any{"source": "Duyu/Pinyin-Hanzi", "source_sha256": sha, "source_rows": line, "targets": len(targets), "targets_with_context": len(best), "policy": "minimum SHA256(context-v1/target/sentence); punctuation-bearing 12-200-rune sentences; first target occurrence; at least 6 preceding Han characters; last 64 prefix runes; no suffix or target sent to scorer", "limitations": "Substring-derived contexts, not human typo traces. Source provenance and pronunciation quality are not independently validated. No model-dependent selection."})
}
