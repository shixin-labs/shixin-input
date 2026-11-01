package client

import (
	"strconv"
	"testing"

	"shixin/internal/ipc"
)

func TestPerAppContexts(t *testing.T) {
	c := New(Options{PerAppContexts: true}, &recorder{})
	defer c.timer.Stop()
	c.FocusIn("first", false)
	first := c.ctx
	c.clearSeq, c.dropZero = 42, true // old field's asynchronous Clear fence
	c.FocusIn("second", false)
	second := c.ctx
	if first == second || c.clearSeq != c.seq+1 || !c.dropZero {
		t.Fatal("switching apps reused the session or failed to fence its previous owner")
	}
	drop := c.dropZero
	if c.acceptResult(&ipc.Result{Ctx: first, Seq: 43, Commit: []byte("old")}, &drop) {
		t.Fatal("old app's late result can reach the new text field")
	}
	if c.acceptResult(&ipc.Result{Ctx: second, Seq: c.contextFence}, &drop) ||
		!c.acceptResult(&ipc.Result{Ctx: second}, &drop) {
		t.Fatal("target fence failed to admit its following FocusIn reply")
	}
	c.FocusIn("first", false)
	if c.ctx != first {
		t.Fatal("returning to an app did not reuse its engine context")
	}
	c.FocusIn("first", true)
	if c.ctx != first {
		t.Fatal("secure field changed the app's context identity")
	}
	for i := range 100 {
		c.FocusIn("app"+strconv.Itoa(i), false)
		if c.ctx == 0 || int(c.ctx) > len(c.contexts) {
			t.Fatal("context LRU grew beyond its fixed engine-session budget")
		}
	}
}

func TestLegacySingleContext(t *testing.T) {
	c := New(Options{}, &recorder{})
	defer c.timer.Stop()
	c.FocusIn("first", false)
	ctx := c.ctx
	c.FocusIn("second", false)
	if c.ctx != ctx {
		t.Fatal("single-context callers changed behavior without opting in")
	}
}

func TestPerAppContextFence(t *testing.T) {
	c := New(Options{PerAppContexts: true}, &recorder{})
	defer c.timer.Stop()
	c.conn.Store(&conn{out: make(chan outMsg, 16)})
	c.seq = 10
	c.FocusIn("first", false)
	c.FocusIn("second", false)
	fence := c.contextFence
	if fence != 11 {
		t.Fatalf("target context was not fenced: %d", fence)
	}
	drop := c.dropZero
	if c.acceptResult(&ipc.Result{Ctx: c.ctx, Seq: 9, Commit: []byte("old")}, &drop) ||
		c.acceptResult(&ipc.Result{Ctx: c.ctx}, &drop) {
		t.Fatal("a reused context accepted its previous owner's output")
	}
	if c.acceptResult(&ipc.Result{Ctx: c.ctx, Seq: fence}, &drop) {
		t.Fatal("the target Clear reply installed the previous owner's policy")
	}
	if drop || !c.acceptResult(&ipc.Result{Ctx: c.ctx}, &drop) {
		t.Fatal("the new FocusIn reply stayed fenced after Clear")
	}
}

func TestPerAppContextSecureSwitchDoesNotCommitOldText(t *testing.T) {
	a := &recorder{}
	c := New(Options{PerAppContexts: true}, a)
	defer c.timer.Stop()
	c.FocusIn("first", false)
	c.marked = append(c.marked, "old field"...)
	c.composing = true
	c.FocusIn("password app", true)
	if a.commits() != "" || len(c.marked) != 0 || c.composing || !c.secure {
		t.Fatal("FocusIn committed through the new secure field's proxy")
	}
}

func TestPerAppContextFailedClearKeepsFence(t *testing.T) {
	a := &recorder{}
	c := New(Options{PerAppContexts: true}, a)
	defer c.timer.Stop()
	c.FocusIn("first", false)
	c.seq = 10
	// No connection: Clear cannot be posted, but output already read by
	// the old connection must remain fenced until reconnect is processed.
	c.FocusIn("second", false)
	drop := c.dropZero
	if !drop || c.clearSeq != 11 || c.contextFence != 11 ||
		c.acceptResult(&ipc.Result{Ctx: c.ctx, Seq: 9, Commit: []byte("old")}, &drop) ||
		c.acceptResult(&ipc.Result{Ctx: c.ctx}, &drop) {
		t.Fatal("failed Clear allowed the previous context owner's late output")
	}
}
