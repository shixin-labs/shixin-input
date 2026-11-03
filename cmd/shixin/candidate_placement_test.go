package main

import (
	"math"
	"testing"
)

func TestCandidateCardBounds(t *testing.T) {
	for _, tt := range []struct {
		work candidateRect
		w, h float32
	}{
		{candidateRect{W: 1920, H: 1080}, 1376, 536},
		{candidateRect{W: 800, H: 480}, 800, 480},
		{candidateRect{W: 1000, H: 768}, 1000, 536},
	} {
		w, h := candidateCardBounds(tt.work, 1400, 560, 12)
		if w != tt.w || h != tt.h {
			t.Fatalf("work %+v: viewport %v×%v, want %v×%v", tt.work, w, h, tt.w, tt.h)
		}
		x, y := candidatePosition(candidateRect{X: tt.work.W - 1, Y: 1}, tt.work, float64(w), float64(h), tt.work.H, 12)
		if float64(x)+12 < 0 || float64(x)+12+float64(w) > tt.work.W || float64(y)+12 < 0 || float64(y)+12+float64(h) > tt.work.H {
			t.Fatalf("bounded card lies outside work area: %d,%d %v×%v", x, y, w, h)
		}
	}
}

func TestCandidatePosition(t *testing.T) {
	work := candidateRect{0, 40, 1000, 740}
	for _, tt := range []struct {
		name          string
		caret, work   candidateRect
		width, height float64
		x, y          int
	}{
		{"below", candidateRect{100, 400, 0, 20}, work, 300, 60, 88, 392},
		{"right", candidateRect{980, 400, 0, 20}, work, 300, 60, 688, 392},
		{"bottom", candidateRect{100, 45, 0, 20}, work, 300, 60, 88, 659},
		{"both", candidateRect{980, 45, 0, 20}, work, 300, 60, 688, 659},
		{"left display", candidateRect{-10, 150, 0, 24}, candidateRect{-1200, 140, 1200, 836}, 300, 100, -312, 510},
		{"above display", candidateRect{1080, 830, 0, 18}, candidateRect{100, 820, 1000, 650}, 250, 80, 838, -144},
		{"zero caret height", candidateRect{100, 45, 0, 0}, work, 300, 60, 88, 679},
		{"vertical", candidateRect{980, 120, 0, 20}, work, 180, 400, 808, 244},
		{"no room either side", candidateRect{100, 400, 0, 20}, work, 300, 700, 88, 8},
		{"oversized", candidateRect{980, 45, 0, 20}, work, 1100, 800, -12, -52},
	} {
		t.Run(tt.name, func(t *testing.T) {
			x, y := candidatePosition(tt.caret, tt.work, tt.width, tt.height, 800, 12)
			if x != tt.x || y != tt.y {
				t.Fatalf("position (%d, %d), want (%d, %d)", x, y, tt.x, tt.y)
			}
		})
	}
}

func TestCandidateScreenAt(t *testing.T) {
	screens := []candidateScreen{
		{frame: candidateRect{0, 0, 1000, 800}},
		{frame: candidateRect{-1200, 100, 1200, 900}},
		{frame: candidateRect{100, 800, 1000, 700}},
	}
	for _, tt := range []struct {
		name  string
		caret candidateRect
		want  int
	}{
		{"main", candidateRect{990, 20, 0, 0}, 0},
		{"left", candidateRect{-10, 150, 0, 24}, 1},
		{"above", candidateRect{1080, 830, 0, 18}, 2},
		{"gap", candidateRect{-10, 50, 0, 0}, 0},
		{"boundary", candidateRect{100, 800, 0, 0}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := candidateScreenAt(tt.caret, screens); got != tt.want {
				t.Fatalf("screen %d, want %d", got, tt.want)
			}
		})
	}
}

func TestModeBesideBadge(t *testing.T) {
	work := candidateRect{0, 0, 1000, 800}
	caret := candidateRect{500, 400, 1, 20}
	r := modeBesideBadge(caret, work, 26)
	if r.X != 520 || r.Y != caret.Y || r.H != caret.H {
		t.Fatalf("beside the badge: %+v", r)
	}
	// At the right edge the bubble moves to the badge's left, still clear of it.
	caret.X = 980
	if r := modeBesideBadge(caret, work, 26); r.X != 980-20-26 {
		t.Fatalf("at the right edge: %+v", r)
	}
}

func TestUsableCaret(t *testing.T) {
	screens := []candidateScreen{
		{frame: candidateRect{0, 0, 1512, 982}, work: candidateRect{0, 0, 1512, 950}},
		{frame: candidateRect{1512, -400, 2560, 1440}, work: candidateRect{1512, -400, 2560, 1415}},
	}
	nan, inf := math.NaN(), math.Inf(1)
	for _, tt := range []struct {
		name  string
		caret candidateRect
		want  bool
	}{
		{"caret", candidateRect{300, 500, 0, 18}, true},
		{"second display", candidateRect{2500, 200, 1, 20}, true},
		{"origin with a line", candidateRect{0, 0, 0, 18}, true},
		{"just past an edge", candidateRect{-20, 500, 0, 18}, true},
		{"zero rect", candidateRect{}, false},
		{"NaN", candidateRect{nan, 500, 0, 18}, false},
		{"NaN height", candidateRect{300, 500, 0, nan}, false},
		{"infinite", candidateRect{inf, 500, 0, 18}, false},
		{"off every screen", candidateRect{-5000, 500, 0, 18}, false},
		{"far below", candidateRect{300, -3000, 0, 18}, false},
	} {
		if got := usableCaret(tt.caret, screens); got != tt.want {
			t.Errorf("%s: usable %v, want %v", tt.name, got, tt.want)
		}
	}
	if !usableCaret(candidateRect{300, 500, 0, 18}, nil) || usableCaret(candidateRect{}, nil) {
		t.Error("without screens, only the zero rect is unusable")
	}
	if c := fallbackCaret(screens); !usableCaret(c, screens) || c.X != 756 {
		t.Errorf("fallback %+v is not on the main screen", c)
	}
}

// A NaN caret is kept at the work area's corner: converting NaN to an int
// is not defined, and on amd64 puts the window off every screen.
func TestCandidatePositionNaN(t *testing.T) {
	work := candidateRect{0, 40, 1000, 740}
	nan := math.NaN()
	for _, tt := range []struct {
		caret candidateRect
		x, y  int
	}{
		{candidateRect{nan, 400, 0, 20}, -12, 372},
		{candidateRect{100, nan, 0, 20}, 88, -8},
		{candidateRect{nan, nan, nan, nan}, -12, -8},
	} {
		if x, y := candidatePosition(tt.caret, work, 300, 60, 780, 12); x != tt.x || y != tt.y {
			t.Errorf("caret %+v: position %d,%d, want %d,%d", tt.caret, x, y, tt.x, tt.y)
		}
	}
}

// The canvas never reaches past its screen's right or bottom edge, so a
// second display can't take the window; the panel always fits in it.
func TestCandidateCanvas(t *testing.T) {
	// A 1800×1169 built-in display, and a 2560×1440 display to its right.
	builtIn := candidateScreen{frame: candidateRect{0, 0, 1800, 1169}, work: candidateRect{57, 0, 1743, 1130}}
	const primary = 1169
	for _, tt := range []struct {
		name string
		x, y int
		w, h int
	}{
		{"room for all", 100, 100, 1400, 560},
		{"right edge", 686, 300, 1114, 560}, // Slack, caret x 698: it spanned into the 5K display
		{"bottom", 300, 1082, 1400, 87},     // near the bottom: only 87 points left
		{"both", 897, 1056, 903, 113},       // the failing Slack composition at 14:14
		{"at the edge", 1799, 1168, 1, 1},   // never zero
	} {
		w, h := candidateCanvas(tt.x, tt.y, builtIn, primary, 1400, 560)
		if w != tt.w || h != tt.h {
			t.Errorf("%s: canvas %dx%d, want %dx%d", tt.name, w, h, tt.w, tt.h)
		}
	}
	// The panel placed by candidatePosition always fits the canvas.
	for _, caret := range []candidateRect{{698, 79, 1, 18}, {1700, 79, 1, 18}, {909, 105, 1, 18}, {919, 294, 1, 19}, {1790, 1100, 0, 18}} {
		for _, size := range [][2]float64{{232, 60}, {600, 60}, {180, 400}} {
			x, y := candidatePosition(caret, builtIn.work, size[0], size[1], primary, 12)
			w, h := candidateCanvas(x, y, builtIn, primary, 1400, 560)
			if float64(w) < size[0]+12 || float64(h) < size[1]+12 {
				t.Errorf("caret %+v panel %v: canvas %dx%d too small", caret, size, w, h)
			}
		}
	}
}

// owner is the screen macOS shows a window on when displays have separate
// Spaces: the one holding most of it. win is in top-left global points,
// screens in Cocoa's bottom-left coordinates about the primary screen.
func owner(win candidateRect, screens []candidateScreen, primaryHeight float64) int {
	best, area := -1, 0.0
	for i, s := range screens {
		top := primaryHeight - (s.frame.Y + s.frame.H) // the screen in top-left coordinates
		w := min(win.X+win.W, s.frame.X+s.frame.W) - max(win.X, s.frame.X)
		h := min(win.Y+win.H, top+s.frame.H) - max(win.Y, top)
		if w > 0 && h > 0 && w*h > area {
			best, area = i, w*h
		}
	}
	return best
}

// With a second display anywhere around the first, the candidate window
// placed at any caret belongs to the caret's display, and the panel lies
// on it: a window spanning two displays shows only on the one holding most
// of it, and a full-size canvas there hid the panel.
func TestCandidateWindowStaysOnCaretDisplay(t *testing.T) {
	const primary = 1169
	builtIn := candidateScreen{frame: candidateRect{0, 0, 1800, 1169}, work: candidateRect{57, 0, 1743, 1130}}
	second := func(x, y float64) candidateScreen {
		return candidateScreen{frame: candidateRect{x, y, 2560, 1440}, work: candidateRect{x, y, 2560, 1415}}
	}
	arrangements := map[string][]candidateScreen{
		"right, offset (the report)": {builtIn, second(1800, -271)},
		"left":                       {builtIn, second(-2560, 0)},
		"above":                      {builtIn, second(-380, 1169)},
		"below":                      {builtIn, second(-380, -1440)},
	}
	// The macOS flyout's constants (candidates_darwin.go), here so that
	// this platform-free test builds everywhere.
	const pad, canvasW, canvasH = 12, 1400, 560
	panels := [][2]float64{{232, 60}, {600, 60}, {180, 400}}
	for name, screens := range arrangements {
		for si, s := range screens {
			for fx := 0.0; fx <= 1; fx += 0.05 {
				for fy := 0.0; fy <= 1; fy += 0.05 {
					caret := candidateRect{X: s.work.X + fx*(s.work.W-1), Y: s.work.Y + fy*(s.work.H-20), W: 1, H: 18}
					if candidateScreenAt(caret, screens) != si {
						continue
					}
					for _, p := range panels {
						x, y := candidatePosition(caret, s.work, p[0], p[1], primary, pad)
						w, h := candidateCanvas(x, y, s, primary, canvasW, canvasH)
						win := candidateRect{float64(x), float64(y), float64(w), float64(h)}
						if got := owner(win, screens, primary); got != si {
							t.Fatalf("%s: caret %+v panel %v: window %+v shown on screen %d, caret on %d", name, caret, p, win, got, si)
						}
						panel := candidateRect{win.X + pad, win.Y + pad, p[0], p[1]}
						if panel.X+panel.W > win.X+win.W || panel.Y+panel.H > win.Y+win.H || owner(panel, screens, primary) != si {
							t.Fatalf("%s: caret %+v: panel %+v outside its window %+v or screen", name, caret, panel, win)
						}
					}
				}
			}
		}
	}

	// The reported failure: the old fixed canvas belonged to the other display.
	screens := arrangements["right, offset (the report)"]
	x, y := candidatePosition(candidateRect{698, 79, 1, 18}, builtIn.work, 232, 60, primary, pad)
	if owner(candidateRect{float64(x), float64(y), canvasW, canvasH}, screens, primary) != 1 {
		t.Fatal("the model no longer shows the reported failure of the fixed-size canvas")
	}
}
