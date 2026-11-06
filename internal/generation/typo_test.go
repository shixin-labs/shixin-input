package generation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTypoResourceManifest(t *testing.T) {
	dir := t.TempDir()
	stage := filepath.Join(dir, "build")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := typoManifest{Version: 1, SHA256: map[string]string{}}
	for _, name := range typoArtifacts {
		if err := os.WriteFile(filepath.Join(stage, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		h, err := fileSHA256(filepath.Join(stage, name))
		if err != nil {
			t.Fatal(err)
		}
		manifest.SHA256[name] = h
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "shixin_typo.manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateTypoResources(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range typoArtifacts {
		p := filepath.Join(stage, name)
		if err := os.WriteFile(p, []byte("stale"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := validateTypoResources(dir); err == nil {
			t.Fatalf("accepted changed %s", name)
		}
		if err := os.WriteFile(p, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
