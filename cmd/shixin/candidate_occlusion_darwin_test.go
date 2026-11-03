package main

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"

	"shixin/internal/settings"
)

// Every frame that draws candidates asks macOS for the window's occlusion
// state; one without candidates, or without a window, asks nothing. Shown
// over some apps (企业微信, ChatGPT), the window otherwise kept reporting
// itself occluded and MyGo never put the panel on screen.
func TestCandidateFramesRefreshOcclusion(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	if screenRect == nil {
		initScreenRect()
	}
	saved := cand
	savedHandle, savedQuery := flyoutHandle, queryOcclusion
	t.Cleanup(func() { cand, flyoutHandle, queryOcclusion = saved, savedHandle, savedQuery })
	s := settings.Default()
	cand.look = lookOf(&s)
	cand.screens = make([]candidateScreen, 0, 8)
	cand.pending.init()
	cand.shown = false // never move a window here
	handle := uintptr(42)
	var asked []uintptr
	flyoutHandle = func() uintptr { return handle }
	queryOcclusion = func(w uintptr) { asked = append(asked, w) }

	tt := ui.NewTester(candView, 600, 200)
	tt.Frame()
	if len(asked) != 0 {
		t.Fatalf("a frame without candidates asked %d times", len(asked))
	}
	results := candidateResults()
	for i := range results {
		cand.pending.capture(&results[i])
		tt.Frame()
		if len(asked) != i+1 || asked[i] != handle {
			t.Fatalf("after frame %d with candidates: asked %v", i+1, asked)
		}
	}
	handle = 0 // no native window yet
	cand.pending.capture(&results[0])
	tt.Frame()
	if len(asked) != len(results) {
		t.Fatalf("asked without a window: %v", asked)
	}
}
