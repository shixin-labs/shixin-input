package main

import (
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shixin/internal/settings"
	"shixin/internal/ui/candview"
)

func TestCandidateHandoffDoesNotAllocate(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	oldPending, oldWindow, oldArgs, oldQueued := cand.pending, cand.flyout, cand.args, cand.queued
	oldCaret := cand.caret
	callback := purego.NewCallback(func(uintptr) uintptr { return 0 })
	cand.pending.init()
	// A zero Window enables the queue branch without creating any GUI. The
	// no-op callback never accesses it or the focused app.
	cand.flyout = &mygo.Window{}
	cand.args = [3]uintptr{mainQueue, 0, callback}
	defer func() {
		cand.pending, cand.flyout, cand.args, cand.queued = oldPending, oldWindow, oldArgs, oldQueued
		cand.caret = oldCaret
	}()
	results := candidateResults()
	if n := testing.AllocsPerRun(100, func() {
		for i := range results {
			cand.queued = false
			showCandidates(&results[i])
		}
		hideCandidates()
	}); n != 0 {
		t.Fatalf("copying/queueing candidate changes allocates %v times", n)
	}
	// A commit/hide may be superseded by a new composition before the UI
	// callback. The next show must still query the new caret, not reuse the
	// previous composition's anchor.
	cand.caret.valid = true
	hideCandidates()
	showCandidates(&results[0])
	if cand.caret.valid || !cand.pending.visible {
		t.Fatal("coalescing a hide lost the caret invalidation")
	}
}

// candView must unmask only when shown with candidates at the target canvas
// size, and must queue an update rather than resizing mid-frame when bounds
// differ from cand.wanted.
func TestCandViewUnmasksOnlyAtTargetSizeAndNeverResizesMidFrame(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	if screenRect == nil {
		initScreenRect()
	}
	saved := cand
	savedMask := maskCandidateSurface
	t.Cleanup(func() {
		cand = saved
		maskCandidateSurface = savedMask
	})
	s := settings.Default()
	cand.look = lookOf(&s)
	cand.screens = make([]candidateScreen, 0, 8)
	cand.pending.init()
	cand.flyout = &mygo.Window{}
	cand.args = [3]uintptr{mainQueue, 0, purego.NewCallback(func(uintptr) uintptr { return 0 })}
	cand.shown = true
	cand.caret = caretTracker{caret: candidateRect{X: 100, Y: 500, W: 1, H: 18}, valid: true, known: true}

	var unmasked int
	maskCandidateSurface = func(_ *mygo.Window, masked bool) {
		if !masked {
			unmasked++
		}
	}

	results := candidateResults()
	cand.pending.capture(&results[0])

	// First frame runs at an old/mismatched viewport size (360×90 while target
	// is 1400×560): must stay masked and not draw or unmask into a stretched frame.
	cand.drawnGen = 0
	cand.wanted = candidateBounds{X: 88, Y: 100, W: 1400, H: 560}
	cand.bounds = cand.wanted
	cand.mask.prepareShowOrResize(false, candidateBounds{W: 360, H: 90}, cand.wanted)
	ttSmall := ui.NewTester(candView, 360, 90)
	_ = ttSmall
	if unmasked != 0 || !cand.mask.masked || cand.drawnGen != 0 {
		t.Fatalf("drew or unmasked on mismatched 360×90 frame: unmasked=%d masked=%v drawnGen=%d", unmasked, cand.mask.masked, cand.drawnGen)
	}

	// Next frame at the matching viewport size (and matching cand.wanted): must unmask and draw.
	refreshCandidateScreens()
	style := candStyle(false)
	w, h := candview.MeasureWith(&cand.state, &style, cand.defaultFont, cand.locale)
	b, _, ok := candidateWindowBounds(cand.caret.caret, cand.screens, float64(w), float64(h), candWindowW, candWindowH, shadowPad)
	if !ok {
		t.Skip("no NSScreen available in this test environment")
	}
	cand.wanted = b
	cand.bounds = b
	cand.queued = false
	cand.mask.prepareShowOrResize(false, candidateBounds{W: 360, H: 90}, b)
	ttMatch := ui.NewTester(candView, b.W, b.H)
	_ = ttMatch
	if unmasked != 1 || cand.mask.masked || cand.drawnGen != 1 {
		t.Fatalf("did not draw/unmask on matching %dx%d frame: unmasked=%d masked=%v drawnGen=%d", b.W, b.H, unmasked, cand.mask.masked, cand.drawnGen)
	}
	if cand.queued {
		t.Fatal("queued redundant update when bounds already matched cand.wanted")
	}

	// If cand.wanted differs from the bounds needed by the drawn candidates
	// (e.g. after a settings/display change), candView must defer repositioning
	// to updateCandidateWindow rather than resizing mid-frame.
	cand.wanted = candidateBounds{X: b.X, Y: b.Y, W: 360, H: 90}
	cand.queued = false
	cand.mask.prepareShowOrResize(true, cand.wanted, b)
	ttMatch.Frame()
	if !cand.queued {
		t.Fatal("did not queue an update when bounds differed from cand.wanted")
	}
	if unmasked != 1 || !cand.mask.masked || cand.drawnGen != 1 {
		t.Fatalf("drew or unmasked while canvas size differed from cand.wanted: unmasked=%d masked=%v drawnGen=%d", unmasked, cand.mask.masked, cand.drawnGen)
	}
}
