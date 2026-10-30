package main

import "testing"

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
