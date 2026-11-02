package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"

	shixupdate "shixin/internal/update"
)

// Checking is the user's click, never automatic.
func TestCheckUpdateOnlyOnClick(t *testing.T) {
	saved := plat
	plat.manualPreview = false
	t.Cleanup(func() { plat = saved })
	a, _, _ := testApp(t)
	var calls atomic.Int32
	release := make(chan struct{})
	a.checkRelease = func(ctx context.Context, current string) (*shixupdate.Release, error) {
		calls.Add(1)
		<-release
		return nil, errors.New("offline")
	}
	tt := ui.NewTester(a.view, 880, 900)
	if err := tt.Click(T("s.page.about")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if calls.Load() != 0 || a.upd.phase != updIdle {
		t.Fatalf("checked before a click: %d calls, phase %d", calls.Load(), a.upd.phase)
	}
	if err := tt.Click(T("s.u.check")); err != nil {
		t.Fatal(err)
	}
	if a.upd.phase != updChecking {
		t.Errorf("phase %d after the click", a.upd.phase)
	}
	close(release)
}

func TestUnsignedPreviewManualUpdates(t *testing.T) {
	saved, oldVersion := plat, version
	plat.manualPreview, plat.selfUpdate = true, false
	version = "0.2.0"
	t.Cleanup(func() { plat, version = saved, oldVersion })
	a, _, _ := testApp(t)
	a.page = "about"
	a.front = "installed-front"
	a.upd = updateState{phase: updAvailable, rel: &shixupdate.Release{Version: "0.2.1", Newer: true}}
	if a.canInstall() {
		t.Fatal("unsigned preview can install an update")
	}
	tt := ui.NewTester(a.view, 880, 900)
	if _, ok := tt.Find(T("s.u.download")); !ok {
		t.Fatal("manual release link missing")
	}
	if _, ok := tt.Find(T("s.u.install")); ok {
		t.Fatal("unsigned preview offers in-app installation")
	}
	if _, ok := tt.Find(T("s.u.check")); ok {
		t.Fatal("preview queries a stable-only update feed")
	}
}

func TestCheckUpdateCapturesRequestInputs(t *testing.T) {
	oldVersion := version
	version = "0.1.0"
	t.Cleanup(func() { version = oldVersion })
	a, _, _ := testApp(t)
	called, release := make(chan string, 1), make(chan struct{})
	defer close(release)
	a.checkRelease = func(_ context.Context, stringVersion string) (*shixupdate.Release, error) {
		called <- stringVersion
		<-release
		return nil, errors.New("offline")
	}
	a.checkUpdate()
	// Request inputs are owned by the main thread. The worker must use its
	// snapshot even if they change before it starts doing network work.
	version = "0.2.0"
	a.checkRelease = func(context.Context, string) (*shixupdate.Release, error) {
		called <- "replacement provider"
		return nil, errors.New("unexpected provider")
	}
	if got := <-called; got != "0.1.0" {
		t.Fatalf("worker used %q instead of its request snapshot", got)
	}
}

func TestFinishCheck(t *testing.T) {
	a, _, _ := testApp(t)
	newer := &shixupdate.Release{Version: "9.9.9", Newer: true, Notes: "Fixed things"}
	a.finishCheck(newer, nil)
	if a.upd.phase != updAvailable || a.upd.rel != newer {
		t.Errorf("newer: phase %d", a.upd.phase)
	}
	if a.canInstall() && version == "dev" {
		t.Error("a development build may install")
	}
	a.finishCheck(&shixupdate.Release{Version: "0.0.1"}, nil)
	if a.upd.phase != updUpToDate {
		t.Errorf("same: phase %d", a.upd.phase)
	}
	a.finishCheck(nil, errors.New("offline"))
	if a.upd.phase != updFailed || a.upd.err != "offline" {
		t.Errorf("error: phase %d %q", a.upd.phase, a.upd.err)
	}
}
