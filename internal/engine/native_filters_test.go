package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	cl := startNativeFilterEngine(t, "table_translator", "")
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

// Every selected segment keeps its menu, and so its filter translations,
// alive. Filters must keep applying after more partial selections than the
// warmed pool holds, and release everything once the composition ends.
func TestNativeFiltersAcrossSelectedSegments(t *testing.T) {
	if os.Getenv("SHIXIN_CHILD") != "TestNativeFiltersAcrossSelectedSegments" {
		runInChild(t, "TestNativeFiltersAcrossSelectedSegments")
		return
	}
	cl := startNativeFilterEngine(t, "script_translator", "")
	cl.setASCII(false)
	const segments = 10
	for round := range 3 {
		r := cl.typ(strings.Repeat("ni", segments))
		// A sentence covers longer input, so the filters leave those menus
		// alone. Select 你 piecewise: each selected menu keeps its filters'
		// translations alive while the last segment is filtered.
		for i := range segments - 1 {
			if len(r.Candidates) < 2 || string(r.Candidates[1].Text) != "你" {
				t.Fatalf("round %d segment %d: %q", round, i, texts(r))
			}
			r = cl.key('2', 0)
		}
		// Long-word priority keeps 你 泥 拟 and promotes 你好 to 4th.
		if got := texts(r); len(got) < 4 || got[3] != "你好" {
			t.Fatalf("round %d: long-word filter not applied after %d selections: %q", round, segments-1, got)
		}
		r = cl.key(' ', 0)
		if got := string(r.Commit); got != strings.Repeat("你", segments) {
			t.Fatalf("round %d commit=%q", round, got)
		}
		// The pin still applies to a fresh single-segment menu.
		if got := firstText(cl.typ("ni")); got != "泥" {
			t.Fatalf("round %d: pin lost after long composition: %q", round, got)
		}
		cl.key(policy.KeyEscape, 0)
	}
}

// Paging past the filters' fixed reorder window must reach every candidate
// once, in an order that keeps the filtered head and the upstream tail.
func TestNativeFiltersPageBeyondWindow(t *testing.T) {
	if os.Getenv("SHIXIN_CHILD") != "TestNativeFiltersPageBeyondWindow" {
		runInChild(t, "TestNativeFiltersPageBeyondWindow")
		return
	}
	const extra = 150
	var b strings.Builder
	for i := range extra {
		fmt.Fprintf(&b, "%c\tzu\t%d\n", rune(0x4e00+i), 1000-i)
	}
	b.WriteString("祖宗们\tzu\t1\nzoo\tzu\t2000\n")
	cl := startNativeFilterEngine(t, "table_translator", b.String())
	cl.setASCII(false)
	for round := range 3 {
		r := cl.typ("zu")
		seen := map[string]bool{}
		var order []string
		for page := 0; ; page++ {
			if page > 40 {
				t.Fatal("paging never reached the end of the menu")
			}
			added := 0
			for _, c := range r.Candidates {
				if !seen[string(c.Text)] {
					seen[string(c.Text)] = true
					order = append(order, string(c.Text))
					added++
				}
			}
			if added == 0 {
				break
			}
			r = cl.key(policy.KeyNext, 0)
		}
		if len(order) != extra+2 {
			t.Fatalf("round %d: %d distinct candidates, want %d", round, len(order), extra+2)
		}
		// English demotion moves zoo out of first place within the window.
		// 祖宗们 is beyond the window, so it keeps its upstream position.
		if order[0] == "zoo" || order[len(order)-1] != "祖宗们" {
			t.Fatalf("round %d: unexpected order head=%q tail=%q", round, order[0], order[len(order)-1])
		}
		cl.key(policy.KeyEscape, 0)
	}
}

// startNativeFilterEngine deploys the filter test schema in a scratch data
// and user directory with the named translator. extra is appended to the
// tiny dictionary.
func startNativeFilterEngine(t *testing.T, translator, extra string) *client {
	t.Helper()
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
  translators: [` + translator + `]
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
	dict := "---\nname: test\nversion: '1'\nsort: by_weight\n...\n" + "你\tni\t100\n泥\tni\t90\n拟\tni\t80\n倪\tni\t70\n你好\tni\t60\nhello\tHello\t100\nhello\tHEllo\t100\nvan\tva\t100\nā\tva\t90\n" + extra
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
	t.Cleanup(release)
	e, err := Start(Options{LibPath: librimePath(t), SharedDataDir: data, UserDataDir: user, LogDir: root, MinLogLevel: 2, Deploy: true, Warmup: []string{"ni"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Stop() })
	if !e.Native() {
		t.Skip("requires librime-shixin")
	}
	cl := dial(t, e)
	return cl
}
