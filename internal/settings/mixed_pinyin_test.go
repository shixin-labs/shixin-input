package settings

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestFullPinyinFallback(t *testing.T) {
	if Default().FullPinyinFallback {
		t.Fatal("mixed spelling must be opt-in")
	}
	old, err := Parse([]byte(`{"scheme":"double_pinyin_flypy"}`))
	if err != nil || old.FullPinyinFallback {
		t.Fatalf("old settings: %v", err)
	}
	if _, err := Parse([]byte(`{"full_pinyin_fallback":"yes"}`)); err == nil {
		t.Fatal("non-boolean accepted")
	}
	plain, err := Compile(Default())
	if err != nil {
		t.Fatal(err)
	}
	s := Default()
	s.Scheme, s.FullPinyinFallback = "double_pinyin_flypy", true
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !got.FullPinyinFallback || got.Scheme != s.Scheme {
		t.Fatal("mixed spelling preference lost")
	}
	files, err := Compile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files["rime_ice.custom.yaml"], plain["rime_ice.custom.yaml"]) {
		t.Fatal("fallback changed full-pinyin schema")
	}
	for _, scheme := range Schemes {
		if scheme.ID == "rime_ice" {
			continue
		}
		if strings.Contains(string(plain[scheme.ID+".custom.yaml"]), `\U$1`) {
			t.Fatalf("%s enables full spellings by default", scheme.ID)
		}
		patch := string(files[scheme.ID+".custom.yaml"])
		if !strings.Contains(patch, `derive/^(?!xx$)([a-z]+)$/\U$1/`) || !strings.Contains(patch, `xlit/ABCDEFGHIJKLMNOPQRSTUVWXYZ/abcdefghijklmnopqrstuvwxyz/`) {
			t.Fatalf("%s missing mixed branches", scheme.ID)
		}
	}
	if !EngineChanged(old, got) {
		t.Fatal("spelling change must rebuild the prism")
	}
}
