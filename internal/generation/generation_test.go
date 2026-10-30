package generation

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"shixin/internal/settings"
)

func TestValidateWarmOnly(t *testing.T) {
	for _, tt := range []struct {
		name, defaults, schema string
		ok                     bool
	}{
		{"safe", "schema_list: [{schema: test_warm}]", "schema: {schema_id: test_warm}\nengine:\n  translators: [script_translator, table_translator@extra]\ntranslator: {enable_user_dict: false}\nextra: {enable_user_dict: false}", true},
		{"real default", "schema_list: [{schema: test}]", "", false},
		{"remembered real plus warm", "schema_list: [{schema: test_warm}, {schema: test}]", "", false},
		{"missing learning setting", "schema_list: [{schema: test_warm}]", "schema: {schema_id: test_warm}\nengine: {translators: [script_translator]}", false},
		{"extra namespace", "schema_list: [{schema: test_warm}]", "schema: {schema_id: test_warm}\nengine: {translators: [script_translator, table_translator@extra]}\ntranslator: {enable_user_dict: false}\nextra: {enable_user_dict: true}", false},
		{"lua", "schema_list: [{schema: test_warm}]", "schema: {schema_id: test_warm}\nengine: {translators: [lua_translator@bad]}", false},
		{"bad id", "schema_list: [{schema: test_warm}]", "schema: {schema_id: other_warm}\nengine: {translators: []}", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.Mkdir(filepath.Join(dir, "build"), 0o700)
			os.WriteFile(filepath.Join(dir, "build", "default.yaml"), []byte(tt.defaults), 0o600)
			os.WriteFile(filepath.Join(dir, "build", "test_warm.schema.yaml"), []byte(tt.schema), 0o600)
			if err := Validate(dir); (err == nil) != tt.ok {
				t.Fatalf("Validate = %v, want valid %v", err, tt.ok)
			}
		})
	}
}

// TestPrebuiltReuse builds like the bundle does (every schema, from the
// dictionary sources), then builds generations from data without those
// sources. Unrelated settings must reuse every prebuilt file; fuzzy spelling
// must rebuild only the affected prism.
func TestPrebuiltReuse(t *testing.T) {
	if os.Getenv("SHIXIN_CHILD") == "build" {
		s := settings.Default()
		if path := os.Getenv("SHIXIN_SNAPSHOT"); path != "" {
			var err error
			s, err = settings.Load(path)
			if err != nil {
				t.Fatal(err)
			}
		}
		o := Options{Lib: os.Getenv("SHIXIN_LIBRIME"), Data: os.Getenv("SHIXIN_DATA"), Prebuilt: os.Getenv("SHIXIN_PREBUILT"),
			Dir: os.Getenv("SHIXIN_DIR"), Settings: s, All: os.Getenv("SHIXIN_ALL") != ""}
		if err := Build(o); err != nil {
			t.Fatal(err)
		}
		return
	}
	lib := os.Getenv("SHIXIN_LIBRIME")
	data, _ := filepath.Abs("../../build/rime-data")
	if _, err := os.Stat(filepath.Join(data, "rime_ice.schema.yaml")); testing.Short() || lib == "" || err != nil {
		t.Skip("needs SHIXIN_LIBRIME and build/rime-data")
	}
	tmp := t.TempDir()
	// librime's service is process-global: one build per child process.
	build := func(t *testing.T, data, prebuilt, dir string, all bool, s settings.Settings) {
		t.Helper()
		snapshot := dir + ".json"
		if err := settings.Save(snapshot, s); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestPrebuiltReuse$")
		cmd.Env = append(os.Environ(), "SHIXIN_CHILD=build", "SHIXIN_DATA="+data, "SHIXIN_PREBUILT="+prebuilt, "SHIXIN_DIR="+dir, "SHIXIN_SNAPSHOT="+snapshot)
		if all {
			cmd.Env = append(cmd.Env, "SHIXIN_ALL=1")
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
	}
	build(t, data, "", filepath.Join(tmp, "bundle"), true, settings.Default())
	pre := filepath.Join(tmp, "prebuilt")
	os.Mkdir(pre, 0o700)
	bins, _ := filepath.Glob(filepath.Join(tmp, "bundle", "build", "*.bin"))
	for _, b := range bins {
		if err := os.Link(b, filepath.Join(pre, filepath.Base(b))); err != nil {
			t.Fatal(err)
		}
	}
	for _, sc := range settings.Schemes {
		if _, err := os.Stat(filepath.Join(pre, sc.ID+".prism.bin")); err != nil {
			t.Fatalf("bundle prism for %s: %v", sc.ID, err)
		}
	}
	// The bundle's data: everything but the dictionary sources.
	shipped := filepath.Join(tmp, "data")
	os.Mkdir(shipped, 0o700)
	entries, _ := os.ReadDir(data)
	for _, e := range entries {
		if n := e.Name(); n != "cn_dicts" && !strings.HasSuffix(n, ".dict.yaml") {
			os.Symlink(filepath.Join(data, n), filepath.Join(shipped, n))
		}
	}
	for _, name := range []string{"defaults", "page-size", "non-spelling", "fuzzy", "double-pinyin-fuzzy", "ziranma", "ziranma-fuzzy", "mixed-pinyin", "mixed-pinyin-fuzzy"} {
		t.Run(name, func(t *testing.T) {
			s := settings.Default()
			wantPrism := ""
			switch name {
			case "page-size":
				s.PageSize = 7
			case "non-spelling":
				s.Emoji, s.Traditional, s.MixedEnglish = false, true, false
			case "ziranma":
				s.Scheme = "double_pinyin"
			case "mixed-pinyin", "mixed-pinyin-fuzzy":
				s.Scheme, s.FullPinyinFallback = "double_pinyin_flypy", true
				if name == "mixed-pinyin-fuzzy" {
					s.Fuzzy = []string{"z=zh", "an=ang"}
				}
				wantPrism = s.Scheme + ".prism.bin"
			case "fuzzy", "double-pinyin-fuzzy", "ziranma-fuzzy":
				s.Fuzzy = []string{"z=zh"}
				if name == "double-pinyin-fuzzy" {
					s.Scheme = "double_pinyin_flypy"
				}
				if name == "ziranma-fuzzy" {
					s.Scheme = "double_pinyin"
				}
				wantPrism = s.Scheme + ".prism.bin"
			}
			dir := filepath.Join(tmp, name)
			start := time.Now()
			build(t, shipped, pre, dir, false, s)
			t.Logf("build with prebuilt data: %v", time.Since(start).Round(time.Millisecond))
			rebuilt, err := filepath.Glob(filepath.Join(dir, "build", "*.bin"))
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			if wantPrism != "" {
				want = []string{wantPrism}
			}
			for i, path := range rebuilt {
				rebuilt[i] = filepath.Base(path)
			}
			if !slices.Equal(rebuilt, want) {
				t.Fatalf("rebuilt %v, want %v", rebuilt, want)
			}
		})
	}
}
