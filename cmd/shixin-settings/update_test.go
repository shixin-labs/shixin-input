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
