package rime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"shixin/internal/dictionary"
	"shixin/internal/rerank"
)

// Each stage owns only a scratch database, in its own process. A cell
// dictionary larger than the AI export window must not crowd out common hh
// candidates, but its words must remain selectable and learn normally.
func TestCellVocabularyRanking(t *testing.T) {
	if role := os.Getenv("SHIXIN_CELL_TEST_ROLE"); role != "" {
		cellVocabularyProcess(t, os.Getenv("SHIXIN_CELL_TEST_ROOT"), role)
		return
	}
	if !loadForTest(t).HasModule("shixin") {
		t.Skip("requires our librime with librime-shixin")
	}
	root := t.TempDir()
	const schema = `schema: {schema_id: rime_ice, name: Test, version: '1'}
switches: [{name: ascii_mode, reset: 0}]
engine:
  processors: [ascii_composer, speller, selector, navigator, express_editor]
  segmentors: [ascii_segmentor, abc_segmentor, fallback_segmentor]
  translators: [script_translator]
  filters: [shixin_rerank, uniquifier]
speller:
  alphabet: abcdefghijklmnopqrstuvwxyz
  delimiter: " '"
  algebra: ['abbrev/^([a-z]).+$/$1/']
translator: {dictionary: rime_ice, enable_user_dict: true, enable_sentence: false, enable_completion: false}
menu: {page_size: 5}
`
	files := map[string]string{
		"default.yaml":         "config_version: '1'\nschema_list: [{schema: rime_ice}]\n",
		"rime_ice.schema.yaml": schema,
		"rime_ice.dict.yaml":   "---\nname: rime_ice\nversion: '1'\nsort: by_weight\n...\n呵呵\the he\t10000\n哈哈\tha ha\t9000\n很好\then hao\t8000\n红\thong\t100\n黄\thuang\t100\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"import", "menu", "export"} {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCellVocabularyRanking$", "-test.v")
		cmd.Env = append(os.Environ(), "SHIXIN_CELL_TEST_ROLE="+role, "SHIXIN_CELL_TEST_ROOT="+root)
		if v := os.Getenv("SHIXIN_DYLD_INSERT"); v != "" {
			cmd.Env = append(cmd.Env, "DYLD_INSERT_LIBRARIES="+v)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", role, err, out)
		}
	}
}

func cellVocabularyProcess(t *testing.T, root, role string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	l := loadForTest(t)
	l.Setup(Traits{SharedDataDir: root, UserDataDir: root, LogDir: root, MinLogLevel: 3, Modules: []string{"default", "levers", "shixin"}})
	if role != "menu" {
		l.InitializeDeployer()
		defer l.Finalize()
		path := filepath.Join(root, "words.txt")
		if role == "import" {
			var text strings.Builder
			fmt.Fprintln(&text, "洪晃\thong huang\t0")
			for i := range 60 {
				fmt.Fprintf(&text, "%c晃\thong huang\t0\n", rune(0x5000+i))
			}
			if err := os.WriteFile(path, []byte(text.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := l.DictionaryTable(path, true); err != nil {
				t.Fatal(err)
			}
			return
		}
		if _, err := l.DictionaryTable(path, false); err != nil {
			t.Fatal(err)
		}
		b, err := dictionary.ReadFile(path)
		if err != nil || len(b.Words) != 61 {
			t.Fatalf("export: count=%d error=%v", len(b.Words), err)
		}
		for _, e := range b.Words {
			want := 0
			if e.Text == "洪晃" {
				want = 1
			}
			if e.Count != want {
				t.Fatalf("%s count=%d, want %d", e.Text, e.Count, want)
			}
		}
		return
	}
	l.Initialize()
	defer l.Finalize()
	l.StartMaintenance(true)
	l.JoinMaintenanceThread()
	slots, err := rerank.NewSlots(1)
	if err != nil {
		t.Fatal(err)
	}
	defer slots.Close()
	if !l.AttachRerank(slots.Base(), 1, int(rerank.SlotSize)) {
		t.Fatal("attach rerank")
	}
	defer l.AttachRerank(nil, 0, 0)
	s, err := l.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Destroy()
	if !s.SelectSchema("rime_ice") {
		t.Fatal("schema")
	}
	s.SetProperty(rerank.SlotProperty, "0")
	slots.At(0).Enabled = rerank.Enabled
	var c Context
	query := func(code string) {
		s.ClearComposition()
		if !s.SimulateKeySequence(code) || !s.ReadContext(&c) || len(c.Candidates) == 0 {
			t.Fatal("no candidates")
		}
	}
	query("hh")
	if got := string(c.Candidates[0].Text); got != "呵呵" {
		t.Fatalf("import displaced common word: %s", got)
	}
	for i := 0; i < int(slots.At(0).Count); i++ {
		if slots.At(0).Entries[i].Flags&rerank.EntryUser != 0 {
			t.Fatal("unlearned vocabulary exported as personal history")
		}
	}
	query("honghuang")
	found := false
	for page := 0; page < 20; page++ {
		for i, candidate := range c.Candidates {
			if string(candidate.Text) == "洪晃" {
				if !s.SelectCandidateOnCurrentPage(i) {
					t.Fatal("select imported word")
				}
				found = true
				break
			}
		}
		if found || c.IsLastPage {
			break
		}
		s.ProcessKey(0xff56, 0)
		s.ReadContext(&c)
	}
	if !found {
		t.Fatal("imported vocabulary unavailable")
	}
	query("hh")
	if got := string(c.Candidates[0].Text); got != "洪晃" {
		t.Fatalf("selection did not learn word: %s", got)
	}
	if slots.At(0).Entries[0].Flags&rerank.EntryUser == 0 {
		t.Fatal("selected word not exported as learned")
	}
}
