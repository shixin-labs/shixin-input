package client

import (
	"testing"
	"time"

	"shixin/internal/appsim"
	"shixin/internal/ipc"
	"shixin/internal/policy"
)

// Keep every reply queued until after the next spelling has been typed.
// Memory access is serial here: the failure is stale mode prediction, not
// concurrent access. Exercise both directions so English stays literal.
func TestDelayedModeReplyOwnsFollowingSpelling(t *testing.T) {
	for _, profile := range appsim.Profiles {
		for _, english := range []bool{false, true} {
			name := profile.Name + "/Chinese"
			if english {
				name = profile.Name + "/English"
			}
			t.Run(name, func(t *testing.T) {
				app := appsim.New(profile, appsim.Rect{X: 600, Y: 400, H: 20})
				c := New(Options{Budget: time.Nanosecond}, simApp{app})
				defer c.timer.Stop()
				cn := &conn{out: make(chan outMsg, 16)}
				c.conn.Store(cn)
				idle, composing := fakePolicies()
				ascii := &policy.Policy{ID: 3, Notify: policy.NotifyShift}
				c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing, 3: ascii})
				c.cur, c.spec = ascii, ascii
				if english {
					c.cur, c.spec = idle, idle
				}
				app.Reenter = c.Drain
				c.HandleKey(policy.KeyShiftL, 0)
				c.HandleKey(policy.KeyShiftL, policy.ReleaseMask)
				for _, key := range "ni " {
					if !app.KeyDown(key, func() bool { return c.HandleKey(uint32(key), 0) }) {
						t.Fatalf("key %q escaped while mode reply was pending", key)
					}
				}
				for len(cn.out) > 0 {
					m := <-cn.out
					r := ipc.Result{Ctx: c.ctx, Seq: m.key.Seq, Policy: 1}
					if english {
						r.Policy, r.Flags = 3, ipc.ResultASCII
					} else {
						switch m.key.Keysym {
						case 'n':
							r.Flags, r.Preedit = ipc.ResultHandled|ipc.ResultComposing, []byte("n")
						case 'i':
							r.Flags, r.Preedit = ipc.ResultHandled|ipc.ResultComposing, []byte("ni")
						case ' ':
							r.Flags, r.Commit = ipc.ResultHandled, []byte("你")
						}
					}
					c.push(evResult, &r)
					c.arrived.Store(r.Seq)
					c.Drain()
				}
				want := "你"
				if english {
					want = "ni "
				}
				if app.Text() != want || app.Marked() != "" || app.Leaked != 0 {
					t.Fatalf("text=%q marked=%q leaked=%d, want %q", app.Text(), app.Marked(), app.Leaked, want)
				}
			})
		}
	}
}

func delayedModeKeyCycle(t testing.TB) func() {
	t.Helper()
	c := New(Options{Budget: time.Nanosecond}, nopApplier{})
	t.Cleanup(func() { c.timer.Stop() })
	cn := &conn{out: make(chan outMsg, 8)}
	c.conn.Store(cn)
	ascii := &policy.Policy{ID: 3, Notify: policy.NotifyShift}
	return func() {
		c.cur, c.spec = ascii, ascii
		c.marked = c.marked[:0]
		c.focusWait, c.ownedFirst, c.ownedLast = 0, 0, 0
		c.arrived.Store(c.seq)
		c.HandleKey(policy.KeyShiftL, policy.ReleaseMask)
		if !c.HandleKey('n', 0) {
			panic("first key passed")
		}
		for len(cn.out) > 0 {
			<-cn.out
		}
	}
}

func TestDelayedModeKeyDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	run := delayedModeKeyCycle(t)
	if n := testing.AllocsPerRun(100, run); n != 0 {
		t.Fatalf("allocations/cycle: %v", n)
	}
}

func BenchmarkDelayedModeFirstKey(b *testing.B) {
	run := delayedModeKeyCycle(b)
	b.ReportAllocs()
	for b.Loop() {
		run()
	}
}

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
			h.settle()
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
