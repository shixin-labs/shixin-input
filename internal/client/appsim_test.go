package client

import (
	"strings"
	"testing"
	"time"

	"shixin/internal/appsim"
)

// simApp is an appsim.App as the client's Applier.
type simApp struct{ *appsim.App }

func (simApp) Status(Status) {}

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
	h.c.opt.Budget = 8 * time.Millisecond
	for _, r := range strings.Repeat("ab ", 14) {
		app.KeyDown(r, func() bool { return h.c.HandleKey(uint32(r), 0) })
	}
	h.settle()
	if got := app.Text(); got != strings.Repeat("X", 14) || nested == 0 {
		t.Errorf("text %q after %d nested drains", got, nested)
	}
}
