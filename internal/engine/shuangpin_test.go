package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"shixin/internal/policy"
	"shixin/internal/settings"
)

// TestShuangpinCharts checks the reference charts against librime, using the
// real schemas' spelling algebra and a tiny dictionary with known candidates.
// No large dictionaries or user data are needed, and no projection is reimplemented.
func TestShuangpinCharts(t *testing.T) {
	if testing.Short() {
		t.Skip("needs rime-ice spelling rules")
	}
	if os.Getenv("SHIXIN_CHILD") != "TestShuangpinCharts" {
		runInChild(t, "TestShuangpinCharts")
		return
	}
	finals := map[string]struct{ initial, pinyin string }{
		"a": {"b", "ba"}, "o": {"b", "bo"}, "e": {"g", "ge"},
		"i": {"m", "mi"}, "u": {"b", "bu"}, "ü": {"n", "nv"},
		"ai": {"b", "bai"}, "ei": {"b", "bei"}, "ao": {"b", "bao"}, "ou": {"g", "gou"},
		"an": {"b", "ban"}, "en": {"b", "ben"}, "ang": {"b", "bang"}, "eng": {"b", "beng"},
		"ong": {"g", "gong"}, "ia": {"j", "jia"}, "ie": {"j", "jie"}, "iao": {"j", "jiao"},
		"iu": {"j", "jiu"}, "ian": {"j", "jian"}, "in": {"j", "jin"}, "iang": {"j", "jiang"},
		"ing": {"j", "jing"}, "iong": {"j", "jiong"}, "ua": {"g", "gua"}, "uo": {"g", "guo"},
		"uai": {"g", "guai"}, "ui": {"g", "gui"}, "uan": {"g", "guan"}, "un": {"g", "gun"},
		"uang": {"g", "guang"}, "üe": {"n", "nve"},
	}
	for _, fallback := range []bool{false, true} {
		for _, sc := range settings.Schemes {
			layout := settings.Shuangpin(sc.ID)
			if layout == nil {
				continue
			}
			t.Run(fmt.Sprintf("%s/full=%v", sc.ID, fallback), func(t *testing.T) {
				var checks []struct{ pinyin, code string }
				initialKeys := map[string]string{}
				for _, key := range layout.Keys {
					if key.Initial != "" {
						initialKeys[key.Initial] = key.Key
					}
				}
				for _, key := range layout.Keys {
					if key.Initial != "" {
						suffix := "a"
						if strings.Contains("jqx", key.Initial) {
							suffix = "i"
						}
						checks = append(checks, struct{ pinyin, code string }{key.Initial + suffix, key.Key + suffix})
					}
					for _, final := range key.Finals {
						if final == "er" {
							for _, spelling := range layout.ZeroInitials {
								if spelling.Pinyin == "er" {
									checks = append(checks, struct{ pinyin, code string }{"er", spelling.Codes[0][:1] + key.Key})
								}
							}
							continue
						}
						sample, ok := finals[final]
						if !ok {
							t.Fatalf("unknown chart final %q", final)
						}
						checks = append(checks, struct{ pinyin, code string }{sample.pinyin, initialKeys[sample.initial] + key.Key})
					}
				}
				for _, spelling := range layout.ZeroInitials {
					for _, code := range spelling.Codes {
						checks = append(checks, struct{ pinyin, code string }{spelling.Pinyin, code})
					}
				}

				var dictionary strings.Builder
				dictionary.WriteString("---\nname: test\nversion: '1'\nsort: by_weight\n...\n")
				seen := map[string]bool{}
				for _, check := range checks {
					if !seen[check.pinyin] {
						fmt.Fprintf(&dictionary, "字%s\t%s\t100\n", check.pinyin, check.pinyin)
						seen[check.pinyin] = true
					}
				}
				s := settings.Default()
				s.Scheme, s.FullPinyinFallback = sc.ID, fallback
				cl, _ := spellingEngine(t, s, dictionary.String())
				for _, check := range checks {
					if r := cl.typ(check.code); !hasCandidate(r, "字"+check.pinyin) {
						t.Errorf("chart code %s does not find %s", check.code, check.pinyin)
					}
					cl.key(policy.KeyEscape, 0)
					if fallback {
						if r := cl.typ(check.pinyin); !hasCandidate(r, "字"+check.pinyin) {
							t.Errorf("full spelling %s missing", check.pinyin)
						}
						cl.key(policy.KeyEscape, 0)
					}
				}
				t.Logf("verified %d initial, final and vowel-only spellings", len(checks))
			})
		}
	}
}

// spellingEngine uses the real layout algebra and the compiler's actual
// speller patches, with a tiny dictionary that can't hide invalid spellings
// through completion or sentence synthesis.
func spellingEngine(t *testing.T, s settings.Settings, dictionary string) (*client, *Engine) {
	t.Helper()
	dir := t.TempDir()
	data, user := filepath.Join(dir, "data"), filepath.Join(dir, "user")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	write := func(name string, b []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(data, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var source struct{ Speller map[string]any }
	if err := yaml.Unmarshal(read(filepath.Join("../../build/rime-data", s.Scheme+".schema.yaml")), &source); err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := yaml.Unmarshal(read("testdata/rime/test.schema.yaml"), &schema); err != nil {
		t.Fatal(err)
	}
	schema["speller"] = source.Speller
	schema["translator"] = map[string]any{"dictionary": "test", "enable_user_dict": false, "enable_completion": false, "enable_sentence": false}
	schema["menu"] = map[string]any{"page_size": 10}
	b, err := yaml.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	write("test.schema.yaml", b)
	write("default.yaml", read("testdata/rime/default.yaml"))
	write("test.dict.yaml", []byte(dictionary))
	release, err := Lock(user)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	files, err := settings.Compile(s)
	if err != nil {
		t.Fatal(err)
	}
	var generated struct{ Patch map[string]any }
	if err := yaml.Unmarshal(files[s.Scheme+".custom.yaml"], &generated); err != nil {
		t.Fatal(err)
	}
	patch := map[string]any{}
	for key, value := range generated.Patch {
		if strings.HasPrefix(key, "speller/") {
			patch[key] = value
		}
	}
	b, err = yaml.Marshal(map[string]any{"patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "test.custom.yaml"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := Start(Options{LibPath: librimePath(t), SharedDataDir: data, UserDataDir: user, LogDir: dir, MinLogLevel: 2, Deploy: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Stop)
	return dial(t, e), e
}
