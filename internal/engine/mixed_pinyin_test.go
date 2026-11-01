package engine

import (
	"os"
	"testing"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/policy"
	"shixin/internal/settings"
)

var mixedLayouts = []struct{ scheme, hello, country, zhun string }{
	{"double_pinyin", "nihk", "vsgo", "vp"},
	{"double_pinyin_flypy", "nihc", "vsgo", "vy"},
	{"double_pinyin_mspy", "nihk", "vsgo", "vp"},
	{"double_pinyin_sogou", "nihk", "vsgo", "vp"},
	{"double_pinyin_abc", "nihk", "asgo", "an"},
	{"double_pinyin_ziguang", "nihq", "uhgo", "um"},
	{"double_pinyin_jiajia", "nihd", "vygo", "vz"},
}

func TestRimeIceMixedPinyin(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: deploys rime-ice")
	}
	if os.Getenv("SHIXIN_CHILD") != "TestRimeIceMixedPinyin" {
		runInChild(t, "TestRimeIceMixedPinyin")
		return
	}
	for _, tc := range mixedLayouts {
		t.Run(tc.scheme, func(t *testing.T) {
			s := settings.Default()
			s.Scheme, s.FullPinyinFallback = tc.scheme, true
			s.Fuzzy = []string{"z=zh", "an=ang"}
			s.Phrases = []settings.Phrase{{Code: "yx", Text: "shixin@example.com"}}
			e := rimeIceEngine(t, s)
			defer e.Stop()
			cl := dial(t, e)
			for _, check := range []struct{ input, word string }{
				{"nihao", "你好"}, {tc.hello, "你好"}, {"ni'hao", "你好"},
				{"zhongguo", "中国"}, {tc.country, "中国"}, {"zhonggo", "中国"}, {tc.country[:2] + "guo", "中国"},
				{"zongguo", "中国"}, {"nangfang", "南方"},
			} {
				r := cl.typ(check.input)
				if !hasCandidate(r, check.word) {
					t.Fatalf("%s doesn't find %s", check.input, check.word)
				}
				if firstText(r) != check.word {
					t.Errorf("%s: first %q, want %q", check.input, firstText(r), check.word)
				}
				if got := string(cl.key(' ', 0).Commit); got != check.word {
					t.Errorf("%s: commit %q, want %q", check.input, got, check.word)
				}
			}
			if r := cl.typ("yx"); firstText(r) != "shixin@example.com" {
				t.Error("mixed spelling broke a custom phrase")
			}
			cl.key(policy.KeyEscape, 0)
			if r := cl.typ("hello"); !hasCandidate(r, "hello") && !hasCandidate(r, "Hello") {
				t.Error("mixed English word missing")
			}
			cl.key(policy.KeyEscape, 0)
			if e.Native() {
				checkTranslators(t, cl, tc.scheme)
			}
			mixedPinyinAllocs(t, cl, []string{"nihao", tc.hello, "zhongguo", tc.country})
		})
	}
}

func TestMixedPinyinSpellings(t *testing.T) {
	if testing.Short() {
		t.Skip("needs rime-ice spelling rules")
	}
	if os.Getenv("SHIXIN_CHILD") != "TestMixedPinyinSpellings" {
		runInChild(t, "TestMixedPinyinSpellings")
		return
	}
	const dictionary = "---\nname: test\nversion: '1'\nsort: by_weight\n...\n你好\tni hao\t100\n中国\tzhong guo\t100\n南方\tnan fang\t100\n虐\tnve\t100\n略\tlve\t100\n女\tnv\t100\n去\tqu\t100\n啊\ta\t100\n爱\tai\t100\n安\tan\t50\n准\tzhun\t100\n保留\txx\t100\n"
	for _, tc := range mixedLayouts {
		for _, fuzzy := range []bool{false, true} {
			name := tc.scheme
			if fuzzy {
				name += "/fuzzy"
			}
			t.Run(name, func(t *testing.T) {
				s := settings.Default()
				s.Scheme, s.FullPinyinFallback = tc.scheme, true
				if fuzzy {
					for _, pair := range settings.FuzzyPairs {
						s.Fuzzy = append(s.Fuzzy, pair.ID)
					}
				}
				cl, _ := spellingEngine(t, s, dictionary)
				for _, check := range []struct{ input, word string }{
					{"nihao", "你好"}, {tc.hello, "你好"}, {"ni'hao", "你好"},
					{"zhongguo", "中国"}, {tc.country, "中国"}, {"zhonggo", "中国"}, {tc.country[:2] + "guo", "中国"},
					{"nve", "虐"}, {"nue", "虐"}, {"lve", "略"}, {"lue", "略"}, {"nv", "女"}, {"qu", "去"}, {"qv", "去"},
					{"a", "啊"}, {"ai", "爱"}, {"an", "安"}, {tc.zhun, "准"},
				} {
					if r := cl.typ(check.input); !hasCandidate(r, check.word) {
						t.Errorf("%s doesn't find %s", check.input, check.word)
					}
					cl.key(policy.KeyEscape, 0)
				}
				if r := cl.typ("xx"); hasCandidate(r, "保留") {
					t.Error("full spelling revived reserved xx")
				}
				cl.key(policy.KeyEscape, 0)
				if fuzzy {
					for _, check := range []struct{ input, word string }{{"zongguo", "中国"}, {"nangfang", "南方"}} {
						if r := cl.typ(check.input); !hasCandidate(r, check.word) {
							t.Errorf("fuzzy %s doesn't find %s", check.input, check.word)
						}
						cl.key(policy.KeyEscape, 0)
					}
				}
				// ABC's an is both a double-pinyin code for zhun and full an.
				// Both readings must remain selectable, regardless of ranking.
				if tc.scheme == "double_pinyin_abc" {
					r := cl.typ("an")
					if !hasCandidate(r, "安") || !hasCandidate(r, "准") {
						t.Error("ambiguous spelling lost a reading")
					}
					cl.key(policy.KeyEscape, 0)
				}
				for _, k := range []uint32{'n', 'z', 'v'} {
					if d := cl.policy().Lookup(k, 0).Disposition(); d != policy.Eat {
						t.Errorf("idle letter %c: %v", k, d)
					}
				}
				if !raceEnabled {
					mixedPinyinAllocs(t, cl, []string{"nihao", tc.hello, "zhongguo", tc.country, tc.country[:2] + "guo"})
				}
				cl.setASCII(true)
				if r := cl.typ("nihao"); len(r.Preedit) != 0 || len(r.Commit) != 0 || r.Flags&ipc.ResultHandled != 0 {
					t.Error("English mode consumed full pinyin")
				}
			})
		}
	}
}

func mixedPinyinAllocs(t *testing.T, cl *client, inputs []string) {
	t.Helper()
	if raceEnabled {
		return
	}
	cycle := func() {
		// Each allocation cycle is bounded; the entire repeated batch can
		// exceed the last functional result's deadline on Windows pipes.
		cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
		for _, input := range inputs {
			for i := 0; i < len(input)+2; i++ {
				k := uint32(policy.KeyEscape)
				if i < len(input) {
					k = uint32(input[i])
				} else if i == len(input) {
					k = ' '
				}
				cl.seq++
				cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: k}).Append(cl.buf[:0]))
				for {
					typ, payload, err := cl.r.Next()
					if err != nil {
						t.Fatal(err)
					}
					if typ == ipc.TypeResult {
						if err := cl.res.Decode(payload); err != nil {
							t.Fatal(err)
						}
						break
					}
				}
			}
		}
	}
	for range 3 {
		cycle()
	}
	if n := testing.AllocsPerRun(20, cycle); n != 0 {
		t.Fatalf("mixed spelling cycles: %v Go allocations", n)
	}
}
