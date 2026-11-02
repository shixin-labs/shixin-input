package settings

import (
	"encoding/json"
	"testing"
)

// FuzzParse checks that Parse never yields settings Save would reject: a
// hand-edited or newer file is repaired into valid settings, and saving
// and reading them back changes nothing.
func FuzzParse(f *testing.F) {
	def, _ := json.Marshal(Default())
	f.Add(def)
	for _, s := range []string{
		`{}`,
		`null`,
		`{"page_size":0,"layout":"nope","fuzzy":["x=y"],"apps":[{"id":"","mode":"german"}]}`,
		`{"hotkeys":{"toggle":"Hyper+Q"},"phrases":[{"text":"#x","code":"AB"}]}`,
		`{"skin":"neon","font_size":-3,"rerank":"max","page_keys":["?"],"no_auto_space":[""]}`,
		`{"candidate_keys":"x","punctuation":"?","initial_mode":"klingon","theme":7,"scheme":"","select_char":"!","shift":"hyper"}`,
		`{"pins":[{"code":"a","text":""}],"hotkeys":null,"apps":null,"phrases":null}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		s, _ := Parse(b)
		if err := s.Validate(); err != nil {
			t.Fatalf("Parse(%q) left invalid settings: %v", b, err)
		}
		out, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Parse(out)
		if err != nil {
			t.Fatalf("reparse of %q: %v", out, err)
		}
		if out2, _ := json.Marshal(again); string(out2) != string(out) {
			t.Fatalf("round trip changed:\n%s\n%s", out, out2)
		}
	})
}
