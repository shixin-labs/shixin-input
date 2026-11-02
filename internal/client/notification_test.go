package client

import (
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

func TestPassedPrintableCannotStartComposition(t *testing.T) {
	for _, key := range []uint32{'x', ','} {
		t.Run(string(rune(key)), func(t *testing.T) {
			c := New(Options{}, &recorder{})
			defer c.timer.Stop()
			cn := &conn{out: make(chan outMsg, 8)}
			c.conn.Store(cn)
			c.cur = &policy.Policy{Notify: policy.NotifyShift}
			c.spec = c.cur
			c.shiftDown = true
			if c.HandleKey(key, 0) {
				t.Fatal("passed key was taken")
			}
			m := <-cn.out
			if m.key.Flags&ipc.KeyNotify == 0 || m.key.Mods&policy.ReleaseMask == 0 {
				t.Fatal("passed key can still insert text in the engine")
			}
		})
	}
}

func TestKeyUsesQueuedModeReply(t *testing.T) {
	c := New(Options{}, &recorder{})
	defer c.timer.Stop()
	cn := &conn{out: make(chan outMsg, 8)}
	c.conn.Store(cn)
	idle, composing := fakePolicies()
	c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
	c.cur, c.spec = &policy.Passthrough, &policy.Passthrough
	c.push(evResult, &ipc.Result{Ctx: 1, Policy: 1})
	if !c.HandleKey('x', 0) {
		t.Fatal("first spelling key passed through despite a queued Chinese policy")
	}
	m := <-cn.out
	if m.key.Flags&ipc.KeyNotify != 0 {
		t.Fatal("first spelling key was only notified")
	}
}

// Real librime and the real client reproduce the race: the mode reply has
// arrived, its UI wake has not run, and the first key is passed/notified using
// the previous English policy. The app inserts that literal character before
// the engine commits its Chinese interpretation of the same input.
func TestQueuedModeCannotDuplicateCommittedKey(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{",", "，"}, {"xinghui ", "幸会"}} {
		t.Run(tc.input, func(t *testing.T) {
			h := newHarness(t, realEngine(t), nil)
			h.c.FocusIn("com.example.exact-output", false)
			h.until(func() bool { return !h.c.sp.on })
			chinese := h.c.cur
			stale := &policy.Policy{Notify: policy.NotifyShift}
			h.c.cur, h.c.spec, h.c.shiftDown = stale, stale, true
			h.c.push(evResult, &ipc.Result{Ctx: 1, Policy: chinese.ID})
			for i, r := range tc.input {
				if !h.c.HandleKey(uint32(r), 0) {
					h.rec.Commit([]byte(string(r)))
				}
				if i == 0 {
					h.settle()
				}
			}
			h.settle()
			if got := h.rec.commits(); got != tc.want {
				t.Fatalf("typed %q, got %q, want %q", tc.input, h.rec.commits(), tc.want)
			}
		})
	}
}
