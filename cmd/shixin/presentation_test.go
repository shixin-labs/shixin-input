package main

import (
	"bytes"
	"shixin/internal/ipc"
	"shixin/internal/ui/candview"
	"testing"
)

func TestPresentationOwnership(t *testing.T) {
	var p presentation
	p.init()
	result := ipc.Result{Ctx: 1, Seq: 2, Commit: []byte("private commit"), Preedit: []byte("ni"), Candidates: []ipc.Candidate{{Text: []byte("你")}}}
	caret := ipc.Caret{Ctx: 1, Left: -500, Top: 100, Right: -498, Bottom: 124}
	p.focus(10, 1, false)
	p.result(10, &result)
	if p.visible() {
		t.Fatal("menu appeared before a caret")
	}
	p.position(10, caret)
	if !p.visible() {
		t.Fatal("foreground menu missing")
	}
	var st candview.State
	p.update.apply(&st)
	if st.Items[0].Text != "你" {
		t.Fatal("menu lost")
	}
	if bytes.Contains(p.update.frame, []byte("private commit")) {
		t.Fatal("commit retained")
	}
	p.focus(20, 1, false)
	p.result(10, &result)
	p.position(10, caret)
	p.hide(10)
	if p.visible() || p.secure {
		t.Fatal("old client affected new focus")
	}
	p.result(20, &result)
	p.position(20, caret)
	if !p.visible() {
		t.Fatal("new menu missing")
	}
	stale := result
	stale.Seq = 1
	stale.Candidates = nil
	p.result(20, &stale)
	if !p.visible() {
		t.Fatal("stale result hid menu")
	}
	p.update.apply(&st)
	invalid := caret
	invalid.Right = invalid.Left - 1
	p.position(20, invalid)
	st.Clear()
	p.position(20, caret)
	p.update.apply(&st)
	if len(st.Items) != 1 {
		t.Fatal("caret recovery lost the current menu")
	}
	p.focus(20, 2, true)
	result.Ctx = 2
	caret.Ctx = 2
	p.result(20, &result)
	p.position(20, caret)
	if p.visible() {
		t.Fatal("secure menu shown")
	}
	p.focus(20, 2, false)
	p.result(20, &result)
	caret.Right = caret.Left - 1
	p.position(20, caret)
	if p.visible() {
		t.Fatal("invalid caret accepted")
	}
}
