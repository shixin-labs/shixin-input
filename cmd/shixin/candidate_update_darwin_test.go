package main

import (
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

func TestCandidateHandoffDoesNotAllocate(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	oldPending, oldWindow, oldArgs, oldQueued := cand.pending, cand.flyout, cand.args, cand.queued
	oldCaretValid := cand.caretValid
	callback := purego.NewCallback(func(uintptr) uintptr { return 0 })
	cand.pending.init()
	// A zero Window enables the queue branch without creating any GUI. The
	// no-op callback never accesses it or the focused app.
	cand.flyout = &mygo.Window{}
	cand.args = [3]uintptr{mainQueue, 0, callback}
	defer func() {
		cand.pending, cand.flyout, cand.args, cand.queued = oldPending, oldWindow, oldArgs, oldQueued
		cand.caretValid = oldCaretValid
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
	cand.caretValid = true
	hideCandidates()
	showCandidates(&results[0])
	if cand.caretValid || !cand.pending.visible {
		t.Fatal("coalescing a hide lost the caret invalidation")
	}
}
