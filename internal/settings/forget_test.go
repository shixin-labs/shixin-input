package settings

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestForgetBindings(t *testing.T) {
	for _, key := range []string{"Control+Shift+BackSpace", "Control+Delete", ""} {
		s, err := Parse([]byte(`{"hotkeys":{"forget":"` + key + `"}}`))
		if err != nil || s.Hotkeys["forget"] != key {
			t.Fatalf("load %q: %v", key, err)
		}
		files, err := Compile(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, scheme := range Schemes {
			var file struct{ Patch map[string]any }
			if err := yaml.Unmarshal(files[scheme.ID+".custom.yaml"], &file); err != nil {
				t.Fatal(err)
			}
			bindings := file.Patch["editor/bindings"].(map[string]any)
			want := "noop"
			if key == "Control+Delete" {
				want = "delete_candidate"
			}
			if bindings["Control+Delete"] != want || bindings["Shift+Delete"] != "delete" {
				t.Fatalf("%s %q: %v", scheme.ID, key, bindings)
			}
			if key == "Control+Shift+BackSpace" && bindings[key] != "delete_candidate" {
				t.Fatal("default shortcut not bound")
			}
		}
	}
	s, err := Parse([]byte(`{"hotkeys":{"punct":""}}`))
	if err != nil || s.Hotkeys["forget"] != "Control+Shift+BackSpace" {
		t.Fatal("old settings lost default forget shortcut")
	}
	s, err = Parse([]byte(`{"hotkeys":{"forget":"BackSpace","punct":""},"page_size":9}`))
	if err == nil || s.Hotkeys["forget"] != "Control+Shift+BackSpace" || s.Hotkeys["punct"] != "" || s.PageSize != 9 {
		t.Fatal("invalid shortcut repair discarded valid settings")
	}
}
