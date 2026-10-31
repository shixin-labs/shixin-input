package engine

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/perf"
	"shixin/internal/policy"
	"shixin/internal/rerank"
	"shixin/internal/settings"
)

// TestReplay is the replay-corpus gate (DESIGN §9): it replays recorded
// typing sessions against rime-ice through the engine and fails if
//   - the engine's per-key time exceeds the budget (§3.7: p50 0.3 ms,
//     p99 2 ms, p99.9 8 ms, scaled by SHIXIN_PERF_TOLERANCE; "off" in
//     CI, whose shared runners can't time them reliably), or
//   - the key policy mispredicts: EAT on a non-printable key librime
//     declines, or PASS on a key librime handles.
//
// The corpus is replayed several times after a warm-up pass. Slow; needs
// scripts/fetch-rime-data.sh; skipped with -short.
func TestReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: deploys rime-ice")
	}
	if os.Getenv("SHIXIN_CHILD") != "TestReplay" {
		runInChild(t, "TestReplay")
		return
	}
	traditional := settings.Default()
	traditional.Traditional = true
	candidates := settings.Default()
	candidates.CandidateKeys = "control"
	punctuation := settings.Default()
	punctuation.Punctuation = "english"
	mixed := settings.Default()
	mixed.Scheme, mixed.FullPinyinFallback = "double_pinyin_flypy", true
	mixedFuzzy := mixed
	for _, pair := range settings.FuzzyPairs {
		mixedFuzzy.Fuzzy = append(mixedFuzzy.Fuzzy, pair.ID)
	}
	for _, v := range []struct {
		name string
		s    settings.Settings
	}{{"default", settings.Default()}, {"traditional", traditional}, {"candidate-shortcuts", candidates}, {"english-punctuation", punctuation}, {"mixed-pinyin", mixed}, {"mixed-pinyin-fuzzy", mixedFuzzy}} {
		t.Run(v.name, func(t *testing.T) { replay(t, v.s, pinyinCorpus, nil) })
	}
	// 智能排序 (DESIGN §7.2) with a scorer that reverses every menu at once:
	// the engine's cost of exporting candidates (50 per 简拼 menu), asking
	// and applying the order, gated against the same corpus without it.
	t.Run("rerank", func(t *testing.T) { replay(t, settings.Default(), pinyinCorpus, &reverser{}) })
	var off, on time.Duration
	t.Run("abbrev", func(t *testing.T) { off = replay(t, settings.Default(), abbrevCorpus, nil) })
	t.Run("abbrev-rerank", func(t *testing.T) { on = replay(t, settings.Default(), abbrevCorpus, &reverser{}) })
	t.Logf("简拼 p99: %v without reranking, %v with", off, on)
	if limit, over := perf.OverBudget(on-off, 500*time.Microsecond); over {
		t.Errorf("reranking adds %v to the 简拼 p99, over %v", on-off, limit)
	}
}

const (
	pinyinCorpus = "testdata/replay/pinyin.txt"
	abbrevCorpus = "testdata/replay/abbrev.txt"
)

// reverser answers every request at once, reversing the menu.
type reverser struct {
	e    *Engine
	perm [rerank.MaxOptions]uint8
}

func (r *reverser) Context(int, uint32, []byte) {}
func (r *reverser) Ready() bool                 { return true }
func (r *reverser) Score(req *rerank.Request) {
	n := int(req.Export.Count)
	for k := range n {
		r.perm[k] = uint8(n - 1 - k)
	}
	r.e.Rerank(req.Session, req.ExportSeq, r.perm[:n])
}

// replay runs a corpus through rime-ice and returns the p99 per key.
func replay(t *testing.T, s settings.Settings, corpus string, scorer *reverser) time.Duration {
	e := rimeIceEngine(t, s, func(o *Options) {
		if scorer != nil {
			o.Scorer, o.Rerank = scorer, true
		}
	})
	if scorer != nil {
		scorer.e = e
	}
	defer e.Stop()
	sessions := loadCorpus(t, corpus)
	cl := dial(t, e)

	var mispredicts []string
	run := func(check bool) {
		for _, s := range sessions {
			for _, k := range s.keys {
				d := cl.policy().Lookup(k.keysym, k.mods).Disposition()
				r := cl.key(k.keysym, k.mods)
				handled := r.Flags&ipc.ResultHandled != 0
				printable := k.keysym >= 0x20 && k.keysym < 0x7f
				if check && (d == policy.Eat && !handled && !printable || d == policy.Pass && handled) {
					mispredicts = append(mispredicts, s.line+": "+k.name+" "+d.String())
				}
			}
			cl.key(policy.KeyEscape, 0)
			// Leave ASCII mode if a session ended there.
			if cl.res.Flags&ipc.ResultASCII != 0 {
				cl.key(policy.KeyShiftL, 0)
				cl.key(policy.KeyShiftL, policy.ReleaseMask)
			}
		}
	}
	run(false) // warm-up
	// The first pass runs in a fresh session from the pool, which must be
	// warm already (Engine.warmSession).
	cold := e.TakeStats()
	t.Logf("first pass (fresh session): %v", &cold.KeyTime)
	if limit, over := perf.OverBudget(cold.KeyTime.Max(), 8*time.Millisecond); over {
		t.Errorf("first pass: slowest key %v, over %v", cold.KeyTime.Max(), limit)
	}
	rounds := 5
	for i := 0; i < rounds; i++ {
		run(i == 0)
	}
	st := e.TakeStats()
	h := &st.KeyTime
	t.Logf("engine per key over %d keys: %v, p99.9 %v", h.Count(), h, h.Quantile(.999).Round(time.Microsecond))
	for _, m := range mispredicts {
		t.Errorf("mispredict: %s", m)
	}

	for _, b := range []struct {
		q      float64
		budget time.Duration
	}{{.5, 300 * time.Microsecond}, {.99, 2 * time.Millisecond}, {.999, 8 * time.Millisecond}} {
		got := h.Quantile(b.q)
		if limit, over := perf.OverBudget(got, b.budget); over {
			t.Errorf("p%g = %v, over budget %v", b.q*100, got, limit)
		}
	}
	if scorer != nil && st.Rerank.Requests == 0 {
		t.Error("no candidates were exported")
	}
	return h.Quantile(.99)
}

type corpusKey struct {
	keysym, mods uint32
	name         string
}

type session struct {
	line string
	keys []corpusKey
}

func loadCorpus(t *testing.T, path string) []session {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []session
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s := session{line: line}
		for i := 0; i < len(line); i++ {
			c := line[i]
			if c != '{' {
				var m uint32
				if shifted(c) {
					m = policy.ShiftMask
				}
				s.keys = append(s.keys, corpusKey{uint32(c), m, string(c)})
				continue
			}
			j := strings.IndexByte(line[i:], '}')
			if j < 0 {
				t.Fatalf("%s: unterminated {", line)
			}
			name := line[i+1 : i+j]
			i += j
			keysym, mods, ok := parseKey(name)
			if !ok {
				t.Fatalf("%s: unknown key %q", line, name)
			}
			if keysym == policy.KeyShiftL || keysym == policy.KeyShiftR { // a tap
				s.keys = append(s.keys, corpusKey{keysym, 0, name}, corpusKey{keysym, policy.ReleaseMask, name + " up"})
				continue
			}
			s.keys = append(s.keys, corpusKey{keysym, mods, name})
		}
		out = append(out, s)
	}
	return out
}

// runInChild runs test name in a child process: librime is a process-wide
// singleton and the other tests share one engine.
func runInChild(t *testing.T, name string) {
	if _, err := os.Stat("../../build/rime-data/rime_ice.schema.yaml"); err != nil {
		t.Skip("rime-ice data missing; run scripts/fetch-rime-data.sh")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$", "-test.v")
	cmd.Env = append(os.Environ(), "SHIXIN_CHILD="+name)
	// dyld drops DYLD_* from the environment it hands on; sanitizer runs
	// (scripts/ci.sh) pass the runtime through SHIXIN_DYLD_INSERT.
	if v := os.Getenv("SHIXIN_DYLD_INSERT"); v != "" {
		cmd.Env = append(cmd.Env, "DYLD_INSERT_LIBRARIES="+v)
	}
	out, err := cmd.CombinedOutput()
	t.Logf("child:\n%s", out)
	if err != nil {
		t.Fatal(err)
	}
}

// rimeIceEngine starts an engine on the real rime-ice data with the
// settings compiler's patches, in a scratch user dir.
func rimeIceEngine(t *testing.T, s settings.Settings, opts ...func(*Options)) *Engine {
	data, _ := filepath.Abs("../../build/rime-data")
	lib := librimePath(t)
	dir, err := os.MkdirTemp("/tmp", "shixin-rimeice-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	user := filepath.Join(dir, "user")
	release, err := Lock(user)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	if _, err := settings.Apply(user, s); err != nil {
		t.Fatal(err)
	}
	// OpenCC's data (简繁) comes with our librime build; librime also
	// looks for it in the user dir.
	if src := filepath.Join(filepath.Dir(lib), "..", "share", "opencc"); fileExists(filepath.Join(src, "s2t.json")) {
		if err := os.CopyFS(filepath.Join(user, "opencc"), os.DirFS(src)); err != nil {
			t.Fatal(err)
		}
	}
	opt := Options{
		LibPath: lib, SharedDataDir: data, UserDataDir: user, LogDir: dir,
		MinLogLevel: 2, Version: "test", Deploy: true, Warmup: DefaultWarmup,
	}
	for _, f := range opts {
		f(&opt)
	}
	e, err := Start(opt)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
