package client

import (
	"testing"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

// A direct commit slower than the budget: the key stays eaten (the engine
// has it), and its commit is inserted once, later, never also by the app.
func TestDirectCommitTimeoutStaysEaten(t *testing.T) {
	f := startFake(t)
	f.delay = 50 * time.Millisecond
	h := newHarness(t, f.path, nil)
	h.c.opt.Budget = time.Millisecond
	if !h.c.HandleKey(',', 0) {
		t.Fatal("',' passed to the app after a timeout: the engine commits it too")
	}
	if got := h.rec.commits(); got != "" {
		t.Fatalf("commit before the result arrived: %q", got)
	}
	if h.c.Stats().Timeouts != 1 {
		t.Fatalf("stats %+v", h.c.Stats())
	}
	h.settle()
	if got := h.rec.commits(); got != "，" {
		t.Fatalf("commits %q, want one \"，\"", got)
	}
}

// Only direct commits wait. Letters (marked at once), keys inside a
// composition (marked text exists) and releases return without waiting.
func TestOnlyDirectCommitsWait(t *testing.T) {
	idle, composing := fakePolicies()
	for _, tc := range []struct {
		name   string
		keysym uint32
		mods   uint32
		marked string
		pol    *policy.Policy
	}{
		{"letter", 'a', 0, "", idle},
		{"composing", ' ', 0, "ab", composing},
		{"release", ',', policy.ReleaseMask, "", idle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(Options{Budget: time.Second}, &recorder{})
			defer c.timer.Stop()
			c.conn.Store(&conn{out: make(chan outMsg, 8)}) // never answers
			c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
			c.cur, c.spec = tc.pol, tc.pol
			c.marked = append(c.marked[:0], tc.marked...)
			t0 := time.Now()
			c.HandleKey(tc.keysym, tc.mods)
			if d := time.Since(t0); d > 200*time.Millisecond {
				t.Fatalf("key waited %v for the engine", d)
			}
		})
	}
}

// A FocusIn reply that never comes (engine stuck): the first key waits only
// the budget, then passes, and the client stays usable.
func TestFocusReplyTimeoutPasses(t *testing.T) {
	c := New(Options{PerAppContexts: true, Budget: 2 * time.Millisecond}, &recorder{})
	defer c.timer.Stop()
	c.conn.Store(&conn{out: make(chan outMsg, 16)})
	idle, composing := fakePolicies()
	c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
	c.FocusIn("first", false)
	t0 := time.Now()
	if c.HandleKey('n', 0) {
		t.Fatal("key eaten without a policy")
	}
	if d := time.Since(t0); d > 500*time.Millisecond {
		t.Fatalf("key waited %v", d)
	}
	if c.Stats().Timeouts != 1 {
		t.Fatalf("stats %+v", c.Stats())
	}
}

// A secure field sends no policy Clear after FocusIn: its keys pass without
// a policy, and nothing should wait for one.
func TestSecureFocusSendsNoClear(t *testing.T) {
	c := New(Options{PerAppContexts: true}, &recorder{})
	defer c.timer.Stop()
	cn := &conn{out: make(chan outMsg, 16)}
	c.conn.Store(cn)
	c.FocusIn("first", false)
	for len(cn.out) > 0 {
		<-cn.out
	}
	seq := c.seq
	c.FocusIn("first", true)
	if len(cn.out) != 1 || (<-cn.out).typ != ipc.TypeFocusIn {
		t.Fatal("secure focus sent more than FocusIn")
	}
	if c.seq != seq {
		t.Fatal("secure focus sent a sequenced request to wait for")
	}
}

// The real engine with per-app contexts: switching to a new app and typing
// at once is Chinese input, not a literal letter.
func TestRealEngineFirstKeyInNewApp(t *testing.T) {
	h := newHarness(t, realEngine(t), nil)
	h.c.opt.PerAppContexts = true
	h.c.opt.Budget = time.Second // tests that it waits, not for how long
	for _, app := range []string{"com.example.first-key-a", "com.example.first-key-b"} {
		h.c.FocusOut()
		h.c.FocusIn(app, false)
		if !h.c.HandleKey('n', 0) {
			t.Fatalf("first key in %s passed to the app", app)
		}
		h.settle()
		if !h.c.composing {
			t.Fatalf("first key in %s did not start a composition", app)
		}
		h.c.HandleKey(policy.KeyEscape, 0)
		h.settle()
	}
}
