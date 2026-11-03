package main

import "math"

// Screen and caret rectangles use Cocoa's global, bottom-left coordinates.
type candidateRect struct{ X, Y, W, H float64 }

type candidateScreen struct{ frame, work candidateRect }

// usableCaret reports whether an app's caret rectangle can place a window:
// finite, not the zero rectangle apps answer when they have no caret yet,
// and on or near a screen. Qt apps such as 企业微信 can answer before they
// have laid out the marked text.
func usableCaret(caret candidateRect, screens []candidateScreen) bool {
	for _, v := range [...]float64{caret.X, caret.Y, caret.W, caret.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	if caret == (candidateRect{}) {
		return false
	}
	if len(screens) == 0 {
		return true
	}
	const slack = 64 // a caret just past an edge, as in a gap between displays
	for _, s := range screens {
		r := s.frame
		if caret.X >= r.X-slack && caret.X <= r.X+r.W+slack && caret.Y >= r.Y-slack && caret.Y <= r.Y+r.H+slack {
			return true
		}
	}
	return false
}

// fallbackCaret is where a window goes when no usable caret is known: the
// middle of the lower part of the main screen's work area.
func fallbackCaret(screens []candidateScreen) candidateRect {
	if len(screens) == 0 {
		return candidateRect{}
	}
	w := screens[0].work
	return candidateRect{X: w.X + w.W/2, Y: w.Y + w.H/3}
}

// candidateCardBounds bounds the card by both the screen work area and the
// pre-created flyout's content area. The transparent shadow margin is outside
// the card, so only the flyout limit needs to reserve it.
func candidateCardBounds(work candidateRect, windowWidth, windowHeight, margin float64) (float32, float32) {
	return float32(max(1, min(work.W, windowWidth-2*margin))),
		float32(max(1, min(work.H, windowHeight-2*margin)))
}

// candidateScreenAt chooses the screen containing the caret, or the nearest
// screen if the app reports a caret in a gap between displays.
func candidateScreenAt(caret candidateRect, screens []candidateScreen) int {
	x, y := caret.X, caret.Y+max(caret.H, 0)/2
	best, distance := 0, math.Inf(1)
	for i, s := range screens {
		r := s.frame
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return i
		}
		dx := max(r.X-x, 0, x-r.X-r.W)
		dy := max(r.Y-y, 0, y-r.Y-r.H)
		if d := dx*dx + dy*dy; d < distance {
			best, distance = i, d
		}
	}
	return best
}

// candidatePosition clamps the visible panel, leaving its transparent window
// margin outside the work area. Prefer below the caret, then above its line.
// MyGo's global top-left coordinates are flipped about the primary screen.
func candidatePosition(caret, work candidateRect, width, height, primaryHeight, margin float64) (int, int) {
	const gap = 4
	// NaN survives min and max, and converting it to an int puts the
	// window off every screen: keep such a caret at the work area's corner.
	if math.IsNaN(caret.X) {
		caret.X = work.X
	}
	if math.IsNaN(caret.Y) {
		caret.Y = work.Y + work.H
	}
	if math.IsNaN(caret.H) {
		caret.H = 0
	}
	x := min(max(caret.X, work.X), max(work.X, work.X+work.W-width))
	y := caret.Y - gap - height
	if y < work.Y {
		y = caret.Y + max(caret.H, 0) + gap
	}
	y = min(max(y, work.Y), max(work.Y, work.Y+work.H-height))
	return int(math.Floor(x - margin)), int(math.Floor(primaryHeight - y - height - margin))
}

// candidateCanvas sizes the transparent flyout placed with its top-left
// corner at (x, y), in MyGo's top-left global points, to end at screen s's
// right and bottom edges, at most maxW×maxH. With displays that have
// separate Spaces, macOS shows a window spanning two displays only on the
// one holding most of it: a full-size canvas near the bottom right of one
// display could belong to its neighbour, and the panel in its top-left
// corner then never showed. The panel, kept inside s's work area, always
// fits the canvas.
func candidateCanvas(x, y int, s candidateScreen, primaryHeight float64, maxW, maxH int) (int, int) {
	right := s.frame.X + s.frame.W
	bottom := primaryHeight - s.frame.Y // in top-left coordinates
	w := min(maxW, int(math.Floor(right-float64(x))))
	h := min(maxH, int(math.Floor(bottom-float64(y))))
	return max(w, 1), max(h, 1)
}

// modeBesideBadge moves the caret so the mode bubble clears macOS's own
// input source badge, which is centered under the caret after a switch:
// to its right, or to its left at the work area's right edge.
func modeBesideBadge(caret, work candidateRect, width float64) candidateRect {
	const clearance = 20 // half the badge plus a gap
	r := caret
	r.X = caret.X + clearance
	if r.X+width > work.X+work.W {
		r.X = caret.X - clearance - width
	}
	return r
}
