package settings

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata golden files")

func TestDefaultIsValid(t *testing.T) {
	s := Default()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestZiranmaRoundTrip(t *testing.T) {
	s := Default()
	s.Scheme = "double_pinyin"
	s.Fuzzy = []string{"z=zh", "an=ang"}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	files, err := Compile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["default.custom.yaml"]), "- schema: double_pinyin\n") {
		t.Fatal("自然码 is not the active schema")
	}
}

func TestLoadSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	s, err := Load(path)
	if err != nil || !reflect.DeepEqual(s, Default()) {
		t.Fatalf("missing file: %+v %v", s, err)
	}
	s.PageSize = 7
	s.FontFamily = "Songti SC"
	s.Fuzzy = []string{"z=zh", "n=l"}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	// Unset fields keep defaults.
	os.WriteFile(path, []byte(`{"page_size": 9}`), 0o644)
	got, err = Load(path)
	if err != nil || got.PageSize != 9 || got.Scheme != "rime_ice" || got.Shift != "commit_code" {
		t.Fatalf("partial file: %+v %v", got, err)
	}
	if got.AutoSpace || !reflect.DeepEqual(got.NoAutoSpace, DevApps) {
		t.Fatalf("auto spacing defaults: %v %q", got.AutoSpace, got.NoAutoSpace)
	}
	// An invalid field is reset with an error; the rest is kept.
	os.WriteFile(path, []byte(`{"page_size": 99, "skin": "neon", "phrases": [{"code": "dz", "text": "家"}, {"code": "dz", "text": "家"}, {"code": "X", "text": "secret"}], "apps": [{"id": "a", "mode": "english"}, {"id": "a", "mode": "chinese"}], "page_keys": ["brackets", "tab"], "hotkeys": {"punct": "Control+q", "switcher": "F4"}}`), 0o644)
	got, err = Load(path)
	if err == nil || got.PageSize != Default().PageSize || got.Skin != Default().Skin {
		t.Fatalf("invalid file: %+v %v", got, err)
	}
	if len(got.Phrases) != 1 || len(got.Apps) != 1 || got.Apps[0].Mode != "english" ||
		!reflect.DeepEqual(got.PageKeys, []string{"tab"}) || got.Hotkeys["punct"] != "Control+period" || got.Hotkeys["switcher"] != "F4" {
		t.Fatalf("repair kept the wrong parts: %+v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("repaired settings invalid: %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error quotes phrase text: %v", err)
	}
	// Unparsable JSON falls back to all defaults.
	os.WriteFile(path, []byte(`{"page_size": `), 0o644)
	if got, err = Load(path); err == nil || !reflect.DeepEqual(got, Default()) {
		t.Fatalf("broken file: %+v %v", got, err)
	}
}

func TestValidate(t *testing.T) {
	for _, mod := range []func(*Settings){
		func(s *Settings) { s.Scheme = "luna_pinyin" },
		func(s *Settings) { s.PageSize = 2 },
		func(s *Settings) { s.Shift = "toggle" },
		func(s *Settings) { s.Fuzzy = []string{"q=w"} },
		func(s *Settings) { s.Layout = "diagonal" },
		func(s *Settings) { s.InitialMode = "auto" },
		func(s *Settings) { s.Punctuation = "auto" },
		func(s *Settings) { s.Rerank = "on" },
		func(s *Settings) { s.Theme = "blue" },
		func(s *Settings) { s.Skin = "nope" },
		func(s *Settings) { s.FontSize = 40 },
		func(s *Settings) { s.FontFamily = " Font " },
		func(s *Settings) { s.FontFamily = "Font\nName" },
		func(s *Settings) { s.FontFamily = "Font, Other" },
		func(s *Settings) { s.FontFamily = strings.Repeat("a", 257) },
		func(s *Settings) { s.SelectChar = "tab" },
		func(s *Settings) { s.PageKeys = []string{"brackets"} }, // 以词定字 uses [ ]
		func(s *Settings) { s.PageKeys = []string{"arrows"} },
		func(s *Settings) { s.Hotkeys = map[string]string{"punct": "Control+q"} },
		func(s *Settings) { s.Hotkeys = map[string]string{"teleport": ""} },
		func(s *Settings) { s.Hotkeys["forget"] = "BackSpace" },
		func(s *Settings) { s.CandidateKeys = "unknown" },
		func(s *Settings) { s.Phrases = []Phrase{{Code: "yx", Text: "a\tb"}} },
		func(s *Settings) { s.Phrases = []Phrase{{Code: "Yx", Text: "邮箱"}} },
		func(s *Settings) { s.Phrases = []Phrase{{Code: "yx", Text: "#x"}} },
		func(s *Settings) { s.Apps = []AppRule{{ID: "com.apple.Terminal", Mode: "auto"}} },
		func(s *Settings) { s.Apps = []AppRule{{ID: "", Mode: "english"}} },
		func(s *Settings) { s.Apps = []AppRule{{ID: "a", Mode: "english"}, {ID: "a", Mode: "chinese"}} },
		func(s *Settings) { s.Phrases = []Phrase{{Code: "yx", Text: "x"}, {Code: "yx", Text: "x"}} },
		func(s *Settings) { s.PageKeys = []string{"tab", "tab"} },
		func(s *Settings) { s.NoAutoSpace = []string{""} },
		func(s *Settings) { s.NoAutoSpace = []string{"a", "a"} },
	} {
		s := Default()
		mod(&s)
		if s.Validate() == nil {
			t.Errorf("accepted %+v", s)
		}
	}
}

func goldenSettings() Settings {
	s := Default()
	s.FullPinyinFallback = true
	s.PageSize = 7
	s.Shift = "commit_text"
	s.InitialMode = "english"
	s.Punctuation = "english"
	s.Fuzzy = []string{"z=zh", "an=ang"}
	s.SelectChar = "minus_equal"
	s.PageKeys = []string{"comma_period", "brackets"}
	s.MixedEnglish = false
	s.Traditional = true
	s.Hotkeys["traditional"] = "Control+Shift+4"
	s.Hotkeys["full_shape"] = "Control+comma"
	s.Hotkeys["switcher"] = "F4"
	s.CandidateKeys = "control"
	s.Phrases = []Phrase{{Code: "yx", Text: "shixin@example.com"}, {Code: "dz", Text: "北京市"}}
	return s
}

func TestPartialHotkeys(t *testing.T) {
	s, err := Parse([]byte(`{"hotkeys": {"punct": ""}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Hotkeys["punct"] != "" || s.Hotkeys["switcher"] != "Control+grave" {
		t.Errorf("hotkeys %v", s.Hotkeys)
	}
}

func TestKeyVariants(t *testing.T) {
	for in, want := range map[string][]string{
		"":                {},
		"F4":              {"F4"},
		"Control+period":  {"Control+period"},
		"Control+Shift+3": {"Control+Shift+3", "Control+Shift+numbersign"},
		"Control+Shift+F": {"Control+Shift+F", "Control+Shift+f"},
	} {
		if got := keyVariants(in); len(got) != len(want) || (len(got) > 0 && !reflect.DeepEqual(got, want)) {
			t.Errorf("keyVariants(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEngineChanged(t *testing.T) {
	a := Default()
	b := a
	b.Skin, b.Theme, b.FontSize, b.Layout = "ink", "dark", 20, "vertical"
	b.FontFamily = "Songti SC"
	if EngineChanged(a, b) {
		t.Error("appearance changes reload the engine")
	}
	// The engine applies these live (cmd/shixin-engine watches the file).
	b = a
	b.Apps = []AppRule{{ID: "com.apple.Terminal", Mode: "english"}}
	b.RememberAppMode, b.AutoSpace, b.NoAutoSpace = true, true, nil
	b.Rerank = "standard"
	if EngineChanged(a, b) {
		t.Error("per-app rules, auto spacing or 智能排序 reload the engine")
	}
	for _, mod := range []func(*Settings){
		func(s *Settings) { s.PageSize = 9 },
		func(s *Settings) { s.CandidateKeys = "alt" },
		func(s *Settings) { s.Punctuation = "english" },
		func(s *Settings) { s.Hotkeys["forget"] = "" },
		func(s *Settings) { s.FullPinyinFallback = true },
		func(s *Settings) { s.Phrases = []Phrase{{Code: "yx", Text: "x"}} },
	} {
		c := Default()
		mod(&c)
		if !EngineChanged(a, c) {
			t.Errorf("%+v doesn't reload the engine", c)
		}
	}
}

func TestCompileGolden(t *testing.T) {
	files, err := Compile(goldenSettings())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.Contains(string(files[n]), "lua") {
			t.Errorf("%s mentions lua", n)
		}
		if n != "default.custom.yaml" && n != "rime_ice.custom.yaml" && n != "double_pinyin_flypy.custom.yaml" && n != "double_pinyin.custom.yaml" && n != PhraseTable+".txt" {
			continue
		}
		golden := filepath.Join("testdata", n+".golden")
		if *update {
			os.WriteFile(golden, files[n], 0o644)
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(files[n]) {
			t.Errorf("%s differs from %s (run with -update if intended):\n%s", n, golden, files[n])
		}
	}
	if len(files) != 2+len(Schemes) {
		t.Errorf("%d files", len(files))
	}
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	changed, err := Apply(dir, Default())
	if err != nil || !changed {
		t.Fatalf("first apply: %v %v", changed, err)
	}
	if changed, err = Apply(dir, Default()); err != nil || changed {
		t.Fatalf("second apply: %v %v", changed, err)
	}
	s := Default()
	s.PageSize = 9
	if changed, err = Apply(dir, s); err != nil || !changed {
		t.Fatalf("changed apply: %v %v", changed, err)
	}
	// The user's own patch file is never touched.
	user := filepath.Join(dir, "shixin.user.yaml")
	os.WriteFile(user, []byte("mine"), 0o644)
	Apply(dir, Default())
	if b, _ := os.ReadFile(user); string(b) != "mine" {
		t.Fatal("shixin.user.yaml was modified")
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"speller":                   "speller",
		"table_translator@melt_eng": "table_translator@melt_eng",
		`derive/^z([^h])/zh$1/`:     `'derive/^z([^h])/zh$1/'`,
		"it's":                      `'it''s'`,
		"@x":                        `'@x'`,
	} {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestWatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	got := make(chan Settings, 4)
	stop := make(chan struct{})
	defer close(stop)
	go Watch(path, 10*time.Millisecond, stop, func(s Settings) { got <- s })
	time.Sleep(30 * time.Millisecond)
	s := Default()
	s.Skin = "ink"
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s.Skin != "ink" {
			t.Errorf("skin %q", s.Skin)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change seen")
	}
}
