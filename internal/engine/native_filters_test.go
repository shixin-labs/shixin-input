package engine

import (
	"os"
	"path/filepath"
	"testing"

	"shixin/internal/policy"
)

// A tiny, deterministic table exercises the real native filters without a
// large dictionary build. Every process owns its own scratch user directory.
func TestNativeCandidateFilters(t *testing.T) {
	if os.Getenv("SHIXIN_CHILD") != "TestNativeCandidateFilters" {
		runInChild(t, "TestNativeCandidateFilters")
		return
	}
	root := t.TempDir()
	data, user := filepath.Join(root, "data"), filepath.Join(root, "user")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	schema := `schema: {schema_id: test, name: Test, version: '1'}
switches: [{name: ascii_mode, reset: 0}]
engine:
  processors: [ascii_composer, speller, selector, navigator, express_editor]
  segmentors: [ascii_segmentor, abc_segmentor, fallback_segmentor]
  translators: [table_translator]
  filters: [shixin_long_word_filter, shixin_english_demote_filter, shixin_v_filter, shixin_autocap_filter, uniquifier, shixin_pin_filter]
speller: {alphabet: abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ, delimiter: " '"}
translator: {dictionary: test, enable_user_dict: false, enable_completion: false, enable_sentence: false}
shixin_filters:
  long_word: true
  demote_english: true
  v_symbols: true
  autocap: true
  pins: ["ni\t泥"]
menu: {page_size: 10}
`
	dict := "---\nname: test\nversion: '1'\nsort: by_weight\n...\n你\tni\t100\n泥\tni\t90\n拟\tni\t80\n倪\tni\t70\n你好\tni\t60\nhello\tHello\t100\nhello\tHEllo\t100\nvan\tva\t100\nā\tva\t90\n"
	defaults, err := os.ReadFile("testdata/rime/default.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"test.schema.yaml": []byte(schema), "test.dict.yaml": []byte(dict), "default.yaml": defaults} {
		if err := os.WriteFile(filepath.Join(data, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	release, err := Lock(user)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	e, err := Start(Options{LibPath: librimePath(t), SharedDataDir: data, UserDataDir: user, LogDir: root, MinLogLevel: 2, Deploy: true, Warmup: []string{"ni"}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	if !e.Native() {
		t.Skip("requires librime-shixin")
	}
	cl := dial(t, e)
	cl.setASCII(false)
	for _, tc := range []struct{ code, want string }{{"ni", "泥"}, {"Hello", "Hello"}, {"HEllo", "HELLO"}, {"va", "ā"}} {
		if got := firstText(cl.typ(tc.code)); got != tc.want {
			t.Fatalf("%s first=%q want=%q", tc.code, got, tc.want)
		}
		if got := string(cl.key(' ', 0).Commit); got != tc.want {
			t.Fatalf("%s commit=%q", tc.code, got)
		}
	}
	// Reusing pooled filter translations must preserve pins over repeated
	// menus, paging and cancellation, with no Go key-path allocations.
	for range 20 {
		if firstText(cl.typ("ni")) != "泥" {
			t.Fatal("pin disappeared when filter pools were reused")
		}
		cl.key(policy.KeyEscape, 0)
	}
	if !raceEnabled {
		cycle := func() { cl.key('n', 0); cl.key('i', 0); cl.key(' ', 0) }
		cycle()
		if n := testing.AllocsPerRun(20, cycle); n != 0 {
			t.Fatalf("native filter key cycle: %g Go allocations", n)
		}
	}
}
