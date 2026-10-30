package client

import (
	"bytes"
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

type secureRecorder struct {
	recorder
	reads int
}

func (r *secureRecorder) TextBefore() (rune, bool) {
	r.reads++
	return '中', true
}

func TestSecureFocusGuardsBeforeReply(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "known"}[known], func(t *testing.T) {
			a := &secureRecorder{}
			c := New(Options{}, a)
			cn := &conn{out: make(chan outMsg, 16)}
			c.conn.Store(cn)
			idle, _ := fakePolicies()
			c.cur, c.spec = idle, nil // stale Chinese policy, pending key
			c.composing, c.marked = true, append(c.marked[:0], "ni"...)
			c.seq = 1
			c.keys[1] = sentKey{seq: 1, keysym: 'x', eaten: true}
			c.sp.on = true
			if known {
				c.sp.push(clCJK, false)
			}
			// Output from the ordinary field is still queued when the
			// same proxy starts referring to a password field.
			c.push(evResult, &ipc.Result{
				Ctx: 1, Seq: 1, Flags: ipc.ResultAutoSpace | ipc.ResultComposing,
				Commit: []byte("中文"), Preedit: []byte("ni"),
				Candidates: []ipc.Candidate{{Text: []byte("你")}},
			})
			c.FocusIn("test", true)
			for _, k := range []uint32{'a', policy.KeyShiftL, policy.KeyControlL, ' '} {
				if c.HandleKey(k, 0) {
					t.Fatal("secure key eaten before the focus reply")
				}
			}
			c.Drain()
			// A late ResultAutoSpace must not re-enable the spacer, and
			// neither the old commit nor mispredict repair may reach it.
			if c.sp.on || c.composing || len(c.marked) > 0 || a.reads != 0 || len(a.log) != 0 || a.cands != 0 {
				t.Fatalf("secure output: spacing %v, composing %v, reads %d, log %v, candidates %d",
					c.sp.on, c.composing, a.reads, a.log, a.cands)
			}
			if !a.status.ASCII || c.stats.Barriers != 0 || c.stats.Asked != 0 || c.stats.Timeouts != 0 || c.stats.Repairs != 0 {
				t.Fatalf("secure state: %+v, %+v", a.status, c.stats)
			}
			c.Select(0)
			c.ChangePage(false)
			if len(cn.out) != 1 || (<-cn.out).typ != ipc.TypeFocusIn {
				t.Fatal("sent password keys or candidate actions to the engine")
			}
		})
	}
}

func TestSecureFocusEncodingAndReconnect(t *testing.T) {
	c := New(Options{}, nopApplier{})
	cn := &conn{out: make(chan outMsg, 16)}
	c.conn.Store(cn)
	check := func(secure bool) {
		t.Helper()
		if len(cn.out) == 0 {
			t.Fatal("missing focus message")
		}
		m := <-cn.out
		r := ipc.NewReader(bytes.NewReader(encode(nil, &m)), 1024)
		typ, payload, err := r.Next()
		if err != nil || typ != ipc.TypeFocusIn {
			t.Fatalf("focus frame: type %v, error %v", typ, err)
		}
		var f ipc.FocusIn
		if err := f.Decode(payload); err != nil {
			t.Fatal(err)
		}
		if f.Ctx != c.ctx || string(f.App) != "test" || (f.Flags&ipc.FocusSecure != 0) != secure {
			t.Fatalf("focus frame: %+v", f)
		}
	}
	c.FocusIn("test", true)
	check(true)
	c.SetSecure(true)
	if len(cn.out) != 0 {
		t.Fatal("unchanged secure state posted another focus")
	}
	c.push(evDisconnected, nil)
	c.Drain()
	c.push(evConnected, nil)
	c.Drain()
	check(true)
	c.SetSecure(false)
	check(false)
	if c.sp.left() != clUnknown || c.sp.on {
		t.Fatal("leaving secure reused spacing state before the reply")
	}
}

func TestSecureFocusRestoresMode(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		h := newHarness(t, realEngine(t), nil)
		if h.rec.status.ASCII != ascii {
			h.c.HandleKey(policy.KeyShiftL, 0)
			h.c.HandleKey(policy.KeyShiftL, policy.ReleaseMask)
			h.until(func() bool { return h.rec.status.ASCII == ascii })
		}
		h.c.SetSecure(true)
		h.until(func() bool { return h.rec.status.ASCII && !h.c.sp.on })
		if h.c.HandleKey('a', 0) {
			t.Fatal("secure key eaten")
		}
		h.c.SetSecure(false)
		h.until(func() bool { return h.rec.status.ASCII == ascii && h.c.sp.on })
		if got := h.c.HandleKey('n', 0); got == ascii {
			t.Fatalf("restored ASCII %v: letter eaten %v", ascii, got)
		}
		h.c.FocusOut()
	}
}

func TestSecureSpacingRestoresAfterReply(t *testing.T) {
	a := &secureRecorder{}
	c := New(Options{}, a)
	c.sp.on = true
	c.SetSecure(true)
	c.applyResult(&ipc.Result{Flags: ipc.ResultAutoSpace, Commit: []byte("中文")})
	c.SetSecure(false)
	if c.sp.on || a.reads != 0 {
		t.Fatal("spacing resumed before the ordinary focus reply")
	}
	c.applyResult(&ipc.Result{Flags: ipc.ResultAutoSpace})
	if !c.HandleKey('B', policy.ShiftMask) || a.reads != 1 || a.commits() != " B" {
		t.Fatalf("spacing did not resume: reads %d, commits %q", a.reads, a.commits())
	}
}

func TestSecureFocusDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	c := New(Options{}, nopApplier{})
	cn := &conn{out: make(chan outMsg, 16)}
	c.conn.Store(cn)
	c.app = "test"
	r := ipc.Result{Ctx: 1, Flags: ipc.ResultAutoSpace, Commit: []byte("中文")}
	buf := make([]byte, 0, 1024)
	run := func() {
		c.SetSecure(true)
		c.SetSecure(true)
		c.HandleKey('a', 0)
		c.push(evResult, &r)
		c.Drain()
		c.SetSecure(false)
		for len(cn.out) > 0 {
			m := <-cn.out
			buf = encode(buf[:0], &m)
		}
	}
	if n := testing.AllocsPerRun(1000, run); n != 0 {
		t.Fatalf("%v allocations per secure transition/key/result", n)
	}
}
