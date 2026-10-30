package settings

import (
	"strings"
	"testing"
)

func TestShuangpinLayouts(t *testing.T) {
	for _, scheme := range Schemes {
		layout := Shuangpin(scheme.ID)
		if scheme.ID == "rime_ice" {
			if layout != nil {
				t.Error("full pinyin has a double-pinyin chart")
			}
			continue
		}
		if layout == nil {
			t.Errorf("%s has no chart", scheme.ID)
			continue
		}
		keys, initials, finals := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, key := range layout.Keys {
			if keys[key.Key] || len(key.Key) != 1 || !strings.Contains("abcdefghijklmnopqrstuvwxyz;", key.Key) {
				t.Errorf("%s: invalid or duplicate key %q", scheme.ID, key.Key)
			}
			keys[key.Key] = true
			if key.Initial != "" {
				if initials[key.Initial] {
					t.Errorf("%s: repeated initial %s", scheme.ID, key.Initial)
				}
				initials[key.Initial] = true
			}
			for _, final := range key.Finals {
				finals[final] = true
			}
		}
		if len(keys) != 27 {
			t.Errorf("%s: %d physical keys, want 27", scheme.ID, len(keys))
		}
		for _, initial := range strings.Fields("b p m f d t n l g k h j q x zh ch sh r z c s y w") {
			if !initials[initial] {
				t.Errorf("%s: missing initial %s", scheme.ID, initial)
			}
		}
		for _, final := range strings.Fields("a o e i u ü ai ei ao ou an en ang eng ong er ia ie iao iu ian in iang ing iong ua uo uai ui uan un uang üe") {
			if !finals[final] {
				t.Errorf("%s: missing final %s", scheme.ID, final)
			}
		}
		for _, spelling := range layout.ZeroInitials {
			if len(spelling.Codes) == 0 {
				t.Errorf("%s: no code for %s", scheme.ID, spelling.Pinyin)
			}
			for _, code := range spelling.Codes {
				if len(code) != 2 || !keys[code[:1]] || !keys[code[1:]] {
					t.Errorf("%s: invalid zero-initial code %q", scheme.ID, code)
				}
			}
		}
	}
	if Shuangpin("unknown") != nil {
		t.Error("unknown scheme has a chart")
	}
}
