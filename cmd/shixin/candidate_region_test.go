package main

import "testing"

func TestCandidateViewport(t *testing.T) {
	for _, dpi := range []uint32{96, 144, 192} {
		w, h := candidateViewport(800, 600, dpi)
		scale := float32(dpi) / 96
		if (w+24)*scale > 800.01 || (h+24)*scale > 600.01 || w > 1376 || h > 536 {
			t.Fatalf("dpi %d: viewport %v x %v exceeds screen or warm canvas", dpi, w, h)
		}
	}
	w, h := candidateViewport(8000, 4000, 96)
	if w != 1376 || h != 536 {
		t.Fatal("viewport grew beyond the warmed window")
	}
}

func TestCandidateInputRegion(t *testing.T) {
	for _, dpi := range []uint32{96, 144, 192} {
		r := candidatePanelRegion(300, 80, 20, dpi)
		scale := int32(dpi / 48)
		if r.left != 6*scale || r.top != 6*scale || r.right != 156*scale || r.bottom != 46*scale || r.diameter != 12*scale {
			t.Fatalf("dpi %d: %+v", dpi, r)
		}
		// The password field below an 80-DIP card is outside the input region,
		// even though it is still inside the prewarmed 560-DIP flyout canvas.
		below := int32(120 * dpi / 96)
		if below < r.bottom {
			t.Fatalf("dpi %d: card intercepts app below it", dpi)
		}
	}
	if candidatePanelRegion(300, 80, 20, 0) != candidatePanelRegion(300, 80, 20, 96) {
		t.Fatal("missing DPI must use 96")
	}
	r := candidatePanelRegion(300.25, 80.25, 17, 144)
	if r.right != 469 || r.bottom != 139 {
		t.Fatalf("fractional panel clipped: %+v", r)
	}
}
