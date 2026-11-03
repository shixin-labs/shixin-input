// Package appsim simulates the text fields of real apps as an input method
// sees them, for tests that can't run those apps (WeChat and WeCom need an
// account): when an app lays out marked text, what caret rectangle it
// reports before and after, and whether it types a key the input method
// ate. Each Profile records the behavior of an app Shixin met; see
// Profiles.
//
// An App is driven like the front drives a real one: KeyDown wraps the
// input method's key handler, Preedit and Commit are what the client
// applies (it implements client.Applier but for Status), Caret is the
// answer to attributesForCharacterIndex:lineHeightRectangle: (macOS) or
// the caret query of TSF, Turn runs one turn of the app's event loop, and
// Reenter runs the input method's queued work inside the app's calls.
package appsim

import (
	"math"

	"shixin/internal/ipc"
)

// Rect is a rectangle in global screen coordinates, as the app reports it.
type Rect struct{ X, Y, W, H float64 }

// Answer is what an app reports for the caret.
type Answer int

const (
	// Laid is the caret of the text as last laid out: correct once the
	// app has laid out its latest text.
	Laid Answer = iota
	// Zero is the zero rectangle, as apps answer with no caret yet.
	Zero
	// Stale is the caret before the text changed: the previous layout's.
	Stale
	// NaN is a rectangle of NaNs.
	NaN
	// Offscreen is a rectangle far from every screen.
	Offscreen
)

// Profile is how one kind of app behaves.
type Profile struct {
	Name string
	// LayoutTurns is how many turns of its event loop the app takes to
	// lay out text after it changes: 0 lays it out at once, as Cocoa text
	// views do; Qt posts the input method's text as an event.
	LayoutTurns int
	// BeforeLayout is the caret the app reports while its latest text
	// isn't laid out.
	BeforeLayout Answer
	// Always, when not Laid, is the caret the app always reports.
	Always Answer
	// TypesEatenKey types a key the input method handled as the app's own
	// keystroke when, as the key handler returns, there is no marked text
	// and the key inserted none (WeChat 4, Chromium).
	TypesEatenKey bool
}

// Profiles are the apps Shixin met, by the behavior that broke it.
var (
	// Cocoa is a Cocoa text view (TextEdit, Notes): everything at once.
	Cocoa = Profile{Name: "Cocoa"}
	// WeCom is 企业微信 5 (Qt): it lays out marked text on its next turn
	// and reports the zero rectangle until then.
	WeCom = Profile{Name: "WeCom", LayoutTurns: 1, BeforeLayout: Zero}
	// QtStale is a Qt app reporting the previous caret until layout.
	QtStale = Profile{Name: "QtStale", LayoutTurns: 1, BeforeLayout: Stale}
	// WeChat4 is WeChat 4, which types an eaten key without marked text.
	WeChat4 = Profile{Name: "WeChat4", TypesEatenKey: true}
	// Chromium is a Chromium or Electron app (Codex): it types an eaten
	// key without text and lays out on its next turn.
	Chromium = Profile{Name: "Chromium", LayoutTurns: 1, BeforeLayout: Stale, TypesEatenKey: true}
	// BrokenNaN always reports NaNs.
	BrokenNaN = Profile{Name: "BrokenNaN", Always: NaN}
	// BrokenOffscreen always reports a caret off every screen.
	BrokenOffscreen = Profile{Name: "BrokenOffscreen", Always: Offscreen}

	Profiles = []Profile{Cocoa, WeCom, QtStale, WeChat4, Chromium, BrokenNaN, BrokenOffscreen}
)

// App is one text field of a simulated app. Not safe for concurrent use:
// like a real app's, it belongs to the main thread.
type App struct {
	Profile
	// Origin is the caret before any text: its line's bottom-left corner
	// and height. Each character moves it CharWidth to the right.
	Origin    Rect
	CharWidth float64

	text, marked []rune
	changed      bool // text or marked text changed during this key
	pending      int  // turns until the text is laid out; 0 when laid out
	laid         Rect // the caret of the last layout

	// Reenter, when set, runs inside each Commit and Preedit, after the
	// text changed: on macOS these are synchronous calls into the app,
	// which can run the main run loop, and the input method's queued work
	// with it, before they return.
	Reenter func()

	// Shown is the number of candidates the input method showed last,
	// and Leaked counts eaten keys the app typed itself.
	Shown  int
	Leaked int
}

// New returns an empty text field of p whose caret starts at origin.
func New(p Profile, origin Rect) *App {
	a := &App{Profile: p, Origin: origin, CharWidth: 14}
	a.laid = a.TrueCaret()
	return a
}

// KeyDown delivers key r: handle is the input method's key handler, and
// reports whether it handled the key. A key it doesn't handle is typed;
// one it handles may be typed anyway (TypesEatenKey).
func (a *App) KeyDown(r rune, handle func() bool) bool {
	a.changed = false
	handled := handle()
	switch {
	case !handled:
		a.typeKey(r)
	case a.TypesEatenKey && !a.changed && len(a.marked) == 0:
		a.typeKey(r)
		a.Leaked++
	}
	return handled
}

func (a *App) typeKey(r rune) {
	a.text = append(a.text, r)
	a.touch()
}

// Turn runs one turn of the app's event loop: it may lay out its text.
func (a *App) Turn() {
	if a.pending > 0 {
		if a.pending--; a.pending == 0 {
			a.laid = a.TrueCaret()
		}
	}
}

func (a *App) touch() {
	a.changed = true
	a.pending = a.LayoutTurns
	if a.pending == 0 {
		a.laid = a.TrueCaret()
	}
}

// Candidates, Commit and Preedit are client.Applier's methods.

// Candidates records how many candidates the input method shows.
func (a *App) Candidates(r *ipc.Result) { a.Shown = len(r.Candidates) }

// Commit inserts text, replacing the marked text.
func (a *App) Commit(text []byte) {
	a.text = append(a.text, []rune(string(text))...)
	a.marked = a.marked[:0]
	a.touch()
	a.reenter()
}

// Preedit sets the marked text; empty text clears it.
func (a *App) Preedit(text []byte, cursor int) {
	a.marked = append(a.marked[:0], []rune(string(text))...)
	a.touch()
	a.reenter()
}

func (a *App) reenter() {
	if a.Reenter != nil {
		a.Reenter()
	}
}

// Caret is the app's answer to the input method's caret query: the start
// of the marked text, as the app currently reports it.
func (a *App) Caret() Rect {
	answer := a.Always
	if answer == Laid && a.pending > 0 {
		answer = a.BeforeLayout
	}
	switch answer {
	case Zero:
		return Rect{}
	case NaN:
		n := math.NaN()
		return Rect{n, n, n, n}
	case Offscreen:
		return Rect{X: -1e6, Y: -1e6, H: a.Origin.H}
	}
	// Laid, and Stale: until layout, laid is the previous text's caret.
	return a.laid
}

// TrueCaret is where the start of the marked text really is.
func (a *App) TrueCaret() Rect {
	return Rect{X: a.Origin.X + a.CharWidth*float64(len(a.text)), Y: a.Origin.Y, H: a.Origin.H}
}

// Text is the field's committed text, and Marked its marked text.
func (a *App) Text() string   { return string(a.text) }
func (a *App) Marked() string { return string(a.marked) }
