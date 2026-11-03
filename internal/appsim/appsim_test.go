package appsim

import (
	"math"
	"testing"
)

var origin = Rect{X: 400, Y: 300, H: 20}

// Each profile reports the caret it documents, before and after layout.
func TestCaretAnswers(t *testing.T) {
	for _, tt := range []struct {
		p             Profile
		before, after func(a *App, r Rect) bool
	}{
		{Cocoa, isTrue, isTrue},
		{WeCom, isZero, isTrue},
		{QtStale, isOrigin, isTrue},
		{Chromium, isOrigin, isTrue},
		{BrokenNaN, isNaN, isNaN},
		{BrokenOffscreen, isFar, isFar},
	} {
		a := New(tt.p, origin)
		a.Commit([]byte("你好"))
		a.Turn() // lay out the commit; the next text is the test
		if tt.p.LayoutTurns > 0 {
			a.laid = origin // compare a stale answer with a known caret
		}
		a.Preedit([]byte("ni"), 2)
		if r := a.Caret(); !tt.before(a, r) {
			t.Errorf("%s: before layout %+v", tt.p.Name, r)
		}
		a.Turn()
		if r := a.Caret(); !tt.after(a, r) {
			t.Errorf("%s: after layout %+v", tt.p.Name, r)
		}
	}
}

func isTrue(a *App, r Rect) bool   { return r == a.TrueCaret() && r.X == 428 }
func isZero(_ *App, r Rect) bool   { return r == Rect{} }
func isOrigin(_ *App, r Rect) bool { return r == origin }
func isNaN(_ *App, r Rect) bool    { return math.IsNaN(r.X) && math.IsNaN(r.Y) }
func isFar(_ *App, r Rect) bool    { return r.X < -1e5 && r.Y < -1e5 }

// An app that types eaten keys does so only without text from the input
// method; every app types a key the input method passed.
func TestKeyDown(t *testing.T) {
	for _, p := range Profiles {
		a := New(p, origin)
		a.KeyDown('a', func() bool { return false })
		a.KeyDown(',', func() bool { return true }) // eaten, no text in time
		a.KeyDown('b', func() bool { a.Preedit([]byte("b"), 1); return true })
		a.KeyDown('c', func() bool { return true }) // eaten, marked text exists
		want, leaked := "a", 0
		if p.TypesEatenKey {
			want, leaked = "a,", 1
		}
		if a.Text() != want || a.Marked() != "b" || a.Leaked != leaked {
			t.Errorf("%s: text %q marked %q leaked %d", p.Name, a.Text(), a.Marked(), a.Leaked)
		}
	}
}
