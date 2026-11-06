package settings

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypoCorrectionOptIn(t *testing.T) {
	if Default().TypoCorrection {
		t.Fatal("must default off")
	}
	old, err := Parse([]byte(`{"scheme":"rime_ice"}`))
	if err != nil || old.TypoCorrection {
		t.Fatal("old settings must stay off", err)
	}
	if _, err := Parse([]byte(`{"typo_correction":"yes"}`)); err == nil {
		t.Fatal("non-boolean accepted")
	}
	plain, err := Compile(Default())
	if err != nil {
		t.Fatal(err)
	}
	s := Default()
	s.TypoCorrection = true
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !got.TypoCorrection {
		t.Fatal("preference lost", err)
	}
	if !EngineChanged(old, got) {
		t.Fatal("toggle must rebuild generation")
	}
	files, err := Compile(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(files["shixin_typo.forms.tsv"]) == 0 || len(plain["shixin_typo.forms.tsv"]) != 0 {
		t.Fatal("model opt-in")
	}
	for _, scheme := range Schemes {
		name := scheme.ID + ".custom.yaml"
		if scheme.ID != "rime_ice" {
			if !bytes.Equal(files[name], plain[name]) {
				t.Fatalf("changed shuangpin %s", name)
			}
			continue
		}
		for _, key := range []string{"translator/enable_correction: true", "translator/shixin_typo_separate: true", "shixin_typo/menu: true"} {
			if !strings.Contains(string(files[name]), key) {
				t.Fatalf("missing %s", key)
			}
		}
	}
}
