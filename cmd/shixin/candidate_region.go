package main

import "math"

// candidateRegion is a native-pixel region around the drawn card. The
// prewarmed flyout's larger transparent canvas must never receive app clicks.
type candidateRegion struct{ left, top, right, bottom, diameter int32 }

// candidateViewport fits the card plus its shadow inside both the monitor's
// work area and the prewarmed flyout canvas. It never resizes on a key path.
func candidateViewport(workWidth, workHeight int32, dpi uint32) (float32, float32) {
	if dpi == 0 {
		dpi = 96
	}
	scale := float32(dpi) / 96
	return max(1, min(1376, float32(workWidth)/scale-24)), max(1, min(536, float32(workHeight)/scale-24))
}

func candidatePanelRegion(width, height, fontSize float32, dpi uint32) candidateRegion {
	if dpi == 0 {
		dpi = 96
	}
	scale := float64(dpi) / 96
	return candidateRegion{
		left:     int32(math.Floor(12 * scale)),
		top:      int32(math.Floor(12 * scale)),
		right:    int32(math.Ceil((float64(width) + 12) * scale)),
		bottom:   int32(math.Ceil((float64(height) + 12) * scale)),
		diameter: int32(math.Ceil(float64(max(fontSize, 10)) * 1.2 * scale)),
	}
}
