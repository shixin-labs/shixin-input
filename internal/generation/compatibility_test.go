package generation

import (
	"os"
	"path/filepath"
	"testing"

	"shixin/internal/settings"
)

func TestMatchesSettings(t *testing.T) {
	dir := t.TempDir()
	s := settings.Default()
	input := filepath.Join(dir, "input")
	if _, err := settings.Apply(input, s); err != nil {
		t.Fatal(err)
	}
	match := func(s settings.Settings, want bool) {
		t.Helper()
		if got, err := MatchesSettings(dir, s); err != nil || got != want {
			t.Fatalf("matches %v, want %v: %v", got, want, err)
		}
	}
	match(s, true)
	front := settings.Default()
	front.FontFamily = "Songti SC"
	match(front, true)
	changed := settings.Default()
	changed.Hotkeys["forget"] = ""
	match(changed, false)
	// A snapshot parsed by the current code gains new defaults. Its JSON
	// can therefore match while its old compiled output is incompatible.
	if err := os.WriteFile(filepath.Join(input, "rime_ice.custom.yaml"), []byte("old compiler output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	match(s, false)
	if err := os.Remove(filepath.Join(input, "rime_ice.custom.yaml")); err != nil {
		t.Fatal(err)
	}
	match(s, false)
}
