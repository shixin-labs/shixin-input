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
	// Marked text when the key returns keeps the app from typing it too.
	if h.rec.preedit != "," || h.c.Stats().Timeouts != 1 {
		t.Fatalf("marked %q, stats %+v", h.rec.preedit, h.c.Stats())
	}
	h.settle()
	if got := h.rec.commits(); got != "，" || h.rec.preedit != "" || len(h.c.marked) != 0 {
		t.Fatalf("commits %q, marked %q (%q): want one \"，\" replacing the mark", got, h.rec.preedit, h.c.marked)
	}
}

// All of one key's waits share one budget: a late focus reply followed by
// a slow direct commit must not wait twice.
func TestKeyWaitsShareOneBudget(t *testing.T) {
	const budget = 100 * time.Millisecond
	c := New(Options{PerAppContexts: true, Budget: budget}, &recorder{})
	defer c.timer.Stop()
	cn := &conn{out: make(chan outMsg, 16)}
	c.conn.Store(cn)
	idle, composing := fakePolicies()
	c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
	c.FocusIn("first", false)
	go func() { // answers focus late and never answers the key
		for m := range cn.out {
			if m.typ == ipc.TypeKey {
				return
			}
			if m.typ == ipc.TypeClear && m.key.Seq == c.sent.Load() {
				time.Sleep(budget * 7 / 10)
			}
			c.push(evResult, &ipc.Result{Ctx: m.key.Ctx, Seq: m.key.Seq, Policy: 1})
			if m.key.Seq != 0 {
				c.arrived.Store(m.key.Seq)
			}
			c.signalArrived()
		}
	}()
	t0 := time.Now()
	if !c.HandleKey(',', 0) {
		t.Fatal("',' passed after the focus reply arrived")
	}
	if d := time.Since(t0); d > budget*3/2 {
		t.Fatalf("one key waited %v with a %v budget", d, budget)
	}
}

// hookRecorder runs hook once, from the first status change.
type hookRecorder struct {
	recorder
	hook func()
}

func (r *hookRecorder) Status(s Status) {
	r.recorder.Status(s)
	if h := r.hook; h != nil {
		r.hook = nil
		h()
	}
}

// Replies published while the key handler runs (after its first drain)
// must be applied before the key is classified: the reader marks a result
// arrived only after queueing it. The status callback stands in for the
// reader, publishing the focus replies at the end of that drain.
func TestFocusRepliesDuringKeyAreApplied(t *testing.T) {
	a := &hookRecorder{}
	c := New(Options{PerAppContexts: true, Budget: time.Second}, a)
	defer c.timer.Stop()
	cn := &conn{out: make(chan outMsg, 16)}
	c.conn.Store(cn)
	idle, composing := fakePolicies()
	c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
	c.FocusIn("app", false)
	c.push(evResult, &ipc.Result{Ctx: 99}) // gives the drain work to finish
	c.status = Status{Connected: true}     // ... and a status change to report
	a.hook = func() {
		for len(cn.out) > 0 {
			m := <-cn.out
			c.push(evResult, &ipc.Result{Ctx: m.key.Ctx, Seq: m.key.Seq, Policy: 1})
			if m.key.Seq != 0 {
				c.arrived.Store(m.key.Seq)
			}
		}
	}
	if !c.HandleKey('n', 0) {
		t.Fatal("first key passed with the focus replies already arrived")
	}
	if a.hook != nil {
		t.Fatal("the reader stand-in never ran")
	}
}

// The ordering barrier has the same gap: a commit published after the
// key's first drain must reach the app before the key the app handles.
func TestBarrierAppliesResultsArrivedDuringKey(t *testing.T) {
	a := &hookRecorder{}
	c := New(Options{Budget: time.Second}, a)
	defer c.timer.Stop()
	c.conn.Store(&conn{out: make(chan outMsg, 16)})
	idle, composing := fakePolicies()
	c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
	c.cur, c.spec = idle, idle
	c.seq = 1 // an eaten "," whose commit is on its way
	c.keys[1] = sentKey{seq: 1, keysym: ',', eaten: true}
	c.push(evResult, &ipc.Result{Ctx: 99})
	c.status = Status{Connected: true}
	a.hook = func() {
		c.push(evResult, &ipc.Result{Ctx: 1, Seq: 1, Policy: 1, Flags: ipc.ResultHandled, Commit: []byte("，")})
		c.arrived.Store(1)
	}
	if c.HandleKey('v', policy.SuperMask) {
		t.Fatal("Cmd-V eaten")
	}
	if got := a.commits(); got != "，" {
		t.Fatalf("when Cmd-V reached the app, commits were %q", got)
	}
}
