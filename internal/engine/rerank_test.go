package engine

import (
	"math"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/policy"
	"shixin/internal/rerank"
)

// The shared test engine reranks with testScorer, which isn't ready until
// a test gives it an order or tells it to hold requests. Rerank tests use
// their own words (测试, 台湾, 天气, 上海 in testdata) because commits teach
// librime.
var testScorer = &fakeScorer{}

const testRerankTimeout = 40 * time.Millisecond

type heldRequest struct {
	session   int
	exportSeq uint32
}

// fakeScorer answers on the engine thread, or keeps requests unanswered.
type fakeScorer struct {
	e  *Engine
	mu sync.Mutex
	// order computes an answer; hold keeps requests instead.
	order func(sl *rerank.Slot) []uint8
	hold  bool
	held  []heldRequest
	// rime is the last export's texts: rime's order.
	rime []string
	// requests counts Score calls; contexts the Context calls.
	requests, contexts int
	app                int
	text               []byte
}

func (f *fakeScorer) Ready() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.order != nil || f.hold
}

func (f *fakeScorer) Context(app int, rev uint32, text []byte) {
	f.mu.Lock()
	f.contexts++
	f.app = app
	f.text = append(f.text[:0], text...)
	f.mu.Unlock()
}

func (f *fakeScorer) Score(r *rerank.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if f.hold {
		f.rime = f.rime[:0]
		for i := range int(r.Export.Count) {
			f.rime = append(f.rime, string(r.Export.Text(i)))
		}
		f.held = append(f.held, heldRequest{r.Session, r.ExportSeq})
		return
	}
	if f.order != nil {
		f.e.Rerank(r.Session, r.ExportSeq, f.order(r.Export))
	}
}

// use sets the scorer up for one test, and resets it afterwards.
func (f *fakeScorer) use(t *testing.T, order func(*rerank.Slot) []uint8, hold bool) {
	f.mu.Lock()
	f.order, f.hold, f.held = order, hold, nil
	f.mu.Unlock()
	t.Cleanup(func() {
		f.mu.Lock()
		f.order, f.hold, f.held = nil, false, nil
		f.mu.Unlock()
	})
}

func (f *fakeScorer) takeHeld() []heldRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.held
	f.held = nil
	return h
}

func (f *fakeScorer) lastContext() (int, string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.app, string(f.text), f.contexts
}

func (f *fakeScorer) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

// reverse shows the export in reverse order, and records rime's order.
func reverse(sl *rerank.Slot) []uint8 {
	perm := make([]uint8, sl.Count)
	rime := make([]string, sl.Count)
	for i := range perm {
		perm[i] = uint8(len(perm) - 1 - i)
		rime[i] = string(sl.Text(i))
	}
	testScorer.rime = rime // under testScorer.mu: Score holds it
	return perm
}

func texts(r *ipc.Result) []string {
	var out []string
	for _, c := range r.Candidates {
		out = append(out, string(c.Text))
	}
	return out
}

func (f *fakeScorer) rimeOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rime)
}

func rerankEngine(t *testing.T) *Engine {
	e := testEngine(t)
	if !e.rr.attached {
		t.Skip("librime without shixin_rerank; build it with scripts/build-librime.sh")
	}
	return e
}

// timedKey sends a key and returns its result and how long it took.
func (cl *client) timedKey(keysym uint32, flags uint8) (*ipc.Result, time.Duration) {
	cl.seq++
	t0 := time.Now()
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: keysym, Flags: flags}).Append(cl.buf[:0]))
	r := cl.result()
	return r, time.Since(t0)
}

// TestRerankApplies: the key's one result already has the scorer's order.
func TestRerankApplies(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, reverse, false)
	r := cl.typ("ceshi")
	rime := testScorer.rimeOrder()
	if len(rime) < 3 || !slices.Contains(rime, "测试") {
		t.Fatalf("export %v", rime)
	}
	want := slices.Clone(rime[:3])
	slices.Reverse(want)
	got := texts(r)
	if !slices.Equal(got[:3], want) {
		t.Fatalf("result %v, want %v first (rime: %v)", got, want, rime)
	}
	if r := cl.key(' ', 0); string(r.Commit) != want[0] {
		t.Fatalf("commit %q, want %q", r.Commit, want[0])
	}
}

// TestRerankSwitch: off, nothing is exported, held or recorded, and
// every app's context is forgotten; on again, results are reordered.
func TestRerankSwitch(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, reverse, false)
	cl.focus("switch", 0)
	cl.typ("ceshi")
	cl.key(' ', 0)
	e.SetRerank(false)
	defer e.SetRerank(true)
	n := testScorer.requestCount()
	cl.typ("ceshi")
	cl.key(' ', 0)
	if got := testScorer.requestCount(); got != n {
		t.Errorf("%d requests while off", got-n)
	}
	e.SetRerank(true)
	cl.focus("switch", 0)
	cl.typ("ceshi")
	if testScorer.requestCount() == n {
		t.Error("no request after turning it back on")
	}
	// The context was forgotten: the request's context is empty.
	if _, text, _ := testScorer.lastContext(); text != "" {
		t.Errorf("context %q survived turning 智能排序 off", text)
	}
	cl.key(policy.KeyEscape, 0)
}

// TestRerankPausesWhenSlow: after slowMisses timeouts in a row, results
// aren't held (bar one probe in slowProbe), and a probe answered in time
// resumes.
func TestRerankPausesWhenSlow(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, true)
	start := e.Stats().Rerank.Timeouts
	for i := 0; e.Stats().Rerank.Timeouts-start < slowMisses; i++ {
		if i == 40 {
			t.Fatal("too few timeouts")
		}
		cl.typ("ceshi")
		cl.key(policy.KeyEscape, 0)
	}
	if !e.Stats().Rerank.Slow {
		t.Fatal("not paused after the timeouts")
	}
	n := testScorer.requestCount()
	var slowest time.Duration
	for _, k := range "shanghai" {
		_, d := cl.timedKey(uint32(k), 0)
		slowest = max(slowest, d)
	}
	cl.key(policy.KeyEscape, 0)
	if slowest >= testRerankTimeout || testScorer.requestCount() != n {
		t.Errorf("paused: slowest key %v, %d requests", slowest, testScorer.requestCount()-n)
	}
	// Fast again: the next probe is answered, and holding resumes.
	testScorer.use(t, reverse, false)
	for i := 0; e.Stats().Rerank.Slow; i++ {
		if i == 40 {
			t.Fatal("never resumed")
		}
		cl.typ("ceshi")
		cl.key(policy.KeyEscape, 0)
	}
}

// TestRerankTimeout: without an order in time, the result comes in rime's
// order, and the order that comes later is dropped.
func TestRerankTimeout(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, true)
	cl.typ("tianq")
	r, d := cl.timedKey('i', 0)
	if d < testRerankTimeout {
		t.Errorf("result after %v, before the %v timeout", d, testRerankTimeout)
	}
	rime := testScorer.rimeOrder()
	if got := texts(r); !slices.Equal(got[:2], rime[:2]) {
		t.Errorf("result %v, want rime's order %v", got, rime)
	}
	h := testScorer.takeHeld()
	before := e.Stats().Rerank
	last := h[len(h)-1]
	e.Rerank(last.session, last.exportSeq, []uint8{1, 0})
	// A no-op key release: its result is the next frame, no late one.
	cl.key('a', policy.ReleaseMask)
	st := e.Stats().Rerank
	if st.Dropped != before.Dropped+1 {
		t.Errorf("dropped %d, want %d", st.Dropped, before.Dropped+1)
	}
	if st.Timeouts == 0 {
		t.Error("no timeout counted")
	}
	cl.key(policy.KeyEscape, 0)
}

// TestRerankSuperseded: a newer request sends the held result at once.
func TestRerankSuperseded(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, true)
	for _, next := range []func(seq uint32) []byte{
		func(seq uint32) []byte { return (&ipc.Key{Ctx: 1, Seq: seq, Keysym: 'i'}).Append(nil) },
		func(seq uint32) []byte { return (&ipc.ChangePage{Ctx: 1, Seq: seq}).Append(nil) },
	} {
		cl.typ("shangh")
		before := e.Stats().Rerank.Superseded
		// The next request is sent before 'a''s result: that comes back
		// without waiting for the timeout.
		t0 := time.Now()
		cl.seq++
		cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: 'a'}).Append(cl.buf[:0]))
		cl.send(next(cl.seq + 1))
		r := cl.result()
		if d := time.Since(t0); r.Seq != cl.seq || d >= testRerankTimeout {
			t.Errorf("result %d after %v", r.Seq, d)
		}
		cl.seq++
		cl.result()
		if got := e.Stats().Rerank.Superseded; got != before+1 {
			t.Errorf("superseded %d, want %d", got, before+1)
		}
		cl.key(policy.KeyEscape, 0)
	}
}

// TestRerankNeverHolds: ASK keys, whose result the app waits for, and keys
// while the scorer isn't ready are answered at once.
func TestRerankNeverHolds(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, true)
	for _, k := range "ceshi" {
		if _, d := cl.timedKey(uint32(k), ipc.KeySync); d >= testRerankTimeout {
			t.Fatalf("ASK key %q held for %v", k, d)
		}
	}
	cl.key(policy.KeyEscape, 0)
	testScorer.use(t, nil, false)
	n := testScorer.requestCount()
	for _, k := range "ceshi" {
		if _, d := cl.timedKey(uint32(k), 0); d >= testRerankTimeout {
			t.Fatalf("key %q held for %v without a ready scorer", k, d)
		}
	}
	if testScorer.requestCount() != n {
		t.Error("requests sent to a scorer that isn't ready")
	}
	cl.key(policy.KeyEscape, 0)
}

func TestRerankRejectsBadOrders(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, func(*rerank.Slot) []uint8 { return []uint8{0, 0, 1} }, false)
	before := e.Stats().Rerank.Rejected
	r := cl.typ("ceshi")
	if got := e.Stats().Rerank.Rejected; got <= before {
		t.Errorf("rejected %d, want more than %d", got, before)
	}
	if len(r.Candidates) < 3 {
		t.Errorf("result %v", texts(r))
	}
	cl.key(policy.KeyEscape, 0)
}

// TestRerankExportsWeights checks the priors the policy gets: dictionary
// log weights (上海 100, 伤害 90), and learned words flagged, as their
// weights come from the user dictionary on another scale.
func TestRerankExportsWeights(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	var mu sync.Mutex
	type entry struct {
		w     float32
		flags uint8
	}
	got := map[string]entry{}
	testScorer.use(t, func(sl *rerank.Slot) []uint8 {
		mu.Lock()
		defer mu.Unlock()
		for i := range int(sl.Count) {
			got[string(sl.Text(i))] = entry{sl.Entries[i].Weight, sl.Entries[i].Flags}
		}
		return nil
	}, false)
	cl.typ("shanghai")
	cl.key(policy.KeyEscape, 0)
	cl.typ("ceshi") // 测试's order was learned in TestRerankApplies
	cl.key(policy.KeyEscape, 0)
	mu.Lock()
	defer mu.Unlock()
	a, b := got["上海"], got["伤害"]
	if a.flags != 0 || b.flags != 0 || math.Abs(float64(a.w-b.w)-math.Log(100.0/90)) > 0.01 {
		t.Errorf("上海 %+v, 伤害 %+v: want dictionary words a log(100/90) apart", a, b)
	}
	learned := 0
	for _, w := range []string{"测试", "侧室", "策士"} {
		if got[w].flags == rerank.EntryUser {
			learned++
		}
	}
	if learned == 0 {
		t.Errorf("no learned word among 测试 侧室 策士: %v", got)
	}
}

func TestRerankAppContexts(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	// commit types code and commits #1, which earlier tests may have
	// taught librime.
	commit := func(code string) string {
		t.Helper()
		cl.typ(code)
		return string(cl.key(' ', 0).Commit)
	}
	cl.focus("chat", 0)
	w1 := commit("ceshi")
	chat, text, _ := testScorer.lastContext()
	if text != w1 {
		t.Fatalf("chat context %q, want %q", text, w1)
	}
	cl.focus("editor", 0)
	w2 := commit("tianqi")
	editor, text, _ := testScorer.lastContext()
	if editor == chat || text != w2 {
		t.Fatalf("editor context %d %q (chat %d)", editor, text, chat)
	}
	// Back in the chat: its context continues after a soft break.
	cl.focus("chat", 0)
	if app, text, _ := testScorer.lastContext(); app != chat || text != w1+"\n" {
		t.Fatalf("chat after focus: %d %q", app, text)
	}
	w3 := commit("taiwan")
	if _, text, _ := testScorer.lastContext(); text != w1+"\n"+w3 {
		t.Fatalf("chat context %q", text)
	}
	// Secure fields record nothing.
	_, _, calls := testScorer.lastContext()
	cl.focus("chat", ipc.FocusSecure)
	cl.typ("abc")
	cl.key(policy.KeyReturn, 0)
	if _, _, got := testScorer.lastContext(); got != calls {
		t.Errorf("%d context updates in a secure field", got-calls)
	}
	cl.focus("chat", 0)
	// A caret move passed to the app breaks the text too: idle caret keys
	// are PASS_NOTIFY.
	w4 := commit("shanghai")
	for _, k := range []uint32{policy.KeyLeft, policy.KeyHome, policy.KeyBackSpace} {
		for _, mods := range []uint32{0, policy.ShiftMask} {
			if d := cl.policy().Lookup(k, mods).Disposition(); d != policy.PassNotify {
				t.Errorf("idle key %#x mods %#x: %v, want PASS_NOTIFY", k, mods, d)
			}
		}
	}
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: policy.KeyLeft, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
	cl.result()
	if _, text, _ := testScorer.lastContext(); text != w1+"\n"+w3+"\n"+w4+"\n" {
		t.Fatalf("after a caret move: %q", text)
	}
}

func TestRerankKeyPathDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	e := rerankEngine(t)
	cl := dial(t, e)
	perm := []uint8{2, 1, 0}
	testScorer.use(t, func(sl *rerank.Slot) []uint8 { return perm[:min(int(sl.Count), 3)] }, false)
	keys := []uint32{'c', 'e', 's', 'h', 'i', ' ', 'c', 'e', 's', 'h', 'i', policy.KeyEscape}
	run := func() {
		for _, k := range keys {
			cl.seq++
			cl.buf = (&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: k}).Append(cl.buf[:0])
			cl.c.Write(cl.buf)
			for {
				typ, payload, err := cl.r.Next()
				if err != nil {
					t.Fatal(err)
				}
				if typ == ipc.TypeResult {
					cl.res.Decode(payload)
					break
				}
			}
		}
	}
	run() // warm buffers
	applied := e.Stats().Rerank.Applied
	runtime.GC()
	if n := testing.AllocsPerRun(100, run); n != 0 {
		t.Fatalf("%v allocations per %d keys with reranking (engine + client)", n, len(keys))
	}
	if e.Stats().Rerank.Applied == applied {
		t.Error("no order was applied")
	}
}
