package client

import (
	"strings"
	"testing"
	"time"

	"shixin/internal/appsim"
	"shixin/internal/ipc"
	"shixin/internal/policy"
)

// simApp is an appsim.App as the client's Applier.
type simApp struct{ *appsim.App }

func (simApp) Status(Status) {}

// simAppReader also implements TextReader, as the macOS front does for auto spacing.
type simAppReader struct{ simApp }

func (a simAppReader) TextBefore() (rune, bool) {
	text := []rune(a.Text())
	if len(text) == 0 {
		return 0, true
	}
	return text[len(text)-1], true
}

// Any printable key the policy says starts composition must establish
// marked text before returning to apps that also insert eaten keys. Uppercase
// and modifier-bearing letters need the same protection as plain lowercase.
func TestFirstCompositionKeyInSimulatedApps(t *testing.T) {
	for _, p := range appsim.Profiles {
		for _, tc := range []struct {
			name string
			key  uint32
			mods uint32
		}{
			{"lowercase", 'n', 0},
			{"uppercase", 'R', 0},
			{"shift-uppercase", 'R', policy.ShiftMask},
			{"shift-lowercase", 'n', policy.ShiftMask},
			{"caps-uppercase", 'R', policy.LockMask},
		} {
			t.Run(p.Name+"/"+tc.name, func(t *testing.T) {
				app := appsim.New(p, appsim.Rect{X: 600, Y: 400, H: 20})
				c := New(Options{}, simApp{app})
				defer c.timer.Stop()
				cn := &conn{out: make(chan outMsg, 8)}
				c.conn.Store(cn)
				idle, composing := fakePolicies()
				idle.Printable[tc.key-0x20] = eatC
				c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
				c.cur, c.spec = idle, idle
				app.Reenter = c.Drain
				if !app.KeyDown(rune(tc.key), func() bool { return c.HandleKey(tc.key, tc.mods) }) {
					t.Fatal("composition key passed")
				}
				if app.Leaked != 0 || app.Text() != "" || app.Marked() != string(rune(tc.key)) {
					t.Fatalf("first key: text=%q marked=%q leaked=%d", app.Text(), app.Marked(), app.Leaked)
				}
				m := <-cn.out
				c.push(evResult, &ipc.Result{Ctx: c.ctx, Seq: m.key.Seq, Policy: 1, Flags: ipc.ResultHandled, Commit: []byte("你好")})
				c.arrived.Store(m.key.Seq)
				c.Drain()
				if app.Text() != "你好" || app.Marked() != "" || app.Leaked != 0 {
					t.Fatalf("commit: text=%q marked=%q leaked=%d", app.Text(), app.Marked(), app.Leaked)
				}
			})
		}
	}
}

func BenchmarkFirstCompositionKey(b *testing.B) {
	for _, key := range []uint32{'n', 'R'} {
		b.Run(string(rune(key)), func(b *testing.B) {
			c := New(Options{}, nopApplier{})
			defer c.timer.Stop()
			cn := &conn{out: make(chan outMsg, 8)}
			c.conn.Store(cn)
			idle, composing := fakePolicies()
			idle.Printable[key-0x20] = eatC
			c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
			var mods uint32
			if key == 'R' {
				mods = policy.ShiftMask
			}
			b.ReportAllocs()
			for b.Loop() {
				c.cur, c.spec = idle, idle
				c.marked = c.marked[:0]
				c.arrived.Store(c.seq)
				if !c.HandleKey(key, mods) {
					b.Fatal("first key passed")
				}
				<-cn.out
			}
		})
	}
}

// Typing pinyin and Chinese punctuation into every simulated app leaves
// exactly the input method's text: no first letter or punctuation typed
// by an app that types eaten keys itself (WeChat 4, Chromium), and no
// commit applied twice when the app's calls run the input method's queued
// Drain (macOS's synchronous IMK calls), with a slow engine so that keys
// wait for the results before them.
func TestTypingInSimulatedApps(t *testing.T) {
	for _, p := range appsim.Profiles {
		for _, reenter := range []bool{false, true} {
			name := p.Name
			if reenter {
				name += "/reentrant"
			}
			t.Run(name, func(t *testing.T) {
				f := startFake(t)
				f.delay = 3 * time.Millisecond
				app := appsim.New(p, appsim.Rect{X: 600, Y: 400, H: 20})
				h := newHarness(t, f.path, simApp{app})
				if reenter {
					app.Reenter = h.c.Drain
				}
				h.c.opt.Budget = time.Second // tests that it waits, not for how long
				for _, r := range ",zhege zhege zhege ," {
					app.KeyDown(r, func() bool { return h.c.HandleKey(uint32(r), 0) })
					app.Turn()
				}
				h.settle()
				if got := app.Text(); got != "，XXX，" || app.Leaked != 0 || app.Marked() != "" {
					t.Errorf("text %q marked %q, %d keys typed by the app", got, app.Marked(), app.Leaked)
				}
			})
		}
	}
}

// A Drain from within the app's calls during a Drain applies nothing
// twice, and the outer Drain still applies what arrived meanwhile.
func TestNestedDrain(t *testing.T) {
	f := startFake(t)
	f.delay = time.Millisecond
	app := appsim.New(appsim.Cocoa, appsim.Rect{X: 600, Y: 400, H: 20})
	h := newHarness(t, f.path, simApp{app})
	nested := 0
	app.Reenter = func() {
		nested++
		time.Sleep(2 * time.Millisecond) // results arrive during the call
		h.c.Drain()
	}
	// This tests reentrancy, not the timeout fallback. The fake engine and
	// app sleeps can exceed the production budget on Windows or a busy runner.
	h.c.opt.Budget = time.Second
	for _, r := range strings.Repeat("ab ", 14) {
		app.KeyDown(r, func() bool { return h.c.HandleKey(uint32(r), 0) })
	}
	h.settle()
	if st := h.c.Stats(); st.Timeouts != 0 {
		t.Fatalf("unexpected timeouts: %+v", st)
	}
	if got := app.Text(); got != strings.Repeat("X", 14) || nested == 0 {
		t.Errorf("text %q after %d nested drains", got, nested)
	}
}

// A PassNotify reply (e.g. Cmd+K, Backspace, Return, Space, digits or Shift
// down, especially with 智能排序 on) arriving while the first composition key's
// synchronous Preedit call runs—or while 智能排序 holds the first key's result—
// must not clear the local first-key mark. Otherwise Chromium/Slack sees an
// eaten key with no marked text and types the letter itself, and auto spacing
// turns the following Chinese commit into "n 你".
func TestPassNotifyReplyDoesNotWipeFirstKeyMark(t *testing.T) {
	notifySame := policy.Act(policy.PassNotify, policy.Same)
	for _, p := range appsim.Profiles {
		for _, tc := range []struct {
			name   string
			keysym uint32
			mods   uint32
			typed  rune
			want   string
		}{
			{"cmd-k", 'k', policy.SuperMask, 0, "你"},
			{"backspace", policy.KeyBackSpace, 0, 0, "你"},
			{"return", policy.KeyReturn, 0, 0, "你"},
			{"space", ' ', 0, ' ', " 你"},
			{"digit", '1', 0, '1', "1 你"},
			{"shift-down", policy.KeyShiftL, 0, 0, "你"},
		} {
			t.Run(p.Name+"/"+tc.name, func(t *testing.T) {
				app := appsim.New(p, appsim.Rect{X: 600, Y: 400, H: 20})
				c := New(Options{}, simAppReader{simApp{app}})
				defer c.timer.Stop()
				cn := &conn{out: make(chan outMsg, 16)}
				c.conn.Store(cn)
				idle, composing := fakePolicies()
				idle.CommandDefault = notifySame
				idle.Printable[' '-0x20] = notifySame
				idle.Printable['1'-0x20] = notifySame
				idle.Special = append(idle.Special,
					policy.Special{Keysym: policy.KeyBackSpace, Action: notifySame},
					policy.Special{Keysym: policy.KeyReturn, Action: notifySame},
				)
				idle.Sort()
				c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
				c.cur, c.spec = idle, idle
				c.sp.on = true
				c.sp.freshBoundary = false // user has already typed in this field

				// Prior PassNotify key leaves an engine reply in flight while
				// keeping the idle Chinese policy active.
				var handled bool
				if tc.typed != 0 {
					handled = app.KeyDown(tc.typed, func() bool { return c.HandleKey(tc.keysym, tc.mods) })
				} else {
					handled = c.HandleKey(tc.keysym, tc.mods)
				}
				if handled {
					t.Fatalf("notify key %s was eaten", tc.name)
				}
				notifyMsg := <-cn.out
				wantBefore := ""
				if tc.typed != 0 {
					wantBefore = string(tc.typed)
				}

				// Inside the synchronous setMarkedText:"n" call, the PassNotify
				// reply arrives and wakes a re-entrant Drain while the engine
				// still holds 'n' (e.g. for 智能排序).
				reentered := false
				app.Reenter = func() {
					if reentered {
						return
					}
					reentered = true
					c.push(evResult, &ipc.Result{
						Ctx:    c.ctx,
						Seq:    notifyMsg.key.Seq,
						Policy: 1,
						Flags:  ipc.ResultAutoSpace,
					})
					c.arrived.Store(notifyMsg.key.Seq)
					c.Drain()
				}
				if !app.KeyDown('n', func() bool { return c.HandleKey('n', 0) }) {
					t.Fatal("first letter passed")
				}
				app.Reenter = c.Drain
				if !reentered {
					t.Fatal("re-entrant Drain did not run during Preedit")
				}
				if app.Leaked != 0 || app.Text() != wantBefore || app.Marked() != "n" {
					t.Fatalf("after first key: text=%q marked=%q leaked=%d", app.Text(), app.Marked(), app.Leaked)
				}
				nMsg := <-cn.out

				// Even a second stale unsequenced/earlier reply while 'n' is
				// still held by 智能排序 must not clear the local mark.
				c.push(evResult, &ipc.Result{Ctx: c.ctx, Seq: 0, Policy: 1, Flags: ipc.ResultAutoSpace})
				c.Drain()
				if app.Marked() != "n" {
					t.Fatalf("stale reply while held cleared mark: %q", app.Marked())
				}

				// Engine finishes 'n', then user types 'i' and ' ' to commit "你".
				c.push(evResult, &ipc.Result{
					Ctx: c.ctx, Seq: nMsg.key.Seq, Policy: 2,
					Flags:      ipc.ResultHandled | ipc.ResultComposing | ipc.ResultAutoSpace,
					Preedit:    []byte("n"),
					Cursor:     1,
					Candidates: []ipc.Candidate{{Text: []byte("你")}},
				})
				c.arrived.Store(nMsg.key.Seq)
				c.Drain()

				if !app.KeyDown('i', func() bool { return c.HandleKey('i', 0) }) {
					t.Fatal("'i' passed")
				}
				iMsg := <-cn.out
				c.push(evResult, &ipc.Result{
					Ctx: c.ctx, Seq: iMsg.key.Seq, Policy: 2,
					Flags:      ipc.ResultHandled | ipc.ResultComposing | ipc.ResultAutoSpace,
					Preedit:    []byte("ni"),
					Cursor:     2,
					Candidates: []ipc.Candidate{{Text: []byte("你")}},
				})
				c.arrived.Store(iMsg.key.Seq)
				c.Drain()

				if !app.KeyDown(' ', func() bool { return c.HandleKey(' ', 0) }) {
					t.Fatal("Space passed")
				}
				spMsg := <-cn.out
				c.push(evResult, &ipc.Result{
					Ctx: c.ctx, Seq: spMsg.key.Seq, Policy: 1,
					Flags:  ipc.ResultHandled | ipc.ResultAutoSpace,
					Commit: []byte("你"),
				})
				c.arrived.Store(spMsg.key.Seq)
				c.Drain()

				if app.Text() != tc.want || app.Marked() != "" || app.Leaked != 0 {
					t.Fatalf("final: text=%q marked=%q leaked=%d, want %q", app.Text(), app.Marked(), app.Leaked, tc.want)
				}
			})
		}
	}
}

// An earlier commit or declined-key repair arriving re-entrantly inside the
// first key's Preedit replaces the app's marked text; the client must restore
// the local mark so the first key still shows and cannot leak.
func TestPriorCommitInsideFirstKeyPreeditRestoresMark(t *testing.T) {
	for _, repair := range []bool{false, true} {
		name := "commit"
		if repair {
			name = "repair"
		}
		t.Run(name, func(t *testing.T) {
			app := appsim.New(appsim.Chromium, appsim.Rect{X: 600, Y: 400, H: 20})
			c := New(Options{}, simApp{app})
			defer c.timer.Stop()
			cn := &conn{out: make(chan outMsg, 16)}
			c.conn.Store(cn)
			idle, composing := fakePolicies()
			c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
			c.cur, c.spec = idle, idle

			// Seed an in-flight earlier eaten key (seq 1).
			c.seq = 1
			c.sent.Store(1)
			c.keys[1] = sentKey{seq: 1, keysym: ',', eaten: true, t: time.Now()}

			reentered := false
			app.Reenter = func() {
				if reentered {
					return
				}
				reentered = true
				r := ipc.Result{Ctx: c.ctx, Seq: 1, Policy: 1}
				if !repair {
					r.Flags = ipc.ResultHandled
					r.Commit = []byte("，")
				}
				c.push(evResult, &r)
				c.arrived.Store(1)
				c.Drain()
			}
			if !app.KeyDown('n', func() bool { return c.HandleKey('n', 0) }) {
				t.Fatal("first key passed")
			}
			wantPrior := "，"
			if repair {
				wantPrior = ","
			}
			if app.Text() != wantPrior || app.Marked() != "n" || app.Leaked != 0 {
				t.Fatalf("after prior %s: text=%q marked=%q leaked=%d", name, app.Text(), app.Marked(), app.Leaked)
			}
			m := <-cn.out
			c.push(evResult, &ipc.Result{
				Ctx: c.ctx, Seq: m.key.Seq, Policy: 2,
				Flags:   ipc.ResultHandled | ipc.ResultComposing,
				Preedit: []byte("n"),
				Cursor:  1,
			})
			c.arrived.Store(m.key.Seq)
			c.Drain()
			if app.Text() != wantPrior || app.Marked() != "n" || app.Leaked != 0 {
				t.Fatalf("after 'n' reply: text=%q marked=%q leaked=%d", app.Text(), app.Marked(), app.Leaked)
			}
		})
	}
}

// A direct-commit key that times out (including under Caps Lock) must keep its
// local mark even if an earlier PassNotify reply drains inside Preedit.
func TestDirectCommitTimeoutMarkProtectedFromStaleReply(t *testing.T) {
	notifySame := policy.Act(policy.PassNotify, policy.Same)
	for _, mods := range []uint32{0, policy.ShiftMask, policy.LockMask} {
		app := appsim.New(appsim.Chromium, appsim.Rect{X: 600, Y: 400, H: 20})
		c := New(Options{Budget: time.Nanosecond}, simApp{app})
		defer c.timer.Stop()
		cn := &conn{out: make(chan outMsg, 16)}
		c.conn.Store(cn)
		idle, composing := fakePolicies()
		idle.CommandDefault = notifySame
		c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
		c.cur, c.spec = idle, idle

		c.HandleKey('k', policy.SuperMask)
		notifyMsg := <-cn.out

		reentered := false
		app.Reenter = func() {
			if reentered {
				return
			}
			reentered = true
			c.push(evResult, &ipc.Result{Ctx: c.ctx, Seq: notifyMsg.key.Seq, Policy: 1})
			c.arrived.Store(notifyMsg.key.Seq)
			c.Drain()
		}
		if !app.KeyDown(',', func() bool { return c.HandleKey(',', mods) }) {
			t.Fatalf("direct commit passed (mods=%#x)", mods)
		}
		if app.Leaked != 0 || app.Text() != "" || app.Marked() != "," {
			t.Fatalf("mods=%#x after timeout: text=%q marked=%q leaked=%d", mods, app.Text(), app.Marked(), app.Leaked)
		}
		m := <-cn.out
		c.push(evResult, &ipc.Result{Ctx: c.ctx, Seq: m.key.Seq, Policy: 1, Flags: ipc.ResultHandled, Commit: []byte("，")})
		c.arrived.Store(m.key.Seq)
		c.Drain()
		if app.Text() != "，" || app.Marked() != "" || app.Leaked != 0 {
			t.Fatalf("mods=%#x after commit: text=%q marked=%q leaked=%d", mods, app.Text(), app.Marked(), app.Leaked)
		}
	}
}

// Combining a local first-key mark with delayed retained keys (e.g. PassNotify,
// then 'n', Space, and 'w' before any reply) must keep every letter across
// stale replies and disconnect.
func TestMarkSeqWithRetainedSuffixAndDisconnect(t *testing.T) {
	notifySame := policy.Act(policy.PassNotify, policy.Same)
	for _, disconnect := range []bool{false, true} {
		name := "replies"
		if disconnect {
			name = "disconnect"
		}
		t.Run(name, func(t *testing.T) {
			app := appsim.New(appsim.Chromium, appsim.Rect{X: 600, Y: 400, H: 20})
			c := New(Options{Budget: time.Nanosecond}, simApp{app})
			defer c.timer.Stop()
			cn := &conn{out: make(chan outMsg, 16)}
			c.conn.Store(cn)
			idle, composing := fakePolicies()
			idle.CommandDefault = notifySame
			c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
			c.cur, c.spec = idle, idle
			app.Reenter = c.Drain

			c.HandleKey('k', policy.SuperMask)
			notifyMsg := <-cn.out
			for _, r := range "n w" {
				if !app.KeyDown(r, func() bool { return c.HandleKey(uint32(r), 0) }) {
					t.Fatalf("key %q passed", r)
				}
			}
			if app.Marked() != "nw" || app.Leaked != 0 {
				t.Fatalf("before replies: marked=%q leaked=%d", app.Marked(), app.Leaked)
			}
			// Stale PassNotify reply arrives first.
			c.push(evResult, &ipc.Result{Ctx: c.ctx, Seq: notifyMsg.key.Seq, Policy: 1})
			c.arrived.Store(notifyMsg.key.Seq)
			if disconnect {
				c.push(evDisconnected, nil)
				c.Drain()
				if app.Text() != "nw" || app.Marked() != "" || app.Leaked != 0 {
					t.Fatalf("disconnect: text=%q marked=%q leaked=%d", app.Text(), app.Marked(), app.Leaked)
				}
				return
			}
			c.Drain()
			if app.Marked() != "nw" {
				t.Fatalf("stale reply altered mark: %q", app.Marked())
			}
			for len(cn.out) > 0 {
				m := <-cn.out
				r := ipc.Result{Ctx: c.ctx, Seq: m.key.Seq, Policy: 1, Flags: ipc.ResultHandled}
				switch m.key.Keysym {
				case 'n':
					r.Policy, r.Flags, r.Preedit = 2, ipc.ResultHandled|ipc.ResultComposing, []byte("n")
				case ' ':
					r.Commit = []byte("你")
				case 'w':
					r.Policy, r.Flags, r.Preedit = 2, ipc.ResultHandled|ipc.ResultComposing, []byte("w")
				}
				c.push(evResult, &r)
				c.arrived.Store(r.Seq)
				c.Drain()
			}
			if app.Text() != "你" || app.Marked() != "w" || app.Leaked != 0 {
				t.Fatalf("final: text=%q marked=%q leaked=%d", app.Text(), app.Marked(), app.Leaked)
			}
		})
	}
}

func TestStalePassNotifyDrainDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	notifySame := policy.Act(policy.PassNotify, policy.Same)
	c := New(Options{}, nopApplier{})
	defer c.timer.Stop()
	cn := &conn{out: make(chan outMsg, 16)}
	c.conn.Store(cn)
	idle, composing := fakePolicies()
	idle.CommandDefault = notifySame
	c.pols.Store(&map[uint16]*policy.Policy{1: idle, 2: composing})
	res := ipc.Result{Ctx: c.ctx, Policy: 1}
	nRes := ipc.Result{Ctx: c.ctx, Policy: 2, Flags: ipc.ResultHandled | ipc.ResultComposing, Preedit: []byte("n"), Cursor: 1}
	run := func() {
		c.cur, c.spec = idle, idle
		c.marked = c.marked[:0]
		c.arrived.Store(c.seq)
		c.HandleKey('k', policy.SuperMask)
		notifySeq := (<-cn.out).key.Seq
		c.HandleKey('n', 0)
		nSeq := (<-cn.out).key.Seq
		res.Seq = notifySeq
		c.push(evResult, &res)
		c.arrived.Store(notifySeq)
		c.Drain()
		nRes.Seq = nSeq
		c.push(evResult, &nRes)
		c.arrived.Store(nSeq)
		c.Drain()
	}
	run()
	if n := testing.AllocsPerRun(100, run); n != 0 {
		t.Fatalf("allocations/cycle: %v", n)
	}
}
