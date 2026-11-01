package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildStageDoesNotExposeNativeDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status")
	for _, text := range []string{"dictionaries", "reuse", "source YAML: private phrase", strings.Repeat("private", 1000)} {
		os.WriteFile(path, []byte(text), 0600)
		got := buildStage(path)
		if text == "reuse" || text == "dictionaries" {
			if got != text {
				t.Fatal(got)
			}
		} else if got != "unknown stage" {
			t.Fatalf("source text exposed: %q", got)
		}
	}
}
