package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"shixin/internal/dictionary"
)

// sampleSources are what Settings would find with Sogou and Squirrel used
// before Shixin.
func sampleSources(dir string) []dictionary.Source {
	return []dictionary.Source{
		{ID: "sogou:user", IME: "sogou", Path: filepath.Join(dir, "sgim_gd_usr.bin"), Words: 1832},
		{ID: "sogou:cell:4.scel", IME: "sogou", Cell: true, Name: "网络流行新词", Path: filepath.Join(dir, "4.scel"), Words: 8397},
		{ID: "squirrel:rime_ice", IME: "squirrel", Name: "rime_ice", Path: filepath.Join(dir, "rime_ice.userdb"), Rime: true},
	}
}

// The guide and the Dictionary page list every source found; nothing is
// shown as a prompt over other pages.
func TestSourcesListed(t *testing.T) {
	for _, page := range []string{"start", "dictionary"} {
		a, _, _ := testApp(t)
		a.page = page
		a.sourcesLoaded(sampleSources(t.TempDir()), map[string]string{"sogou:user": sourceImported})
		tt := ui.NewTester(a.view, 880, 1400)
		tt.Frame()
		for _, want := range []string{T("s.d.ime.squirrel"), T("s.d.src.done"), T("s.d.src.again")} {
			if !tt.HasText(want) {
				t.Fatalf("%s: missing %q", page, want)
			}
		}
	}
	a, _, _ := testApp(t)
	a.sourcesLoaded(nil, map[string]string{})
	a.page = "dictionary"
	tt := ui.NewTester(a.view, 880, 1400)
	tt.Frame()
	if !tt.HasText(T("s.d.src.none")) {
		t.Fatal("no sources: missing the explanation")
	}
}

func TestSourceStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dictionary-sources.json")
	for _, content := range []string{"null", "[]", "{", ""} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		state := loadSourceState(path)
		state["sogou:user"] = sourceImported // must not panic
	}
}

func TestImportSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sgim_gd_usr.bin"), []byte("你好\tni hao\t3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, _ := testApp(t)
	a.sourcesLoaded(sampleSources(dir), map[string]string{})
	var requests []string
	a.requestDictionary = func(action, path string) error {
		if action == "import" {
			b, err := dictionary.ReadFile(path)
			if err != nil || len(b.Words) != 1 {
				t.Errorf("import table: %v %+v", err, b)
			}
		}
		requests = append(requests, action+" "+filepath.Base(path))
		return nil
	}
	a.dictionaryBusy = true
	a.runImport([]dictionary.Source{a.sources[0], a.sources[2]})()
	if len(requests) != 2 || requests[1] != "import-rime rime_ice.userdb" {
		t.Fatalf("requests %q", requests)
	}
	if a.dictionaryBusy || a.dictionaryError || a.sourceState["sogou:user"] != sourceImported || a.sourceState["squirrel:rime_ice"] != sourceImported {
		t.Fatalf("state %+v message %q", a.sourceState, a.dictionaryMessage)
	}
	if loadSourceState(a.sourceStatePath())["squirrel:rime_ice"] != sourceImported {
		t.Fatal("imported sources were not remembered")
	}

	// A failure stops the batch and records only what finished.
	b, _, _ := testApp(t)
	b.sourcesLoaded(sampleSources(dir), map[string]string{})
	b.requestDictionary = func(action, path string) error {
		if action == "import-rime" {
			return errors.New("dictionary: cannot read the Rime dictionary; quit that input method and retry")
		}
		return nil
	}
	b.runImport([]dictionary.Source{b.sources[0], b.sources[2]})()
	if !b.dictionaryError || b.sourceState["sogou:user"] != sourceImported || b.sourceState["squirrel:rime_ice"] != "" {
		t.Fatalf("failed batch: %+v %q", b.sourceState, b.dictionaryMessage)
	}
}
