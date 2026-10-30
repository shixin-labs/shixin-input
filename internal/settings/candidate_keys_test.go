package settings

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCandidateKeys(t *testing.T) {
	for _, id := range []string{"off", "control", "alt"} {
		t.Run(id, func(t *testing.T) {
			s := Default()
			s.CandidateKeys = id
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := Save(path, s); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil || got.CandidateKeys != id {
				t.Fatalf("round trip: %q, %v", got.CandidateKeys, err)
			}
			files, err := Compile(got)
			if err != nil {
				t.Fatal(err)
			}
			keys, enabled := CandidateKeyPair(id)
			for _, schema := range Schemes {
				patch := string(files[schema.ID+".custom.yaml"])
				if enabled {
					for i, key := range keys {
						want := "when: has_menu, accept: " + quote(key) + ", send: Control+" + string(rune('2'+i))
						if !strings.Contains(patch, want) {
							t.Errorf("%s lacks %s", schema.ID, want)
						}
					}
				} else if strings.Contains(patch, "send: Control+2") || strings.Contains(patch, "send: Control+3") {
					t.Errorf("%s has candidate shortcuts when off", schema.ID)
				}
			}
		})
	}
	for _, content := range []string{`{}`, `{"candidate_keys":"invalid", "page_size":7}`} {
		got, err := Parse([]byte(content))
		if got.CandidateKeys != "off" {
			t.Errorf("missing/invalid candidate_keys = %q", got.CandidateKeys)
		}
		if strings.Contains(content, "invalid") && (err == nil || got.PageSize != 7) {
			t.Errorf("repair lost unrelated settings: %+v, %v", got, err)
		}
	}
}
