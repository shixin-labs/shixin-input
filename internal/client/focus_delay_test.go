package client

import (
	"shixin/internal/appsim"
	"shixin/internal/ipc"
	"shixin/internal/policy"
	"testing"
	"time"
)

func delayedFocus(t testing.TB) (*Client, *recorder, *conn) {
	t.Helper()
	a := &recorder{}
	c := New(Options{PerAppContexts: true, Budget: time.Nanosecond}, a)
	t.Cleanup(func() { c.timer.Stop() })
	cn := &conn{out: make(chan outMsg, 256)}
	c.conn.Store(cn)
	idle, composing := fakePolicies()
	c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
	c.FocusIn("new.app", false)
	return c, a, cn
}

func TestDelayedFocusPrintableOwnership(t *testing.T) {
	for _, english := range []bool{false, true} {
		t.Run(map[bool]string{false: "Chinese", true: "English"}[english], func(t *testing.T) {
			c, a, cn := delayedFocus(t)
			for _, key := range "ni" {
				if !c.HandleKey(uint32(key), 0) {
					t.Fatal("printable passed")
				}
			}
			if a.preedit != "ni" {
				t.Fatalf("marked %q", a.preedit)
			}
			// Focus acknowledgements must not erase the retained spelling.
			for len(cn.out) > 0 {
				m := <-cn.out
				r := ipc.Result{Ctx: c.ctx, Seq: m.key.Seq, Policy: 1}
				if m.typ == ipc.TypeKey && !english {
					r.Flags = ipc.ResultHandled | ipc.ResultComposing
					r.Preedit = []byte("n")
					if m.key.Keysym == 'i' {
						r.Preedit = []byte("ni")
					}
				}
				c.push(evResult, &r)
				if r.Seq != 0 {
					c.arrived.Store(r.Seq)
				}
				c.Drain()
				if m.typ != ipc.TypeKey && a.preedit != "ni" {
					t.Fatalf("focus erased retained input: %q", a.preedit)
				}
				if m.typ == ipc.TypeKey && m.key.Keysym == 'n' {
					want := "ni"
					if english {
						want = "i"
					}
					if a.preedit != want {
						t.Fatalf("partial reply marked %q want %q", a.preedit, want)
					}
				}
			}
			if english && (a.commits() != "ni" || a.preedit != "") {
				t.Fatalf("English commit=%q mark=%q", a.commits(), a.preedit)
			}
			if !english && (a.commits() != "" || a.preedit != "ni") {
				t.Fatalf("Chinese commit=%q mark=%q", a.commits(), a.preedit)
			}
		})
	}
}

func TestDelayedFocusCapsLockOwnership(t *testing.T) {
	for _, mods := range []uint32{policy.LockMask, policy.LockMask | policy.ShiftMask} {
		c, a, cn := delayedFocus(t)
		if !c.HandleKey('R', mods) || a.preedit != "R" {
			t.Fatalf("Caps Lock key escaped delayed focus: mods=%x marked=%q", mods, a.preedit)
		}
		var key outMsg
		for len(cn.out) > 0 {
			m := <-cn.out
			if m.typ == ipc.TypeKey {
				key = m
			}
		}
		if key.key.Keysym != 'R' || key.key.Mods != mods || key.key.Flags&ipc.KeyNotify != 0 {
			t.Fatalf("retained key lost its original modifiers: %+v", key)
		}
	}
}

func TestDelayedFocusFinalize(t *testing.T) {
	for _, action := range []string{"focusout", "disconnect", "backspace", "shortcut"} {
		t.Run(action, func(t *testing.T) {
			c, a, _ := delayedFocus(t)
			for _, k := range "n i" {
				if !c.HandleKey(uint32(k), 0) {
					t.Fatal("passed printable")
				}
			}
			switch action {
			case "focusout":
				c.FocusOut()
			case "disconnect":
				c.push(evDisconnected, nil)
				c.Drain()
			case "backspace":
				if c.HandleKey(policy.KeyBackSpace, 0) {
					t.Fatal("ate Backspace")
				}
			case "shortcut":
				if c.HandleKey('v', policy.SuperMask) {
					t.Fatal("ate shortcut")
				}
			}
			if a.commits() != "n i" {
				t.Fatalf("finalized %q", a.commits())
			}
			c.push(evResult, &ipc.Result{Ctx: c.ctx, Seq: 1, Commit: []byte("old")})
			if action != "disconnect" {
				c.Drain()
				if a.commits() != "n i" {
					t.Fatal("late result duplicated text")
				}
			}
		})
	}
}

func TestDelayedFocusPartialReplyDisconnect(t *testing.T) {
	c, a, cn := delayedFocus(t)
	c.HandleKey('n', 0)
	c.HandleKey('i', 0)
	for len(cn.out) > 0 {
		m := <-cn.out
		if m.key.Keysym == 'i' {
			break
		}
		c.push(evResult, &ipc.Result{Ctx: c.ctx, Seq: m.key.Seq, Policy: 1})
	}
	c.push(evDisconnected, nil)
	c.Drain()
	if a.commits() != "ni" {
		t.Fatalf("duplicated partial reply on disconnect: %q", a.commits())
	}
}

func TestDelayedFocusSecureAndShortcut(t *testing.T) {
	c, _, _ := delayedFocus(t)
	if c.HandleKey('c', policy.SuperMask) {
		t.Fatal("ate shortcut")
	}
	c.secure = true
	if c.HandleKey('n', 0) || c.ownedFirst != 0 {
		t.Fatal("retained secure input")
	}
}

func BenchmarkDelayedFocusRetain(b *testing.B) {
	c, _, cn := delayedFocus(b)
	c.a = nopApplier{}
	b.ReportAllocs()
	for b.Loop() {
		for len(cn.out) > 0 {
			<-cn.out
		}
		c.marked = c.marked[:0]
		c.ownedFirst, c.ownedLast = 0, 0
		c.retainFocusKey('n', 0)
	}
}

func TestDelayedFocusRetainDoesNotAllocate(t *testing.T) {
	c, _, cn := delayedFocus(t)
	c.a = nopApplier{}
	if n := testing.AllocsPerRun(100, func() {
		for len(cn.out) > 0 {
			<-cn.out
		}
		c.marked = c.marked[:0]
		c.ownedFirst, c.ownedLast = 0, 0
		if !c.retainFocusKey('n', 0) {
			panic("not retained")
		}
	}); n != 0 {
		t.Fatalf("allocations/key: %v", n)
	}
}

func TestDelayedFocusSimulatedApps(t *testing.T) {
	for _, p := range appsim.Profiles {
		t.Run(p.Name, func(t *testing.T) {
			c, _, cn := delayedFocus(t)
			app := appsim.New(p, appsim.Rect{X: 600, Y: 400, H: 20})
			c.a = simApp{app}
			app.Reenter = c.Drain
			for _, key := range "ni " {
				app.KeyDown(key, func() bool { return c.HandleKey(uint32(key), 0) })
				app.Turn()
			}
			for len(cn.out) > 0 {
				m := <-cn.out
				r := ipc.Result{Ctx: c.ctx, Seq: m.key.Seq, Policy: 1}
				if m.typ == ipc.TypeKey {
					r.Flags = ipc.ResultHandled | ipc.ResultComposing
					switch m.key.Keysym {
					case 'n':
						r.Preedit = []byte("n")
					case 'i':
						r.Preedit = []byte("ni")
					case ' ':
						r.Flags = ipc.ResultHandled
						r.Commit = []byte("你")
					}
				}
				c.push(evResult, &r)
				if r.Seq != 0 {
					c.arrived.Store(r.Seq)
				}
				c.Drain()
				app.Turn()
			}
			if app.Text() != "你" || app.Marked() != "" || app.Leaked != 0 {
				t.Fatalf("text=%q marked=%q leaks=%d", app.Text(), app.Marked(), app.Leaked)
			}
		})
	}
}

func BenchmarkDelayedFocusKey(b *testing.B) {
	c, _, cn := delayedFocus(b)
	c.a = nopApplier{}
	b.ReportAllocs()
	for b.Loop() {
		for len(cn.out) > 0 {
			<-cn.out
		}
		c.marked = c.marked[:0]
		c.ownedFirst, c.ownedLast = 0, 0
		c.focusWait = c.seq
		c.spec = nil
		c.arrived.Store(c.seq - 1)
		if !c.HandleKey('n', 0) {
			b.Fatal("first key passed")
		}
	}
}
