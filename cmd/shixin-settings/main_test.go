package main

import (
	"errors"
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
	"shixin/internal/ipc"
	"shixin/internal/models"
	"shixin/internal/rerank"
	"shixin/internal/settings"
	shixupdate "shixin/internal/update"
)

var snapshot = flag.String("snapshot", "", "write every page in Chinese and English, light and dark, to this directory")

var testLanguages = []struct {
	name string
	lang i18n.Lang
}{
	{"zh", i18n.Chinese},
	{"en", i18n.English},
}

// TestMain defaults to Chinese: interaction tests click Chinese labels, and the
// language otherwise follows $LANG (English on CI runners).
func TestMain(m *testing.M) {
	i18n.SetLang(i18n.Chinese)
	os.Exit(m.Run())
}

// testApp is an app whose saves and engine applies are recorded.
func testApp(t *testing.T) (*app, *[]settings.Settings, *int) {
	a := newApp(filepath.Join(t.TempDir(), "settings.json"), settings.Default())
	var saves []settings.Settings
	applies := 0
	a.saveFile = func(s settings.Settings) error { saves = append(saves, clone(s)); return nil }
	a.applyEngine = func(done func(error)) { applies++; done(nil) }
	return a, &saves, &applies
}

func TestPagesBuild(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		t.Run(language.name, func(t *testing.T) {
			i18n.SetLang(language.lang)
			a, _, _ := testApp(t)
			tt := ui.NewTester(a.view, 880, 640)
			for _, pg := range pages {
				if err := tt.Click(T("s.page." + pg.id)); err != nil {
					t.Fatalf("%s: %v", pg.id, err)
				}
				if a.page != pg.id {
					t.Fatalf("clicked %s, page is %s", pg.id, a.page)
				}
			}
		})
	}
}

func TestZiranmaSelection(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		t.Run(language.name, func(t *testing.T) {
			i18n.SetLang(language.lang)
			a, saves, applies := testApp(t)
			tt := ui.NewTester(a.view, 880, 640)
			initial, want := "全拼", "自然码双拼"
			if language.lang == i18n.English {
				initial, want = "Full pinyin", "Ziranma Shuangpin"
			}
			if err := tt.Click(initial); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click(want); err != nil {
				t.Fatal(err)
			}
			tt.Advance(time.Second)
			if a.s.Scheme != "double_pinyin" || len(*saves) != 1 || (*saves)[0].Scheme != "double_pinyin" || *applies != 1 || a.status != statusSaved {
				t.Fatalf("scheme %q saves %v applies %d status %v", a.s.Scheme, *saves, *applies, a.status)
			}
		})
	}
}

func TestAppearanceAppliesWithoutBuild(t *testing.T) {
	a, saves, applies := testApp(t)
	a.page = "appearance"
	tt := ui.NewTester(a.view, 880, 640)
	if err := tt.Click("墨"); err != nil {
		t.Fatal(err)
	}
	if a.s.Skin != "ink" || a.status != statusPending || len(*saves) != 0 {
		t.Fatalf("skin %q status %v saves %d", a.s.Skin, a.status, len(*saves))
	}
	tt.Advance(time.Second)
	if len(*saves) != 1 || (*saves)[0].Skin != "ink" || *applies != 0 || a.status != statusSaved {
		t.Fatalf("saves %d applies %d status %v", len(*saves), *applies, a.status)
	}
}

func TestEngineSettingsApply(t *testing.T) {
	a, saves, applies := testApp(t)
	tt := ui.NewTester(a.view, 880, 640)
	if err := tt.Click("繁体输出"); err == nil {
		t.Fatal("input page shown before choosing it")
	}
	tt.Click(T("s.page.input"))
	if err := tt.Click(T("s.i.traditional")); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !a.saved.Traditional || len(*saves) != 1 || *applies != 1 || a.status != statusSaved {
		t.Fatalf("traditional %v saves %d applies %d status %v", a.saved.Traditional, len(*saves), *applies, a.status)
	}
	// A failed save shows the error and keeps the edit.
	a.saveFile = func(settings.Settings) error { return errors.New("disk full") }
	tt.Click(T("s.i.emoji"))
	tt.Advance(time.Second)
	if a.status != statusError || a.saved.Emoji == a.s.Emoji {
		t.Fatalf("status %v", a.status)
	}
}

func TestPhrases(t *testing.T) {
	a, _, _ := testApp(t)
	a.page = "dictionary"
	tt := ui.NewTester(a.view, 880, 640)
	a.newCode, a.newText = "YX ", "shixin@example.com"
	tt.Frame()
	if err := tt.Click(T("s.d.add")); err != nil {
		t.Fatal(err)
	}
	if len(a.s.Phrases) != 1 || a.s.Phrases[0] != (settings.Phrase{Code: "yx", Text: "shixin@example.com"}) {
		t.Fatalf("phrases %+v", a.s.Phrases)
	}
	a.newCode, a.newText = "y1", "x"
	tt.Frame()
	if !tt.HasText(T("s.d.bad_code")) {
		t.Error("invalid code accepted silently")
	}
	if err := tt.Click(T("s.d.delete")); err != nil {
		t.Fatal(err)
	}
	if len(a.s.Phrases) != 0 {
		t.Errorf("phrases %+v", a.s.Phrases)
	}
}

func TestSelectCharFreesPageKeys(t *testing.T) {
	a, _, _ := testApp(t)
	a.page = "input"
	a.s.SelectChar, a.s.PageKeys = "off", []string{"minus_equal", "brackets"}
	tt := ui.NewTester(a.view, 880, 900)
	tt.Frame()
	a.s.SelectChar = "brackets" // as the drop-down would
	tt.Frame()
	if err := a.s.Validate(); err != nil {
		t.Fatalf("%v (page keys %v)", err, a.s.PageKeys)
	}
}

func TestSnapshots(t *testing.T) {
	if *snapshot == "" {
		t.Skip("-snapshot DIR renders every page")
	}
	if err := os.MkdirAll(*snapshot, 0o755); err != nil {
		t.Fatal(err)
	}
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		for _, dark := range []bool{false, true} {
			for _, pg := range pages {
				a, _, _ := testApp(t)
				a.page = pg.id
				if pg.id == "start" {
					a.added = true
				}
				if pg.id == "about" {
					// A released, installed build, whatever this test runs as.
					oldVersion := version
					version, a.front = "0.1.0", "/Applications/Shixin"
					for _, st := range aboutStates() {
						a.upd = st.state
						a.page = pg.id
						writeSnapshot(t, a, language.lang, dark, 900, "about-"+st.name)
					}
					a.upd, version, a.front = updateState{}, oldVersion, ""
				}
				if pg.id == "general" {
					a.s.Scheme = "double_pinyin"
					a.s.Punctuation = "english"
				}
				if pg.id == "shortcuts" {
					a.s.CandidateKeys = "control"
				}
				a.s.Phrases = []settings.Phrase{{Code: "yx", Text: "shixin@example.com"}, {Code: "dz", Text: "北京市海淀区"}}
				a.s.Apps = []settings.AppRule{{ID: "com.apple.Terminal", Name: "Terminal", Mode: "english"}}
				a.s.Fuzzy = []string{"z=zh", "c=ch", "s=sh"}
				a.saved = clone(a.s)
				a.seen = clone(a.s)
				// The model's state is the test's, not this machine's: on,
				// a third downloaded, waiting for it.
				a.model = modelState{dir: "models", active: true, done: models.ERNIE.Size / 3, total: models.ERNIE.Size, aiState: rerank.StateNoModel}
				if pg.id == "rerank" {
					a.s.Rerank = "standard"
					a.saved, a.seen = clone(a.s), clone(a.s)
				}
				if pg.id == "input" {
					a.quickShown = true
					writeSnapshot(t, a, language.lang, dark, 720, "input-quick")
					a.quickShown = false
					// No extra page keys, with [ ] taken by 以词定字: the
					// page must say so, not show four unpicked chips.
					off := clone(a.s)
					off.PageKeys, off.SelectChar = []string{}, "brackets"
					a.s, a.saved, a.seen = off, clone(off), clone(off)
					writeSnapshot(t, a, language.lang, dark, 1400, "input-no-page-keys")
					a.fuzzyShown = true
					writeSnapshot(t, a, language.lang, dark, 720, "input-fuzzy")
					a.fuzzyShown = false
					a.s.PageKeys, a.s.SelectChar = []string{"minus_equal"}, "brackets"
					a.saved, a.seen = clone(a.s), clone(a.s)
				}
				if pg.id == "perf" {
					a.stats = sampleStats()
				}
				writeSnapshot(t, a, language.lang, dark, 720, pg.id)
			}
		}
	}
}

// writeSnapshot renders a's current page to DIR/base[-dark][-en].png.
func writeSnapshot(t *testing.T, a *app, lang i18n.Lang, dark bool, height int, base string) {
	t.Helper()
	tt := ui.NewTester(a.view, 880, height)
	tt.SetDark(dark)
	tt.Frame()
	name := base
	if dark {
		name += "-dark"
	}
	// Preserve existing Chinese filenames for snapshot consumers.
	if lang == i18n.English {
		name += "-en"
	}
	f, err := os.Create(filepath.Join(*snapshot, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(f, tt.Image())
	closeErr := f.Close()
	if encodeErr != nil {
		t.Fatalf("%s: %v", name, encodeErr)
	}
	if closeErr != nil {
		t.Fatalf("%s: %v", name, closeErr)
	}
}

// aboutStates are the 更新 group's states, rendered by the snapshot test.
func aboutStates() []struct {
	name  string
	state updateState
} {
	rel := &shixupdate.Release{Version: "0.2.0", Newer: true, Size: 60 << 20,
		Notes: "- 新增：检查并安装更新\n- 修复：候选窗在多显示器上的位置\n- Faster startup after an upgrade"}
	return []struct {
		name  string
		state updateState
	}{
		{"upd-idle", updateState{}},
		{"upd-checking", updateState{phase: updChecking}},
		{"upd-uptodate", updateState{phase: updUpToDate, rel: &shixupdate.Release{Version: "0.1.0"}}},
		{"upd-available", updateState{phase: updAvailable, rel: rel}},
		{"upd-downloading", updateState{phase: updDownloading, rel: rel, done: 24 << 20, total: rel.Size}},
		{"upd-installing", updateState{phase: updInstalling, rel: rel}},
		{"upd-failed", updateState{phase: updFailed, rel: rel, err: "update: not installing: the app is signed by team OTHER98765, not the installed ABCDE12345"}},
	}
}

func sampleStats() *ipc.Stats {
	st := &ipc.Stats{Keys: 2595, StartupMS: 142, UptimeS: 5400, Librime: []byte("1.17.0"), Native: true, Schemas: []byte("rime_ice")}
	for i := range 2595 {
		us := 30 + (i*37)%90
		if i%50 == 0 {
			us = 300 + i%400
		}
		st.KeyTime.Add(time.Duration(us) * time.Microsecond)
	}
	st.Rerank = &ipc.RerankStats{On: true, Ready: true, Requests: 1480, Applied: 1466, Timeouts: 14, Superseded: 3, Dropped: 14}
	for i := range 1480 {
		st.Rerank.Wait.Add(time.Duration(6000+(i*53)%9000) * time.Microsecond)
	}
	return st
}

// A failed engine apply stays visible: later frames and appearance-only
// edits don't hide it.
func TestApplyErrorSticks(t *testing.T) {
	a, _, _ := testApp(t)
	a.applyEngine = func(done func(error)) { done(errors.New("engine gone")) }
	tt := ui.NewTester(a.view, 880, 640)
	tt.Click(T("s.page.input"))
	tt.Click(T("s.i.traditional"))
	tt.Advance(time.Second)
	tt.Frame()
	tt.Frame()
	if a.status != statusError || a.err == "" {
		t.Fatalf("status %v err %q after frames", a.status, a.err)
	}
}

// Applies never overlap: an edit during one queues exactly one more, and
// "saved" shows only after the last.
func TestAppliesSerialized(t *testing.T) {
	a, _, _ := testApp(t)
	var pending []func(error)
	a.applyEngine = func(done func(error)) { pending = append(pending, done) }
	tt := ui.NewTester(a.view, 880, 640)
	tt.Click(T("s.page.input"))
	tt.Click(T("s.i.traditional"))
	tt.Advance(time.Second)
	tt.Click(T("s.i.emoji"))
	tt.Advance(time.Second)
	tt.Click(T("s.i.mixed"))
	tt.Advance(time.Second)
	if len(pending) != 1 || !a.applying || !a.applyAgain {
		t.Fatalf("%d applies in flight, again %v", len(pending), a.applyAgain)
	}
	pending[0](nil)
	tt.Frame()
	if len(pending) != 2 || a.status != statusApplying {
		t.Fatalf("after the first: %d applies, status %v", len(pending), a.status)
	}
	pending[1](nil)
	tt.Frame()
	if len(pending) != 2 || a.status != statusSaved || a.applying {
		t.Fatalf("after the second: %d applies, status %v", len(pending), a.status)
	}
}

// A failed save is shown and not retried until the next edit.
func TestFailedSaveNotRetried(t *testing.T) {
	a, _, _ := testApp(t)
	tries := 0
	a.saveFile = func(settings.Settings) error { tries++; return errors.New("disk full") }
	a.page = "appearance"
	tt := ui.NewTester(a.view, 880, 640)
	tt.Click("墨")
	tt.Advance(time.Second)
	for range 5 {
		tt.Frame()
	}
	if tries != 1 || a.status != statusError {
		t.Fatalf("%d tries, status %v", tries, a.status)
	}
}

// A settings.json repaired on load is kept as settings.json.bad before the
// first write replaces it.
func TestRepairedFileBackedUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"skin": "neon", "phrases": [{"code": "dz", "text": "家"}]}`), 0o644)
	s, err := settings.Load(path)
	if err == nil || len(s.Phrases) != 1 {
		t.Fatalf("load: %v %+v", err, s.Phrases)
	}
	a := newApp(path, s)
	a.backup = true
	a.applyEngine = func(done func(error)) { done(nil) }
	a.page = "appearance"
	tt := ui.NewTester(a.view, 880, 640)
	tt.Click("墨")
	tt.Advance(time.Second)
	if b, err := os.ReadFile(path + ".bad"); err != nil || !strings.Contains(string(b), "neon") {
		t.Fatalf("backup: %v %s", err, b)
	}
	if got, err := settings.Load(path); err != nil || got.Skin != "ink" || len(got.Phrases) != 1 {
		t.Fatalf("saved: %v %+v", err, got)
	}
}

// TestDateCodes checks the codes the 快捷输入 group shows against rime-ice's
// schemas (needs scripts/fetch-rime-data.sh).
func TestDateCodes(t *testing.T) {
	for _, sc := range settings.Schemes {
		b, err := os.ReadFile(filepath.Join("..", "..", "build", "rime-data", sc.ID+".schema.yaml"))
		if err != nil {
			t.Skip("no rime-ice data")
		}
		block := regexp.MustCompile(`(?m)^date_translator:\n(?:[ #].*\n|\n)*`).Find(b)
		for k, v := range dateCodes(sc.ID) {
			if !regexp.MustCompile(`(?m)^  ` + k + `: ` + v + `\b`).Match(block) {
				t.Errorf("%s: date_translator/%s isn't %q", sc.ID, k, v)
			}
		}
	}
}

func TestFuzzySummary(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	i18n.SetLang(i18n.Chinese)
	s := settings.Default()
	s.Fuzzy = nil
	if got := fuzzySummary(&s); got != "未开启" {
		t.Errorf("none: %q", got)
	}
	s.Fuzzy = []string{"s=sh", "z=zh"} // shown in table order
	if got, want := fuzzySummary(&s), "已开启：z ⇄ zh、s ⇄ sh"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}
