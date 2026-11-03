package main

import (
	"math"
	"testing"

	"shixin/internal/appsim"
)

// simScreens are a laptop's built-in display and a 5K display above it.
var simScreens = []candidateScreen{
	{frame: candidateRect{0, 0, 1512, 982}, work: candidateRect{0, 0, 1512, 950}},
	{frame: candidateRect{-500, 982, 2560, 1440}, work: candidateRect{-500, 982, 2560, 1415}},
}

// compose types a composition into app as the front does: each letter is
// marked inside the key handler, the window's update reads the caret
// after the handler returns, before the app's next turn, and a retry the
// tracker asks for runs on a later turn.
func compose(app *appsim.App, tr *caretTracker, letters string) {
	tr.reset()
	for i := range letters {
		app.KeyDown(rune(letters[i]), func() bool {
			app.Preedit([]byte(letters[:i+1]), i+1)
			return true
		})
		retry := !tr.valid && tr.read(rect(app.Caret()), simScreens)
		for retry {
			app.Turn()
			retry = !tr.valid && tr.read(rect(app.Caret()), simScreens)
		}
		app.Turn()
	}
}

func rect(r appsim.Rect) candidateRect { return candidateRect(r) }

// In every simulated app the candidate window ends up on a screen, and at
// the caret in every app that eventually reports it, however late: one
// letter or several, the first composition or a later one.
func TestCaretInSimulatedApps(t *testing.T) {
	for _, p := range appsim.Profiles {
		for _, letters := range []string{"n", "nihao"} {
			app := appsim.New(p, appsim.Rect{X: 600, Y: 400, H: 20})
			var tr caretTracker
			compose(app, &tr, letters)
			app.Commit([]byte("你好"))
			app.Turn()
			compose(app, &tr, letters) // a second composition, further along
			got, want := tr.caret, rect(app.TrueCaret())
			if !usableCaret(got, simScreens) {
				t.Errorf("%s %q: the window is placed at %+v, off every screen", p.Name, letters, got)
			}
			broken := p.Always != appsim.Laid
			if !broken && (math.Abs(got.X-want.X) > 1 || math.Abs(got.Y-want.Y) > 1) {
				t.Errorf("%s %q: window at %+v, caret at %+v", p.Name, letters, got, want)
			}
			if !broken && !tr.valid {
				t.Errorf("%s %q: the caret was never read", p.Name, letters)
			}
		}
	}
}

// Reading the caret only once per composition, as Shixin 0.2.2 did, places
// the window at 企业微信's zero rectangle: the bug the tracker fixes.
func TestCaretReadOnceFailsInWeCom(t *testing.T) {
	app := appsim.New(appsim.WeCom, appsim.Rect{X: 600, Y: 400, H: 20})
	app.KeyDown('n', func() bool { app.Preedit([]byte("n"), 1); return true })
	if first := rect(app.Caret()); first != (candidateRect{}) {
		t.Fatalf("WeCom answered %+v before its layout", first)
	}
}

// A composition keeps asking a broken app only a few times.
func TestCaretRetriesAreBounded(t *testing.T) {
	app := appsim.New(appsim.BrokenNaN, appsim.Rect{X: 600, Y: 400, H: 20})
	var tr caretTracker
	tr.reset()
	asks := 0
	for tr.read(rect(app.Caret()), simScreens) {
		if asks++; asks > 100 {
			t.Fatal("retries never stop")
		}
	}
	if asks != maxCaretRetries {
		t.Errorf("asked again %d times, want %d", asks, maxCaretRetries)
	}
	if tr.caret != fallbackCaret(simScreens) {
		t.Errorf("window at %+v, want the fallback", tr.caret)
	}
	// Given up: later updates of the composition don't ask again.
	if !tr.valid {
		t.Error("after the retries, every key still queries the app")
	}
}
