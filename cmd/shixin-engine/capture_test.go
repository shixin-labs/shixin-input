package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureConfig(t *testing.T) {
	base := config{lib: "library", data: "data", generation: "generation"}
	for _, tt := range []struct {
		name   string
		modify func(*config)
	}{
		{"live user", func(c *config) { c.user = "/live/user" }},
		{"public socket", func(c *config) { c.socket = "/live/socket" }},
		{"live settings", func(c *config) { c.settings = "/live/settings" }},
		{"builder", func(c *config) { c.builder = "/builder" }},
		{"discovery", func(c *config) { c.public = "/public" }},
		{"no generation", func(c *config) { c.generation = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.modify(&c)
			if err := captureConfig(&c, false); err == nil {
				t.Fatal("unsafe/missing capture path accepted")
			}
		})
	}
	if err := captureConfig(&base, true); err == nil {
		t.Fatal("worker promotion path accepted")
	}
	if err := captureConfig(&base, false); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(base.generation) || base.user != "" || base.socket != "" || base.settings != "" {
		t.Fatal("capture resolved live defaults")
	}
}

func TestCaptureRejectsUnsafeGenerationBeforeLibrime(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "build"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build/default.yaml"), []byte("schema_list: [{schema: live}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := runCapture(config{generation: root, lib: "/does-not-exist", data: "/unused"}, t.Logf, nil, strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "validate generation") {
		t.Fatalf("unsafe capture: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("unsafe generation announced readiness")
	}
}
