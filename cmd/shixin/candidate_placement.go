package main

import "math"

// Screen and caret rectangles use Cocoa's global, bottom-left coordinates.
type candidateRect struct{ X, Y, W, H float64 }

type candidateScreen struct{ frame, work candidateRect }

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
	x := min(max(caret.X, work.X), max(work.X, work.X+work.W-width))
	y := caret.Y - gap - height
	if y < work.Y {
		y = caret.Y + max(caret.H, 0) + gap
	}
	y = min(max(y, work.Y), max(work.Y, work.Y+work.H-height))
	return int(math.Floor(x - margin)), int(math.Floor(primaryHeight - y - height - margin))
}
