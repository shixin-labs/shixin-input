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
		c.ownedFirst, c.ownedLast, c.ownedPrefix = 0, 0, 0
		c.retainKey('n', 0)
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
		c.ownedFirst, c.ownedLast, c.ownedPrefix = 0, 0, 0
		if !c.retainKey('n', 0) {
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
		c.ownedFirst, c.ownedLast, c.ownedPrefix = 0, 0, 0
		c.spec = nil
		c.arrived.Store(c.seq - 1)
		if !c.HandleKey('n', 0) {
			b.Fatal("first key passed")
		}
	}
}

// A queued evConnected runs FocusIn inside HandleKey's initial Drain.
// HandleKey must re-check pending after Drain instead of classifying the
// first letter against Passthrough while the new focus handshake is in flight.
func TestFirstKeyAfterQueuedConnectWaitsForFocusPolicy(t *testing.T) {
	for _, delayed := range []bool{false, true} {
		name := "fast"
		if delayed {
			name = "delayed"
		}
		t.Run(name, func(t *testing.T) {
			app := appsim.New(appsim.Chromium, appsim.Rect{X: 600, Y: 400, H: 20})
			budget := time.Second
			if delayed {
				budget = time.Nanosecond
			}
			c := New(Options{PerAppContexts: true, Budget: budget}, simApp{app})
			defer c.timer.Stop()
			cn := &conn{out: make(chan outMsg, 16)}
			c.conn.Store(cn)
			idle, composing := fakePolicies()
			c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
			c.app = "com.tinyspeck.slackmacgap"
			c.push(evConnected, nil)

			if !delayed {
				go func() {
					for m := range cn.out {
						if m.typ == ipc.TypeKey {
							return
						}
						c.push(evResult, &ipc.Result{Ctx: m.key.Ctx, Seq: m.key.Seq, Policy: 1})
						if m.key.Seq != 0 {
							c.arrived.Store(m.key.Seq)
						}
						c.signalArrived()
					}
				}()
			}

			if !app.KeyDown('n', func() bool { return c.HandleKey('n', 0) }) {
				t.Fatal("first letter after queued evConnected escaped to app")
			}
			if app.Text() != "" || app.Marked() != "n" || app.Leaked != 0 {
				t.Fatalf("text=%q marked=%q leaked=%d", app.Text(), app.Marked(), app.Leaked)
			}
		})
	}
}

// Apps such as Slack/Chromium often call commitComposition: right after
// activation or when clicking into a text box. CommitComposition must not drop
// printable-key ownership when the Clear reply exceeds the budget.
func TestDelayedCommitCompositionRetainsFirstKey(t *testing.T) {
	for _, p := range appsim.Profiles {
		for _, afterFocus := range []bool{false, true} {
			for _, english := range []bool{false, true} {
				name := p.Name
				if afterFocus {
					name += "/after-focus"
				} else {
					name += "/idle"
				}
				if english {
					name += "/English"
				} else {
					name += "/Chinese"
				}
				t.Run(name, func(t *testing.T) {
					app := appsim.New(p, appsim.Rect{X: 600, Y: 400, H: 20})
					c := New(Options{PerAppContexts: true, Budget: time.Nanosecond}, simApp{app})
					defer c.timer.Stop()
					cn := &conn{out: make(chan outMsg, 32)}
					c.conn.Store(cn)
					idle, composing := fakePolicies()
					ascii := &policy.Policy{ID: 3, Notify: policy.NotifyShift}
					c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing, 3: ascii})
					app.Reenter = c.Drain
					if afterFocus {
						c.FocusIn("com.tinyspeck.slackmacgap", false)
					} else {
						c.cur, c.spec = idle, idle
					}
					c.CommitComposition()

					for _, key := range "ni " {
						if !app.KeyDown(key, func() bool { return c.HandleKey(uint32(key), 0) }) {
							t.Fatalf("key %q escaped after delayed CommitComposition", key)
						}
						app.Turn()
					}
					for len(cn.out) > 0 {
						m := <-cn.out
						r := ipc.Result{Ctx: m.key.Ctx, Seq: m.key.Seq, Policy: 1}
						if english {
							r.Policy, r.Flags = 3, ipc.ResultASCII
						} else if m.typ == ipc.TypeKey {
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
						if r.Seq != 0 {
							c.arrived.Store(r.Seq)
						}
						c.Drain()
						app.Turn()
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
}

// Typing the first letter of the next word while Space or candidate click
// (Select) is still pending on the engine must retain that letter instead of
// letting it leak directly onto the screen.
func TestDelayedPostCommitRetainsNextFirstKey(t *testing.T) {
	for _, p := range appsim.Profiles {
		for _, viaSelect := range []bool{false, true} {
			name := p.Name + "/space"
			if viaSelect {
				name = p.Name + "/select"
			}
			t.Run(name, func(t *testing.T) {
				app := appsim.New(p, appsim.Rect{X: 600, Y: 400, H: 20})
				c := New(Options{Budget: time.Nanosecond}, simApp{app})
				defer c.timer.Stop()
				cn := &conn{out: make(chan outMsg, 32)}
				c.conn.Store(cn)
				idle, composing := fakePolicies()
				c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
				c.cur, c.spec = composing, composing
				c.composing = true
				c.marked = append(c.marked[:0], "ni hao"...)
				app.Preedit(c.marked, len(c.marked))
				app.Reenter = c.Drain

				if viaSelect {
					c.Select(0)
				} else if !app.KeyDown(' ', func() bool { return c.HandleKey(' ', 0) }) {
					t.Fatal("space not eaten while composing")
				}
				for _, key := range "wo " {
					if !app.KeyDown(key, func() bool { return c.HandleKey(uint32(key), 0) }) {
						t.Fatalf("key %q escaped while previous commit was pending", key)
					}
					app.Turn()
				}
				for len(cn.out) > 0 {
					m := <-cn.out
					r := ipc.Result{Ctx: m.key.Ctx, Seq: m.key.Seq, Policy: 1}
					switch {
					case m.typ == ipc.TypeSelect || m.typ == ipc.TypeKey && m.key.Keysym == ' ' && m.key.Seq == 1:
						r.Flags, r.Commit = ipc.ResultHandled, []byte("你好")
					case m.typ == ipc.TypeKey && m.key.Keysym == 'w':
						r.Flags, r.Policy, r.Preedit = ipc.ResultHandled|ipc.ResultComposing, 2, []byte("w")
					case m.typ == ipc.TypeKey && m.key.Keysym == 'o':
						r.Flags, r.Policy, r.Preedit = ipc.ResultHandled|ipc.ResultComposing, 2, []byte("wo")
					case m.typ == ipc.TypeKey && m.key.Keysym == ' ':
						r.Flags, r.Commit = ipc.ResultHandled, []byte("我")
					}
					c.push(evResult, &r)
					if r.Seq != 0 {
						c.arrived.Store(r.Seq)
					}
					c.Drain()
					app.Turn()
				}
				if app.Text() != "你好我" || app.Marked() != "" || app.Leaked != 0 {
					t.Fatalf("text=%q marked=%q leaked=%d", app.Text(), app.Marked(), app.Leaked)
				}
			})
		}
	}
}

func TestDelayedPostCommitFinalizeStripsPreeditSpaces(t *testing.T) {
	for _, action := range []string{"focusout", "backspace", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			a := &recorder{}
			c := New(Options{Budget: time.Nanosecond}, a)
			defer c.timer.Stop()
			cn := &conn{out: make(chan outMsg, 16)}
			c.conn.Store(cn)
			idle, composing := fakePolicies()
			c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
			c.cur, c.spec = composing, composing
			c.composing = true
			c.marked = append(c.marked[:0], "ni hao"...)
			if !c.HandleKey(' ', 0) {
				t.Fatal("space not eaten")
			}
			for _, k := range "w o" {
				if !c.HandleKey(uint32(k), 0) {
					t.Fatalf("key %q escaped", k)
				}
			}
			switch action {
			case "focusout":
				c.FocusOut()
			case "backspace":
				if c.HandleKey(policy.KeyBackSpace, 0) {
					t.Fatal("ate Backspace")
				}
			case "disconnect":
				c.push(evDisconnected, nil)
				c.Drain()
			}
			if got := a.commits(); got != "nihaow o" {
				t.Fatalf("finalized %q, want %q", got, "nihaow o")
			}
		})
	}
}

func TestUnavailableDuringUnknownWaitDoesNotRetain(t *testing.T) {
	a := &recorder{}
	c := New(Options{PerAppContexts: true, Budget: time.Nanosecond}, a)
	defer c.timer.Stop()
	cn := &conn{out: make(chan outMsg, 16)}
	c.conn.Store(cn)
	c.FocusIn("com.tinyspeck.slackmacgap", false)
	c.push(evUnavailable, nil)
	if c.HandleKey('n', 0) || c.ownedFirst != 0 || a.preedit != "" {
		t.Fatalf("retained key while unavailable: owned=%d preedit=%q", c.ownedFirst, a.preedit)
	}
}
