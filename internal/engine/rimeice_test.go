package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"shixin/internal/ipc"
	"shixin/internal/perf"
	"shixin/internal/policy"
	"shixin/internal/settings"
)

// TestRimeIce deploys the real rime-ice data with the settings compiler's
// patches into a scratch user dir and types through it. It is the
// end-to-end no-Lua gate: Start fails if any deployed schema lists a lua_*
// component. Needs scripts/fetch-rime-data.sh; skipped with -short.
func TestRimeIce(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: deploys rime-ice")
	}
	if os.Getenv("SHIXIN_CHILD") != "TestRimeIce" {
		runInChild(t, "TestRimeIce")
		return
	}
	for _, tc := range []struct{ scheme, hello, fuzzyInitial, fuzzyFinal string }{
		{"rime_ice", "nihao", "zongguo", "nangfang"},
		{"double_pinyin", "nihk", "zsgo", "nhfh"},
		{"double_pinyin_flypy", "nihc", "zsgo", "nhfh"},
		{"double_pinyin_mspy", "nihk", "zsgo", "nhfh"},
		{"double_pinyin_sogou", "nihk", "zsgo", "nhfh"},
		{"double_pinyin_abc", "nihk", "zsgo", "nhfh"},
		{"double_pinyin_ziguang", "nihq", "zhgo", "nsfs"},
		{"double_pinyin_jiajia", "nihd", "zygo", "ngfg"},
	} {
		t.Run(tc.scheme, func(t *testing.T) {
			scheme := tc.scheme
			s := settings.Default()
			s.Scheme = scheme
			s.CandidateKeys = "control"
			var fuzzyRules []string
			for _, pair := range settings.FuzzyPairs {
				s.Fuzzy = append(s.Fuzzy, pair.ID)
				fuzzyRules = append(fuzzyRules, pair.Rules...)
			}
			t0 := time.Now()
			e := rimeIceEngine(t, s)
			defer e.Stop()
			t.Logf("deploy + warm-up: %v", time.Since(t0).Round(time.Millisecond))
			user := e.opt.UserDataDir

			for _, f := range []string{scheme, "melt_eng", "radical_pinyin"} {
				b, err := os.ReadFile(filepath.Join(user, "build", f+".schema.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(b), "lua_") {
					t.Errorf("deployed %s.schema.yaml mentions lua_", f)
				}
				if f == scheme && scheme != "rime_ice" {
					var deployed struct {
						Speller struct{ Algebra []string }
					}
					if err := yaml.Unmarshal(b, &deployed); err != nil {
						t.Fatal(err)
					}
					if len(deployed.Speller.Algebra) <= len(fuzzyRules) {
						t.Fatal("double-pinyin conversion rules lost during patching")
					}
					for i, rule := range fuzzyRules {
						if got := deployed.Speller.Algebra[i]; got != rule {
							t.Fatalf("algebra[%d] = %q, want fuzzy rule %q before conversion", i, got, rule)
						}
					}
				}
			}

			cl := dial(t, e)
			checkPunctuation(t, cl, false)
			checkForgetWord(t, cl, s.Hotkeys["forget"], shiCode(scheme))
			input, want := tc.hello, "你好"
			r := cl.typ(input)
			if len(r.Candidates) == 0 || string(r.Candidates[0].Text) != want {
				t.Fatalf("%s: first candidate %q", input, firstText(r))
			}
			if c := r.Candidates[0].Comment; len(c) != 0 { // no spelling hints
				t.Errorf("%s: comment %q", want, c)
			}
			if r = cl.key(' ', 0); string(r.Commit) != want {
				t.Fatalf("commit %q", r.Commit)
			}
			// Modified shortcuts preserve apostrophe delimiters and the
			// semicolon used by some double-pinyin layouts for ing.
			if r := cl.typ(tc.hello[:2] + "'" + tc.hello[2:]); !hasCandidate(r, "你好") {
				t.Error("candidate shortcuts broke syllable delimiters")
			}
			cl.key(policy.KeyEscape, 0)
			if scheme == "double_pinyin_mspy" || scheme == "double_pinyin_sogou" || scheme == "double_pinyin_ziguang" {
				if r := cl.typ("j;"); len(r.Candidates) == 0 || len(r.Commit) != 0 {
					t.Error("candidate shortcuts broke the ing key")
				}
				cl.key(policy.KeyEscape, 0)
			}
			checkCandidateShortcuts(t, cl, s.CandidateKeys, shiCode(scheme), e.Native())
			for _, fuzzy := range []struct{ input, want string }{
				{tc.fuzzyInitial, "中国"},
				{tc.fuzzyFinal, "南方"},
			} {
				r = cl.typ(fuzzy.input)
				if !hasCandidate(r, fuzzy.want) {
					t.Errorf("fuzzy %s: %s not among candidates", fuzzy.input, fuzzy.want)
				}
				cl.key(policy.KeyEscape, 0)
			}
			if e.Native() {
				checkTranslators(t, cl, scheme)
			}
		})
	}
}

// checkTranslators types what rime-ice's Lua translators handled, now
// librime-shixin's (DESIGN §3.8), with rime-ice's own trigger codes: its
// double pinyin schemas use words for the dates (two letters are a
// syllable there).
func checkTranslators(t *testing.T, cl *client, scheme string) {
	t.Helper()
	date, time, week, ts, datezh := "rq", "sj", "xq", "ts", "rqzh"
	if scheme != "rime_ice" {
		date, time, week, ts, datezh = "date", "time", "week", "timestamp", "datezh"
		if r := cl.typ("sj"); regexp.MustCompile(`^\d`).MatchString(firstText(r)) {
			t.Errorf("%s: sj (a syllable) shows the time %q", scheme, firstText(r))
		}
		cl.key(policy.KeyEscape, 0)
	}
	for _, c := range []struct{ input, want string }{
		{date, `^\d{4}-\d{2}-\d{2}$`},
		{time, `^\d{2}:\d{2}$`},
		{week, `^星期[一二三四五六日]$`},
		{ts, `^\d{10}$`},
		{datezh, `^二〇[〇一二三四五六七八九]{2}年`},
		{"R123.45", `^壹佰贰拾叁点肆伍$`},
		{"U62fe", `^拾$`},
		{"cC1+2*3", `^7$`},
		{"cC(2+3)^2/10", `^2\.5$`},
		{"cCsqrt(2)", `^1\.4142135623731$`},
		{"cC100000000000+0.5", `^100000000000\.5$`},
		{"cCpi", `^3\.14159265358979$`}, // all letters: also an abc segment
		{"Ubeef", `^뻯$`},
	} {
		r := cl.typ(c.input)
		if !regexp.MustCompile(c.want).MatchString(firstText(r)) {
			t.Errorf("%s: first candidate %q, want /%s/", c.input, firstText(r), c.want)
		}
		cl.key(policy.KeyEscape, 0)
	}
	// Emoji must not interleave native readings. Do not restrict the
	// filter to abc: recognized symbols and other input still use it.
	weekdayEnglish := regexp.MustCompile(`^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)`)
	for _, code := range []string{"R1", "R10", week, "U31", "cC1+0"} {
		r := cl.typ(code)
		for _, c := range r.Candidates {
			text := string(c.Text)
			if strings.ContainsAny(text, "🔟\u20e3") || weekdayEnglish.MatchString(text) {
				t.Errorf("%s: native reading converted by emoji filter: %q", code, text)
			}
		}
		cl.key(policy.KeyEscape, 0)
	}
	cl.typ("R1001")
	if r := cl.key(' ', 0); string(r.Commit) != "壹仟零壹" {
		t.Errorf("R1001 + space: commit %q", r.Commit)
	}
	cl.typ("cC6*7")
	if r := cl.key(policy.KeyDown, 0); r.Highlighted != 1 {
		t.Errorf("cC6*7: Down highlights %d", r.Highlighted)
	}
	if r := cl.key(' ', 0); string(r.Commit) != "6*7=42" {
		t.Errorf("cC6*7, second candidate: commit %q", r.Commit)
	}
}

// TestRimeIceOptions deploys rime-ice with most settings away from their
// defaults and checks librime does what each one promises.
func TestRimeIceOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: deploys rime-ice")
	}
	if os.Getenv("SHIXIN_CHILD") != "TestRimeIceOptions" {
		runInChild(t, "TestRimeIceOptions")
		return
	}
	s := settings.Default()
	s.InitialMode = "english"
	s.Punctuation = "english"
	s.Traditional = true
	s.Fuzzy = []string{"z=zh"}
	s.MixedEnglish = false
	s.SelectChar = "minus_equal"
	s.PageKeys = []string{"comma_period"}
	s.Hotkeys["traditional"] = "Control+Shift+4"
	s.Hotkeys["forget"] = "Control+Delete"
	s.CandidateKeys = "alt"
	s.LongWordPriority, s.DemoteEnglish, s.VSymbolsFirst, s.EnglishAutocap = true, true, true, true
	s.Pins = []settings.Pin{{Scheme: "rime_ice", Code: "nihao", Text: "你好"}}
	s.Phrases = []settings.Phrase{{Code: "yx", Text: "shixin@example.com"}, {Code: "em", Text: "笑脸"}}
	e := rimeIceEngine(t, s)
	defer e.Stop()
	deployed, err := os.ReadFile(filepath.Join(e.opt.UserDataDir, "build", s.Scheme+".schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Filters struct {
			LongWord      bool     `yaml:"long_word"`
			DemoteEnglish bool     `yaml:"demote_english"`
			VSymbols      bool     `yaml:"v_symbols"`
			Autocap       bool     `yaml:"autocap"`
			Pins          []string `yaml:"pins"`
		} `yaml:"shixin_filters"`
		Emoji struct {
			ExcludedTypes []string `yaml:"excluded_types"`
		}
	}
	if err := yaml.Unmarshal(deployed, &config); err != nil {
		t.Fatal(err)
	}
	if strings.Join(config.Emoji.ExcludedTypes, ",") != "date,number,unicode,calc" {
		t.Fatalf("native translator emoji exclusions lost in deploy: %v", config.Emoji.ExcludedTypes)
	}
	if f := config.Filters; !f.LongWord || !f.DemoteEnglish || !f.VSymbols || !f.Autocap || len(f.Pins) != 1 || f.Pins[0] != "nihao\t你好" {
		t.Fatal("native filter settings lost during deploy")
	}
	cl := dial(t, e)
	if cl.res.Flags&ipc.ResultASCIIPunct == 0 {
		t.Fatal("new session did not start with English punctuation")
	}

	// The first composition of a fresh session must not load the OpenCC
	// filters' dictionaries on the key path (pool sessions are warmed).
	e.TakeStats()
	cl.typ("ceshi")
	cl.key(policy.KeyEscape, 0)
	st := e.TakeStats()
	if _, over := perf.OverBudget(st.KeyTime.Max(), 8*time.Millisecond); over {
		t.Errorf("first composition in a fresh session: slowest key %v", st.KeyTime.Max())
	}
	cl.setASCII(true)

	if r := cl.key('a', 0); r.Flags&ipc.ResultASCII == 0 || r.Flags&ipc.ResultHandled != 0 {
		t.Fatalf("initial mode english: flags %#x", r.Flags)
	}
	cl.seq++
	cl.send((&ipc.SetOption{Ctx: 1, Seq: cl.seq, Name: []byte("ascii_mode")}).Append(cl.buf[:0]))
	if r := cl.result(); r.Flags&ipc.ResultASCII != 0 {
		t.Fatalf("still ASCII after SetOption: %#x", r.Flags)
	}
	checkPunctuation(t, cl, true)
	cl.key('.', policy.ControlMask) // a session toggle doesn't alter the default
	fresh := dial(t, e)
	if fresh.res.Flags&ipc.ResultASCIIPunct == 0 {
		t.Fatal("a new session inherited another session's punctuation toggle")
	}
	fresh.setASCII(false)
	checkPunctuation(t, fresh, true)
	cl.key('.', policy.ControlMask)
	// Bindings that need a composition don't make idle keys wait.
	if a := cl.policy().Lookup(',', 0); a.Disposition() == policy.Ask {
		t.Error("idle ',' is ASK although its binding needs a menu")
	}
	checkCandidateShortcuts(t, cl, s.CandidateKeys, "shi", e.Native())
	if r := cl.typ("em"); !hasCandidate(r, "😀") {
		t.Error("emoji conversion lost for custom phrases")
	}
	cl.key(policy.KeyEscape, 0)

	if r := cl.typ("yx"); firstText(r) != "shixin@example.com" {
		t.Errorf("custom phrase: first candidate %q", firstText(r))
	}
	if r := cl.key(policy.KeyDelete, policy.ControlMask); len(r.Commit) != 0 || !hasCandidate(r, "shixin@example.com") {
		t.Error("forget altered a custom phrase or committed text")
	}
	cl.key(policy.KeyEscape, 0)
	traditional := fileExists(filepath.Join(e.opt.UserDataDir, "opencc", "s2t.json"))
	if r := cl.typ("zhongguo"); traditional && firstText(r) != "中國" {
		t.Errorf("traditional: first candidate %q", firstText(r))
	}
	if r := cl.key('.', 0); r.PageNo != 1 {
		t.Errorf("'.' doesn't page down: page %d", r.PageNo)
	}
	if r := cl.key(',', 0); r.PageNo != 0 {
		t.Errorf("',' doesn't page up: page %d", r.PageNo)
	}
	cl.key(policy.KeyEscape, 0)
	if r := cl.typ("zongguo"); !hasCandidate(r, "中国") && !hasCandidate(r, "中國") {
		t.Error("fuzzy z=zh did not find 中国/中國 with non-default options")
	}
	cl.key(policy.KeyEscape, 0)
	if r := cl.typ("hello"); hasCandidate(r, "hello") || hasCandidate(r, "Hello") {
		t.Error("English candidates without mixed English input")
	}
	cl.key(policy.KeyEscape, 0)
	if e.Native() && traditional {
		cl.typ("shiyan")
		if r := cl.key('-', 0); string(r.Commit) != "實" {
			t.Errorf("以词定字 on '-': commit %q", r.Commit)
		}
		// '.' pages, but the recognizer takes it first in a number. 简繁
		// applies to numbers (rime-ice's traditionalize/tags).
		if r := cl.typ("R1.5"); firstText(r) != "壹點伍" {
			t.Errorf("R1.5 with '.' paging: first candidate %q", firstText(r))
		}
		cl.key(policy.KeyEscape, 0)
		// The calculator still gets '-' (with 12, a two-character
		// candidate, 以词定字 would take it).
		if r := cl.typ("cC12-3"); firstText(r) != "9" {
			t.Errorf("cC12-3 with 以词定字 on '-': first candidate %q", firstText(r))
		}
		cl.key(policy.KeyEscape, 0)
	}
	checkForgetWord(t, cl, s.Hotkeys["forget"], "shi")
	r := cl.key('4', policy.ControlMask|policy.ShiftMask)
	if r.Flags&ipc.ResultHandled == 0 || r.Flags&ipc.ResultTraditional != 0 {
		t.Errorf("Control+Shift+4: flags %#x", r.Flags)
	}
}

func firstText(r *ipc.Result) string {
	if len(r.Candidates) == 0 {
		return ""
	}
	return string(r.Candidates[0].Text)
}

func hasCandidate(r *ipc.Result, s string) bool {
	for _, c := range r.Candidates {
		if string(c.Text) == s {
			return true
		}
	}
	return false
}
