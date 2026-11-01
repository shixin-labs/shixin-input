package client

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"shixin/internal/engine"
	"shixin/internal/ipc"
	"shixin/internal/policy"
)

// raceEnabled is set by race_test.go in -race builds.
var raceEnabled bool

// recorder is an Applier that records what was applied, in order.
type recorder struct {
	log     []string
	preedit string
	cands   int
	status  Status
}

func (r *recorder) Candidates(res *ipc.Result) { r.cands = len(res.Candidates) }

// Commit replaces the marked text, as insertText does in a real app.
func (r *recorder) Commit(text []byte) {
	r.log = append(r.log, "commit:"+string(text))
	r.preedit = ""
}
func (r *recorder) Preedit(text []byte, cursor int) {
	r.preedit = string(text)
	r.log = append(r.log, "preedit:"+string(text))
}
func (r *recorder) Status(s Status) { r.status = s }

func (r *recorder) commits() string {
	var b strings.Builder
	for _, l := range r.log {
		if s, ok := strings.CutPrefix(l, "commit:"); ok {
			b.WriteString(s)
		}
	}
	return b.String()
}

// harness runs a client with the test goroutine as its main thread.
type harness struct {
	t    *testing.T
	c    *Client
	rec  *recorder
	wake chan struct{}
	stop chan struct{}
}

func newHarness(t *testing.T, socket string, a Applier) *harness {
	h := &harness{t: t, wake: make(chan struct{}, 1), stop: make(chan struct{})}
	if a == nil {
		h.rec = &recorder{}
		a = h.rec
	}
	h.c = New(Options{
		Socket: socket, Kind: ipc.KindTest, App: "test",
		Wake: func() {
			select {
			case h.wake <- struct{}{}:
			default:
			}
		},
		Logf: t.Logf,
	}, a)
	h.c.Start(h.stop)
	t.Cleanup(func() { close(h.stop) })
	h.until(func() bool { return h.c.connected })
	h.c.FocusIn("com.example.Test", false)
	h.until(func() bool { return h.c.cur != &policy.Passthrough })
	return h
}

// until drains on the "main thread" until cond holds.
func (h *harness) until(cond func() bool) {
	h.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		h.c.Drain()
		if cond() {
			return
		}
		select {
		case <-h.wake:
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			h.t.Fatal("timed out")
		}
	}
}

// settle waits until every result has arrived and applies them. The reader
// queues a result before marking it arrived, so a Drain after the last
// arrival sees it; the Drain inside until may have run just before it.
func (h *harness) settle() {
	h.until(func() bool { return !h.c.pending() })
	h.c.Drain()
}

func (h *harness) typ(s string) []bool {
	var out []bool
	for i := 0; i < len(s); i++ {
		out = append(out, h.c.HandleKey(uint32(s[i]), 0))
	}
	return out
}

// ---------------------------------------------------------------- real engine

var (
	realOnce   sync.Once
	realSocket string
	realErr    error
)

func realEngine(t *testing.T) string {
	lib := os.Getenv("SHIXIN_LIBRIME")
	if lib == "" {
		lib = "/opt/homebrew/opt/librime/lib/librime.1.dylib"
	}
	if _, err := os.Stat(lib); err != nil {
		t.Skip("librime not found; set SHIXIN_LIBRIME")
	}
	realOnce.Do(func() {
		dir, err := ipc.TempDir("shixin-client-test-")
		if err != nil {
			realErr = err
			return
		}
		data, _ := filepath.Abs("../engine/testdata/rime")
		user := filepath.Join(dir, "user")
		if _, err := engine.Lock(user); err != nil {
			realErr = err
			return
		}
		e, err := engine.Start(engine.Options{
			LibPath: lib, SharedDataDir: data, UserDataDir: user, LogDir: dir,
			MinLogLevel: 2, Version: "test", Deploy: true,
			Apps: engine.AppModes{AutoSpace: true},
		})
		if err != nil {
			realErr = err
			return
		}
		realSocket = filepath.Join(dir, "e.sock")
		l, err := ipc.Listen(realSocket)
		if err != nil {
			realErr = err
			return
		}
		go e.Serve(l)
	})
	if realErr != nil {
		t.Fatal(realErr)
	}
	return realSocket
}

func TestTypingBurst(t *testing.T) {
	h := newHarness(t, realEngine(t), nil)
	// Every key is classified before any result arrives: the space must be
	// treated as "select" because 'n' started a composition (speculation).
	for i, eaten := range h.typ("nihao ") {
		if !eaten {
			t.Fatalf("key %d passed through", i)
		}
	}
	h.settle()
	if got := h.rec.commits(); got != "你好" {
		t.Fatalf("commits %q, log %v", got, h.rec.log)
	}
	if h.rec.preedit != "" || h.rec.cands != 0 {
		t.Fatalf("leftover preedit %q / %d candidates", h.rec.preedit, h.rec.cands)
	}
	if st := h.c.Stats(); st.Mispredicts != 0 || st.Timeouts != 0 {
		t.Fatalf("stats %+v", st)
	}
	// Return when idle goes to the app.
	if h.c.HandleKey(0xff0d, 0) {
		t.Fatal("idle Return was eaten")
	}
}

func TestShiftTapSwitchesMode(t *testing.T) {
	h := newHarness(t, realEngine(t), nil)
	h.c.HandleKey(policy.KeyShiftL, 0)
	h.c.HandleKey(policy.KeyShiftL, policy.ReleaseMask)
	h.until(func() bool { return h.rec.status.ASCII })
	if h.c.HandleKey('a', 0) {
		t.Fatal("letter eaten in ASCII mode")
	}
	h.c.HandleKey(policy.KeyShiftL, 0)
	h.c.HandleKey(policy.KeyShiftL, policy.ReleaseMask)
	h.until(func() bool { return !h.rec.status.ASCII })
}

func TestShortcutWithShiftDoesNotSwitchMode(t *testing.T) {
	h := newHarness(t, realEngine(t), nil)
	h.c.HandleKey(policy.KeyShiftL, 0)
	if h.c.HandleKey('4', policy.SuperMask|policy.ShiftMask) { // Cmd+Shift+4
		t.Fatal("shortcut eaten")
	}
	h.c.HandleKey(policy.KeyShiftL, policy.ReleaseMask)
	h.settle()
	if h.rec.status.ASCII {
		t.Fatal("Cmd+Shift+4 toggled ASCII mode")
	}
}

type nopApplier struct{}

func (nopApplier) Candidates(*ipc.Result) {}
func (nopApplier) Commit([]byte)          {}
func (nopApplier) Preedit([]byte, int)    {}
func (nopApplier) Status(Status)          {}

func TestKeyPathDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	h := newHarness(t, realEngine(t), nopApplier{})
	// nihao, Escape; nihao, space (a commit, with auto spacing on).
	keys := []uint32{'n', 'i', 'h', 'a', 'o', 0xff1b, 'n', 'i', 'h', 'a', 'o', ' '}
	run := func() {
		for _, k := range keys {
			h.c.HandleKey(k, 0)
		}
		for h.c.pending() {
			h.c.wait(0)
			h.c.Drain()
		}
		h.c.Drain()
	}
	run()
	runtime.GC()
	if n := testing.AllocsPerRun(100, run); n != 0 {
		t.Fatalf("%v allocations per %d keys (client + engine)", n, len(keys))
	}
}

// ---------------------------------------------------------------- fake engine

// fake is a scripted engine: letters compose, space commits "X", '2'
// commits "中文", ',' commits "，", 'x' is declined, and keys can be
// delayed.
type fake struct {
	t         *testing.T
	path      string
	l         net.Listener
	delay     time.Duration
	syncDelay time.Duration
	mu        sync.Mutex
	got       []ipc.Key
	conns     []net.Conn
	autoSpace atomic.Bool // set ResultAutoSpace
}

var (
	eatC = policy.Act(policy.Eat, policy.Composing)
	eatS = policy.Act(policy.Eat, policy.Same)
	eatU = policy.Act(policy.Eat, policy.Unknown)
	ask  = policy.Act(policy.Ask, policy.Unknown)
)

func fakePolicies() (idle, comp *policy.Policy) {
	idle = &policy.Policy{ID: 1, Composing: 2, Notify: policy.NotifyShift}
	comp = &policy.Policy{ID: 2, Composing: 2, Notify: policy.NotifyShift, SpecialDefault: ask, CommandDefault: ask,
		Special: []policy.Special{{Keysym: 0xff0d, Action: eatU}}}
	for c := 'a'; c <= 'z'; c++ {
		idle.Printable[c-0x20] = eatC
		comp.Printable[c-0x20] = eatC
	}
	idle.Printable[','-0x20] = eatS
	idle.Printable['1'-0x20] = ask
	comp.Printable[' '-0x20] = eatU
	comp.Printable['2'-0x20] = eatU
	return idle, comp
}

func startFake(t *testing.T) *fake {
	dir, err := ipc.TempDir("sxc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fake{t: t, path: filepath.Join(dir, "f.sock")}
	f.listen()
	t.Cleanup(f.close)
	return f
}

func (f *fake) listen() {
	l, err := ipc.Listen(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.l = l
	go func() {
		for {
			c, err := ipc.Accept(l)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.conns = append(f.conns, c)
			f.mu.Unlock()
			go f.serve(c)
		}
	}()
}

func (f *fake) close() {
	f.l.Close()
	f.mu.Lock()
	for _, c := range f.conns {
		c.Close()
	}
	f.conns = nil
	f.mu.Unlock()
}

func (f *fake) keys() []ipc.Key {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ipc.Key(nil), f.got...)
}

func (f *fake) serve(c net.Conn) {
	idle, comp := fakePolicies()
	r := ipc.NewReader(c, 1024)
	composing := false
	preedit := ""
	sent := false
	send := func(res ipc.Result) {
		var b []byte
		if !sent {
			b = ipc.AppendPolicy(b, idle)
			b = ipc.AppendPolicy(b, comp)
			sent = true
		}
		res.Policy = 1
		if f.autoSpace.Load() {
			res.Flags |= ipc.ResultAutoSpace
		}
		if composing {
			res.Policy = 2
			res.Flags |= ipc.ResultComposing
			res.Preedit = []byte(preedit)
			res.Candidates = []ipc.Candidate{{Text: []byte("候选")}}
		}
		c.Write(res.Append(b))
	}
	for {
		typ, payload, err := r.Next()
		if err != nil {
			return
		}
		switch typ {
		case ipc.TypeHello:
			c.Write((&ipc.Welcome{Version: ipc.Version}).Append(nil))
		case ipc.TypeFocusIn:
			send(ipc.Result{Ctx: 1})
		case ipc.TypeClear:
			var m ipc.Clear
			m.Decode(payload)
			composing, preedit = false, ""
			send(ipc.Result{Ctx: 1, Seq: m.Seq})
		case ipc.TypeKey:
			var k ipc.Key
			k.Decode(payload)
			f.mu.Lock()
			f.got = append(f.got, k)
			f.mu.Unlock()
			d := f.delay
			if k.Flags&ipc.KeySync != 0 {
				d = f.syncDelay
			}
			time.Sleep(d)
			res := ipc.Result{Ctx: 1, Seq: k.Seq, Flags: ipc.ResultHandled}
			switch {
			case k.Mods&(policy.ReleaseMask|policy.CommandMask) != 0 || k.Keysym > 0xff00:
				if composing && k.Keysym == 0xff0d {
					res.Commit = []byte(preedit)
					composing, preedit = false, ""
				} else {
					res.Flags = 0
				}
			case k.Keysym == 'x':
				res.Flags = 0
			case k.Keysym >= 'a' && k.Keysym <= 'z':
				composing = true
				preedit += string(rune(k.Keysym))
			case k.Keysym == ' ' && composing:
				res.Commit = []byte("X")
				composing, preedit = false, ""
			case k.Keysym == '2' && composing:
				res.Commit = []byte("中文")
				composing, preedit = false, ""
			case k.Keysym == ',' && !composing:
				res.Commit = []byte("，")
			default:
				res.Flags = 0
			}
			send(res)
		}
	}
}

func TestOrderingBarrier(t *testing.T) {
	f := startFake(t)
	f.delay = 3 * time.Millisecond
	h := newHarness(t, f.path, nil)
	// This tests that the client waits, not for how long: a busy CI runner
	// can take longer than the 8 ms default to answer.
	h.c.opt.Budget = time.Second
	if !h.c.HandleKey(',', 0) {
		t.Fatal("',' not eaten")
	}
	// Cmd-V right after: the app must get "，" before the paste.
	if h.c.HandleKey('v', policy.SuperMask) {
		t.Fatal("Cmd-V eaten")
	}
	if got := h.rec.commits(); got != "，" {
		t.Fatalf("before Cmd-V reached the app, commits were %q", got)
	}
	if h.c.Stats().Barriers != 1 {
		t.Fatalf("stats %+v", h.c.Stats())
	}
}

func TestUnknownStateWaits(t *testing.T) {
	f := startFake(t)
	f.delay = 2 * time.Millisecond
	h := newHarness(t, f.path, nil)
	// This tests that the client waits, not for how long: a busy CI runner
	// can take longer than the 8 ms default to answer.
	h.c.opt.Budget = time.Second
	h.typ("ab ") // space may end the composition (Eat, Unknown)
	// Return: eaten while composing, passed when idle. The client must
	// wait for the space's result instead of guessing "composing".
	if h.c.HandleKey(0xff0d, 0) {
		t.Fatal("Return eaten after the composition ended")
	}
	if got := h.rec.commits(); got != "X" {
		t.Fatalf("commits %q", got)
	}
	h.settle()
	if st := h.c.Stats(); st.Mispredicts != 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestAskTimeoutPassesThrough(t *testing.T) {
	f := startFake(t)
	f.syncDelay = 60 * time.Millisecond
	h := newHarness(t, f.path, nil)
	t0 := time.Now()
	if h.c.HandleKey('1', 0) { // ASK in the fake's idle policy
		t.Fatal("timed-out ASK key was eaten")
	}
	if d := time.Since(t0); d > 30*time.Millisecond {
		t.Fatalf("ASK waited %v (budget 8 ms)", d)
	}
	if h.c.Stats().Timeouts != 1 {
		t.Fatalf("stats %+v", h.c.Stats())
	}
}

func TestMispredictRepair(t *testing.T) {
	f := startFake(t)
	h := newHarness(t, f.path, nil)
	if !h.c.HandleKey('x', 0) {
		t.Fatal("'x' not eaten")
	}
	h.settle()
	if got := h.rec.commits(); got != "x" || h.c.Stats().Repairs != 1 {
		t.Fatalf("commits %q stats %+v", got, h.c.Stats())
	}
}

func TestShiftHeldNotifies(t *testing.T) {
	f := startFake(t)
	h := newHarness(t, f.path, nil)
	h.c.HandleKey(policy.KeyShiftL, 0)
	h.c.HandleKey('4', policy.SuperMask|policy.ShiftMask)
	h.c.HandleKey(policy.KeyShiftL, policy.ReleaseMask)
	h.settle()
	keys := f.keys()
	if len(keys) != 3 || keys[1].Keysym != '4' || keys[1].Flags&ipc.KeyNotify == 0 {
		t.Fatalf("engine saw %+v", keys)
	}
}

func TestDisconnectAndReconnect(t *testing.T) {
	f := startFake(t)
	h := newHarness(t, f.path, nil)
	h.typ("ab")
	h.settle()
	if h.rec.preedit != "ab" {
		t.Fatalf("preedit %q", h.rec.preedit)
	}
	f.close()
	h.until(func() bool { return !h.rec.status.Connected })
	if h.rec.preedit != "" || h.rec.cands != 0 {
		t.Fatal("composition left in the app after disconnect")
	}
	if h.c.HandleKey('a', 0) {
		t.Fatal("key eaten while disconnected")
	}
	f.listen()
	h.until(func() bool { return h.rec.status.Connected && h.c.cur != &policy.Passthrough })
	if !h.c.HandleKey('a', 0) {
		t.Fatal("key not eaten after reconnect")
	}
}

func TestFocusOutCommitsPreedit(t *testing.T) {
	f := startFake(t)
	h := newHarness(t, f.path, nil)
	h.typ("ab")
	h.settle()
	h.c.FocusOut()
	if h.rec.preedit != "" || h.rec.commits() != "ab" {
		t.Fatalf("after FocusOut: %v", h.rec.log)
	}
	h.settle()
	h.c.FocusIn("com.example.Test", false)
	h.settle()
	h.typ("c")
	h.settle()
	if h.rec.preedit != "c" {
		t.Fatalf("engine composition wasn't cleared: %q", h.rec.preedit)
	}
}

func TestFocusOutRealPreedit(t *testing.T) {
	h := newHarness(t, realEngine(t), nil)
	h.typ("nihao")
	h.settle()
	// This schema shows the pinyin preedit, not the first candidate 你好.
	if h.rec.preedit != "ni hao" {
		t.Fatalf("inline preedit %q", h.rec.preedit)
	}
	h.c.FocusOut()
	h.settle()
	if h.rec.commits() != "nihao" || h.rec.preedit != "" || h.rec.cands != 0 {
		t.Fatalf("after FocusOut: %v", h.rec.log)
	}
}

// finish tests don't need a running engine: withholding its results makes
// the late-result race deterministic, including replies already queued.
func TestFinishCompositionLateResults(t *testing.T) {
	for _, focusOut := range []bool{false, true} {
		for _, queued := range []bool{false, true} {
			r := &recorder{}
			c := New(Options{}, r)
			cn := &conn{out: make(chan outMsg, 32)}
			c.conn.Store(cn)
			c.seq = 2 // second key or selection is still outstanding
			c.keys[2] = sentKey{seq: 2, keysym: 'x', eaten: true}
			shown := ipc.Result{Ctx: 1, Seq: 1, Flags: ipc.ResultComposing | ipc.ResultHandled,
				Preedit: []byte("ni 好"), Candidates: []ipc.Candidate{{Text: []byte("你好")}}}
			c.push(evResult, &shown)
			c.Drain()
			// Overwrite the result slot's buffer: marked text must be owned
			// by the client rather than alias the reader's reusable storage.
			copy(c.slots[0].res.Preedit, "wrong")
			late := ipc.Result{Ctx: 1, Seq: 2, Commit: []byte("你好"), Preedit: []byte("late"), Candidates: shown.Candidates}
			if queued {
				c.push(evResult, &late)
			}
			finish := c.CommitComposition
			if focusOut {
				finish = c.FocusOut
			}
			finish()
			clear := <-cn.out
			if clear.typ != ipc.TypeClear || clear.key.Seq != 3 {
				t.Fatalf("cleanup message %+v", clear)
			}
			if !queued {
				c.push(evResult, &late)
			}
			// An old unsequenced focus result must also stay invisible.
			shown.Seq = 0
			c.push(evResult, &shown)
			c.Drain()
			if r.commits() != "ni好" || r.preedit != "" || r.cands != 0 || c.stats.Repairs != 0 {
				t.Fatalf("focusOut %v queued %v: log %v, candidates %d", focusOut, queued, r.log, r.cands)
			}
			c.FocusIn("com.example.New", false)
			c.push(evResult, &ipc.Result{Ctx: 1, Seq: 3})                 // clear acknowledgement
			c.push(evResult, &ipc.Result{Ctx: 1, Flags: ipc.ResultASCII}) // fresh FocusIn
			c.Drain()
			if !r.status.ASCII {
				t.Fatal("fresh focus result suppressed")
			}
			finish() // a later deactivation/source notification cannot double commit
			c.Drain()
			if r.commits() != "ni好" {
				t.Fatalf("duplicate commit: %v", r.log)
			}
		}
	}
}

func TestFinishBeforeFirstPreedit(t *testing.T) {
	r := &recorder{}
	c := New(Options{}, r)
	c.seq = 1 // speculative composition, nothing displayed yet
	c.FocusOut()
	c.push(evResult, &ipc.Result{Ctx: 1, Seq: 1, Commit: []byte("late"), Preedit: []byte("late")})
	c.Drain()
	if r.commits() != "" || r.preedit != "" {
		t.Fatalf("undisplayed output reached the app: %v", r.log)
	}
}

func TestKeyAfterLocalCommit(t *testing.T) {
	c := New(Options{}, nopApplier{})
	cn := &conn{out: make(chan outMsg, 4)}
	c.conn.Store(cn)
	idle, comp := fakePolicies()
	c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: comp})
	c.CommitComposition()
	clear := <-cn.out
	// Reader has published the clear acknowledgement, but the main queue
	// hasn't dispatched Drain yet. The next letter must still be eaten.
	c.push(evResult, &ipc.Result{Ctx: 1, Seq: clear.key.Seq, Policy: 1})
	c.arrived.Store(clear.key.Seq)
	if !c.HandleKey('n', 0) {
		t.Fatal("first key passed before clear acknowledgement was applied")
	}
}

func TestFinishCompositionAutoSpace(t *testing.T) {
	r := &recorder{}
	c := New(Options{}, r)
	c.sp.on = true
	c.commit([]byte("中文"))
	c.push(evResult, &ipc.Result{Ctx: 1, Flags: ipc.ResultAutoSpace | ipc.ResultComposing, Preedit: []byte("abc")})
	c.Drain()
	c.CommitComposition()
	if r.commits() != "中文 abc" {
		t.Fatalf("local commit bypassed spacer: %v", r.log)
	}
}

func TestFinishCompositionDoesNotAllocate(t *testing.T) {
	c := New(Options{}, nopApplier{})
	cn := &conn{out: make(chan outMsg, 4)}
	c.conn.Store(cn)
	r := ipc.Result{Ctx: 1, Flags: ipc.ResultComposing, Preedit: []byte("ni hao")}
	run := func() {
		c.seq++
		r.Seq = c.seq
		c.push(evResult, &r)
		c.Drain()
		c.CommitComposition()
		m := <-cn.out
		c.push(evResult, &ipc.Result{Ctx: 1, Seq: m.key.Seq})
		c.arrived.Store(m.key.Seq)
		c.Drain()
	}
	if n := testing.AllocsPerRun(100, run); n != 0 {
		t.Fatalf("local finish allocates %v times", n)
	}
}

func TestAutoSpace(t *testing.T) {
	f := startFake(t)
	f.autoSpace.Store(true)
	h := newHarness(t, f.path, nil)
	h.typ("ni2")
	h.settle()
	if !h.c.HandleKey('A', policy.ShiftMask) {
		t.Fatal("a letter after Chinese wasn't spaced")
	}
	if h.c.HandleKey('B', policy.ShiftMask) {
		t.Fatal("a letter after a letter was taken")
	}
	h.typ("ni2")
	h.settle()
	if got := h.rec.commits(); got != "中文 A 中文" {
		t.Fatalf("commits %q", got)
	}
	// Unknown after a click or a focus change, and no reader to ask.
	h.c.ResetSpacing()
	if h.c.HandleKey('C', policy.ShiftMask) {
		t.Fatal("spaced after a click")
	}
	h.typ("ni2")
	h.settle()
	h.c.FocusIn("com.example.Other", false)
	h.settle()
	if h.c.HandleKey('D', policy.ShiftMask) {
		t.Fatal("spaced in a new text field")
	}
	if got := h.rec.commits(); got != "中文 A 中文 中文" {
		t.Fatalf("commits %q", got)
	}
}

func TestAutoSpaceOff(t *testing.T) {
	f := startFake(t)
	h := newHarness(t, f.path, nil)
	h.typ("ni2")
	h.settle()
	if h.c.HandleKey('A', policy.ShiftMask) {
		t.Fatal("spaced without ResultAutoSpace")
	}
	h.typ("ni2")
	h.settle()
	if got := h.rec.commits(); got != "中文中文" {
		t.Fatalf("commits %q", got)
	}
}

func TestAutoSpaceAfterHiddenSelectAll(t *testing.T) {
	f := startFake(t)
	f.autoSpace.Store(true)
	h := newHarness(t, f.path, nil)
	h.typ("ni2")
	h.settle()
	// Safari consumes Cmd+A before IMK can forward it. The front must
	// reset on Command's flags change; Backspace then deletes the whole
	// selection, rather than the one character the spacer would pop.
	h.c.ResetSpacing()
	if h.c.HandleKey(policy.KeyBackSpace, 0) {
		t.Fatal("selection deletion eaten")
	}
	if h.c.HandleKey('A', policy.ShiftMask) {
		t.Fatal("inserted a space after deleting selected Chinese text")
	}
	if got := h.rec.commits(); got != "中文" {
		t.Fatalf("unexpected insertion after selection deletion: %q", got)
	}
}

// TestDecimalPoint: the engine must see the digits the app inserts, or
// librime commits 3。5 for 3.5 (punctuator digit separators).
func TestDecimalPoint(t *testing.T) {
	h := newHarness(t, realEngine(t), nil)
	var app strings.Builder
	for _, k := range "3.5" {
		if !h.c.HandleKey(uint32(k), 0) {
			app.WriteRune(k)
		}
		h.settle()
	}
	if got := app.String() + h.rec.commits(); got != "3.5" {
		t.Fatalf("typed 3.5, got %q", got)
	}
}

// TestMigrationDrainsQueuedCommit proves that the main-thread migration
// boundary includes writes posted before Drain sees the migration frame.
func TestMigrationDrainsQueuedCommit(t *testing.T) {
	dir, err := ipc.TempDir("sxc-migrate-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	endpoint := filepath.Join(dir, "old.sock")
	l, err := ipc.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	next := startFake(t)
	sent := make(chan struct{})
	migrate := make(chan struct{})
	drained := make(chan error, 1)
	go func() {
		nc, err := ipc.Accept(l)
		if err != nil {
			drained <- err
			return
		}
		defer nc.Close()
		r := ipc.NewReader(nc, 4096)
		var key ipc.Key
		idle, comp := fakePolicies()
		b := []byte(nil)
		b = ipc.AppendPolicy(b, idle)
		b = ipc.AppendPolicy(b, comp)
		for {
			typ, _, err := r.Next()
			if err != nil {
				drained <- err
				return
			}
			if typ == ipc.TypeHello {
				nc.Write((&ipc.Welcome{Version: ipc.Version}).Append(nil))
			}
			if typ == ipc.TypeFocusIn {
				nc.Write(b)
				nc.Write((&ipc.Result{Ctx: 1, Policy: 1}).Append(nil))
				break
			}
		}
		<-migrate
		nc.Write((&ipc.Migrate{Endpoint: []byte(next.path)}).Append(nil))
		close(sent)
		for {
			typ, p, err := r.Next()
			if errors.Is(err, io.EOF) {
				drained <- nil
				return
			}
			if err != nil {
				drained <- err
				return
			}
			if typ == ipc.TypeKey {
				if err := key.Decode(p); err != nil {
					drained <- err
					return
				}
				res := ipc.Result{Ctx: key.Ctx, Seq: key.Seq, Policy: 1, Flags: ipc.ResultHandled}
				if key.Keysym == ' ' {
					res.Commit = []byte("X")
				}
				nc.Write(res.Append(nil))
			}
		}
	}()
	h := newHarness(t, endpoint, nil)
	close(migrate)
	<-sent
	// The main thread has not seen Migrate yet: these are still legitimate
	// accepted writes, and must precede the writer's half-close marker.
	h.typ("ab ")
	h.until(func() bool { return h.c.Stats().Connects == 2 && h.c.cur != &policy.Passthrough })
	h.settle()
	if got := h.rec.commits(); got != "X" {
		t.Fatalf("queued commit %q", got)
	}
	select {
	case err := <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("old writes were not half-closed")
	}
	h.typ("ab ")
	h.settle()
	if got := h.rec.commits(); got != "XX" {
		t.Fatalf("commit after migration %q", got)
	}
}
