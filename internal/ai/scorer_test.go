package ai

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"shixin/internal/llama"
	"shixin/internal/perf"
	"shixin/internal/rerank"
)

var shared *Scorer

// raceEnabled is set by race_test.go in -race builds.
var raceEnabled bool

func TestMain(m *testing.M) {
	code := m.Run()
	// Free llama.cpp's Metal resources before exit: -race builds exit
	// through libc, whose destructors abort on a live device.
	if shared != nil {
		shared.Close()
	}
	os.Exit(code)
}

// testScorer loads build/llama/lib and the 8-bit ERNIE model
// ($SHIXIN_LLAMA, $SHIXIN_MODEL override), or skips.
func testScorer(t testing.TB) *Scorer {
	if shared != nil {
		return shared
	}
	dir := os.Getenv("SHIXIN_LLAMA")
	if dir == "" {
		dir, _ = filepath.Abs("../../build/llama/lib")
	}
	model := os.Getenv("SHIXIN_MODEL")
	if model == "" {
		model, _ = filepath.Abs("../../build/models/ernie-4.5-0.3b-base-q8_0.gguf")
	}
	if _, err := os.Stat(model); err != nil {
		if os.Getenv("SHIXIN_TEST_REQUIRE_ACCELERATOR") == "1" {
			t.Fatal(err)
		}
		t.Skip("model missing; see DESIGN §7.2")
	}
	if _, err := os.Stat(dir); err != nil {
		if os.Getenv("SHIXIN_LLAMA") != "" || os.Getenv("SHIXIN_TEST_REQUIRE_ACCELERATOR") == "1" {
			t.Fatal(err)
		}
		t.Skip("llama.cpp not built; run scripts/build-llama.sh")
	}
	lib, err := llama.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(lib, model)
	if err == ErrNoAccelerator {
		if os.Getenv("SHIXIN_TEST_REQUIRE_ACCELERATOR") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	shared = s
	return s
}

func cands(words ...string) []Candidate {
	var c []Candidate
	for _, w := range words {
		c = append(c, Candidate{Text: []byte(w), Weight: -10})
	}
	return c
}

func TestScoreFollowsContext(t *testing.T) {
	s := testScorer(t)
	perm := make([]uint8, rerank.MaxOptions)
	p := Strengths[rerank.StrengthStandard]
	for _, c := range []struct {
		context string
		options []string
		want    string
	}{
		{"我们明天一起去", []string{"太晚", "台湾", "抬碗"}, "台湾"},
		{"今天的天气预报说下午有", []string{"阵雨", "真鱼", "镇域"}, "阵雨"},
		{"", []string{"你好", "拟好", "泥嚎"}, "你好"},
	} {
		if err := s.Prefill(1, 1, []byte(c.context)); err != nil {
			t.Fatal(err)
		}
		got, err := s.Score(1, cands(c.options...), p, perm)
		if err != nil {
			t.Fatal(err)
		}
		if first := c.options[got[0]]; first != c.want {
			t.Errorf("%q: #1 %q, want %q (order %v)", c.context, first, c.want, got)
		}
	}
}

// This public synthetic fixture keeps the latency gate runnable without the
// external spike corpus. This one heavier menu has p95 budgets; its repeated
// median is not the 300-item corpus's p50<=8ms gate, which remains separate.
func TestScoringLatency(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("latency gate: run alone, without -short or -race")
	}
	s := testScorer(t)
	context := []byte("光大银行黄鹤楼支行的理财经理测算，结婚生子后的家庭开支会明显增加，所以我们建议年轻人尽早规划自己的财务，比如每个月固定存下一部分收入")
	options := cands("太晚", "台湾", "抬碗", "体温", "天文", "同为", "图文", "条文", "庭外", "听闻", "体外", "调味", "突围", "探望", "贪玩", "退伍", "提问", "童玩", "图网", "糖丸")
	perm := make([]uint8, rerank.MaxOptions)
	var prefill, score []time.Duration
	for i := range 35 {
		// A new app evicts a slot and forces a fresh 64-token prefill.
		app := 10000 + i
		t0 := time.Now()
		if err := s.Prefill(app, 1, context); err != nil {
			t.Fatal(err)
		}
		t1 := time.Now()
		if _, err := s.Score(app, options, Strengths[rerank.StrengthStandard], perm); err != nil {
			t.Fatal(err)
		}
		if i >= 5 {
			prefill = append(prefill, t1.Sub(t0))
			score = append(score, time.Since(t1))
		}
	}
	slices.Sort(prefill)
	slices.Sort(score)
	t.Logf("fixed-menu score p50: %v", score[len(score)/2])
	for _, g := range []struct {
		name        string
		got, budget time.Duration
	}{
		{"prefill p95", prefill[len(prefill)*95/100], 20 * time.Millisecond},
		{"score p95", score[len(score)*95/100], 15 * time.Millisecond},
	} {
		t.Logf("%s: %v", g.name, g.got)
		if limit, over := perf.OverBudget(g.got, g.budget); over {
			t.Errorf("%s %v, over budget %v", g.name, g.got, limit)
		}
	}
}

// TestLearnedWordPrior: a learned word's user-dictionary weight doesn't
// count against it; it is scored as if it had the leading word's.
func TestLearnedWordPrior(t *testing.T) {
	s := testScorer(t)
	if err := s.Prefill(4, 1, []byte("我们明天一起去")); err != nil {
		t.Fatal(err)
	}
	c := cands("太晚", "台湾")
	c[0].Weight, c[1].Weight, c[1].Flags = -9, -40, rerank.EntryUser
	if _, err := s.score(4, c, len(c)); err != nil {
		t.Fatal(err)
	}
	if s.opts[1].Prior != s.opts[0].Prior || !s.opts[1].User {
		t.Errorf("learned prior %v, leading %v", s.opts[1].Prior, s.opts[0].Prior)
	}
}

func TestPrefillReusesContext(t *testing.T) {
	s := testScorer(t)
	if err := s.Prefill(2, 1, []byte("我们明天一起去")); err != nil {
		t.Fatal(err)
	}
	n := len(s.apps[s.find(2)].tokens)
	if err := s.Prefill(2, 2, []byte("我们明天一起去台湾")); err != nil {
		t.Fatal(err)
	}
	a := &s.apps[s.find(2)]
	if len(a.tokens) <= n || !s.Warm(2, 2) || s.Warm(2, 1) {
		t.Fatalf("tokens %d → %d, warm %v", n, len(a.tokens), s.Warm(2, 2))
	}
	// The incremental context scores like a fresh one.
	opts := cands("旅游", "绿油", "履约")
	inc := make([]float64, 3)
	if _, err := s.score(2, opts, len(opts)); err != nil {
		t.Fatal(err)
	}
	for i := range inc {
		inc[i] = s.opts[i].LogP
	}
	if err := s.Prefill(3, 1, []byte("我们明天一起去台湾")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.score(3, opts, len(opts)); err != nil {
		t.Fatal(err)
	}
	for i := range inc {
		if d := math.Abs(inc[i] - s.opts[i].LogP); d > 0.05 {
			t.Errorf("%s: incremental %.3f, fresh %.3f", opts[i].Text, inc[i], s.opts[i].LogP)
		}
	}
}

func TestWarmAppsLRU(t *testing.T) {
	s := testScorer(t)
	for app := 10; app < 10+WarmApps; app++ {
		if err := s.Prefill(app, 1, []byte("你好")); err != nil {
			t.Fatal(err)
		}
	}
	s.Prefill(10, 1, []byte("你好")) // touch: 11 is now the oldest
	if err := s.Prefill(20, 1, []byte("再见")); err != nil {
		t.Fatal(err)
	}
	if s.Warm(11, 1) || !s.Warm(10, 1) || !s.Warm(12, 1) || !s.Warm(20, 1) {
		t.Errorf("warm: 10 %v 11 %v 12 %v 20 %v", s.Warm(10, 1), s.Warm(11, 1), s.Warm(12, 1), s.Warm(20, 1))
	}
}

type refItem struct {
	Context string    `json:"context"`
	Options []string  `json:"options"`
	Label   string    `json:"label"`
	P       []float64 `json:"p"`
	W       []float64 `json:"w"`
}

// refItems reads the spike's 简拼 items with fp32 PyTorch scores and
// rime-ice weights (build/laya/abbr_ref.jsonl, written by
// spikes/laya-zeroshot/export_weights.py), or skips.
func refItems(t *testing.T, n int) []refItem {
	f, err := os.Open("../../build/laya/abbr_ref.jsonl")
	if err != nil {
		t.Skip("spike reference missing: build/laya/abbr_ref.jsonl")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	var out []refItem
	for len(out) < n && sc.Scan() {
		var it refItem
		if err := json.Unmarshal(sc.Bytes(), &it); err != nil {
			t.Fatal(err)
		}
		out = append(out, it)
	}
	return out
}

func refCands(it *refItem) []Candidate {
	c := make([]Candidate, len(it.Options))
	for i, o := range it.Options {
		c[i] = Candidate{Text: []byte(o), Weight: float32(math.Log(it.W[i] + 1))}
	}
	return c
}

// TestReferenceScores compares the 8-bit model on the GPU with the spike's
// fp32 PyTorch scores on 50 items, all options scored in full: the
// probabilities over the options must agree.
func TestReferenceScores(t *testing.T) {
	s := testScorer(t)
	items := refItems(t, 50)
	var diff float64
	var n, agree int
	for i := range items {
		it := &items[i]
		if err := s.Prefill(0, uint32(i), []byte(it.Context)); err != nil {
			t.Fatal(err)
		}
		m := min(len(it.Options), rerank.MaxOptions)
		if _, err := s.score(0, refCands(it)[:m], m); err != nil {
			t.Fatal(err)
		}
		// Normalize over the options, as the reference is.
		lse := math.Inf(-1)
		for j := range m {
			lse = logAdd(lse, s.opts[j].LogP)
		}
		best, refBest := 0, 0
		for j := range m {
			if s.opts[j].LogP > s.opts[best].LogP {
				best = j
			}
			if it.P[j] > it.P[refBest] {
				refBest = j
			}
			// Probable options only: a tiny probability's log is noise.
			if it.P[j] > 0.01 {
				diff += math.Abs(s.opts[j].LogP - lse - math.Log(it.P[j]))
				n++
			}
		}
		if best == refBest {
			agree++
		}
	}
	mean := diff / float64(n)
	t.Logf("%d items: same best option %d; mean |Δ log p| %.3f over %d probable options", len(items), agree, mean, n)
	if agree < len(items)*9/10 || mean > 0.25 {
		t.Errorf("scores drifted from the reference: best agrees %d/%d, mean |Δ| %.3f", agree, len(items), mean)
	}
}

func logAdd(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	if math.IsInf(b, -1) {
		return a
	}
	return a + math.Log1p(math.Exp(b-a))
}

// TestAccuracyAndLatency runs pruned scoring and the standard policy over
// 300 spike items, and gates the latency budgets (DESIGN §7.2): prefill
// p95 ≤ 20 ms, score p50 ≤ 8 ms and p95 ≤ 15 ms, scaled by
// SHIXIN_PERF_TOLERANCE. Each item is a fresh 60-character context: the
// worst case for prefill.
func TestAccuracyAndLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("latency gate: run alone, without -short")
	}
	s := testScorer(t)
	items := refItems(t, 300)
	perm := make([]uint8, rerank.MaxOptions)
	p := Strengths[rerank.StrengthStandard]
	var pre, score []time.Duration
	var rimeFirst, rimePage, first, page int
	for i := range items {
		it := &items[i]
		c := refCands(it)
		if len(c) > 50 {
			c = c[:50]
		}
		t0 := time.Now()
		if err := s.Prefill(0, uint32(1000+i), []byte(it.Context)); err != nil {
			t.Fatal(err)
		}
		t1 := time.Now()
		got, err := s.Score(0, c, p, perm)
		if err != nil {
			t.Fatal(err)
		}
		t2 := time.Now()
		if i >= 5 { // warm-up
			pre, score = append(pre, t1.Sub(t0)), append(score, t2.Sub(t1))
		}
		label := slices.Index(it.Options[:len(c)], it.Label)
		if label < 0 {
			continue
		}
		at := slices.Index(got, uint8(label))
		first += b2i(at == 0)
		page += b2i(at < 5)
		rimeFirst += b2i(label == 0)
		rimePage += b2i(label < 5)
	}
	pct := func(n int) string { return strconv.FormatFloat(100*float64(n)/float64(len(items)), 'f', 1, 64) + "%" }
	t.Logf("%d items, first word / first page: rime %s / %s, reranked %s / %s",
		len(items), pct(rimeFirst), pct(rimePage), pct(first), pct(page))
	q := func(d []time.Duration, f float64) time.Duration {
		d = slices.Clone(d)
		slices.Sort(d)
		return d[int(float64(len(d)-1)*f)]
	}
	t.Logf("prefill p50 %v p95 %v; score p50 %v p95 %v", q(pre, .5), q(pre, .95), q(score, .5), q(score, .95))
	for _, g := range []struct {
		name   string
		got    time.Duration
		budget time.Duration
	}{
		{"prefill p95", q(pre, .95), 20 * time.Millisecond},
		{"score p50", q(score, .5), 8 * time.Millisecond},
		{"score p95", q(score, .95), 15 * time.Millisecond},
	} {
		if limit, over := perf.OverBudget(g.got, g.budget); over && !raceEnabled {
			t.Errorf("%s %v, over budget %v", g.name, g.got, limit)
		}
	}
	if first <= rimeFirst || page <= rimePage {
		t.Errorf("reranking didn't help")
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
