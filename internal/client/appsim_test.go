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
