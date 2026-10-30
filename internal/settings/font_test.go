package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFontPreference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, family := range []string{"", "Songti SC", "苹方-简", "Font on another machine"} {
		s := Default()
		s.FontFamily = family
		if err := Save(path, s); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || got.FontFamily != family {
			t.Fatalf("font %q: got %q, %v", family, got.FontFamily, err)
		}
		if EngineChanged(Default(), got) {
			t.Fatalf("font %q changes engine configuration", family)
		}
	}
	for _, document := range []string{`{"page_size":9}`, `{"font_family":"bad\nfont","page_size":9}`} {
		if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if got.FontFamily != "" || got.PageSize != 9 || (err != nil) != (document != `{"page_size":9}`) {
			t.Fatalf("load %s: font %q size %d err %v", document, got.FontFamily, got.PageSize, err)
		}
	}
}
