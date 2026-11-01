package main

import (
	"bytes"
	"shixin/internal/ipc"
	"shixin/internal/ui/candview"
	"testing"
	"time"
)

func TestPresentationOwnership(t *testing.T) {
	var p presentation
	p.init()
	result := ipc.Result{Ctx: 1, Seq: 2, Commit: []byte("private commit"), Preedit: []byte("ni"), Candidates: []ipc.Candidate{{Text: []byte("你")}}}
	caret := ipc.Caret{Ctx: 1, Left: -500, Top: 100, Right: -498, Bottom: 124}
	p.focus(10, 10, 1, false)
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
	p.focus(20, 20, 1, false)
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
	p.focus(20, 20, 2, true)
	result.Ctx = 2
	caret.Ctx = 2
	p.result(20, &result)
	p.position(20, caret)
	if p.visible() {
		t.Fatal("secure menu shown")
	}
	p.focus(20, 20, 2, false)
	p.result(20, &result)
	caret.Right = caret.Left - 1
	p.position(20, caret)
	if p.visible() {
		t.Fatal("invalid caret accepted")
	}
}

func TestPresentationModeHint(t *testing.T) {
	var p presentation
	p.init()
	now := time.Unix(100, 0)
	caret := ipc.Caret{Ctx: 1, Left: 10, Top: 20, Right: 12, Bottom: 40}
	p.focus(1, 10, 1, false)
	r := ipc.Result{Ctx: 1}
	p.result(1, &r)
	p.position(1, caret)
	if p.modeVisibleIn(10, now) {
		t.Fatal("initial mode flashed")
	}
	r.Flags = ipc.ResultASCII
	p.result(1, &r)
	if p.modeVisibleIn(10, now) {
		t.Fatal("mode used a stale caret")
	}
	p.position(1, caret)
	if !p.modeVisibleIn(10, now) {
		t.Fatal("confirmed English mode missing")
	}
	deadline := p.mode.until
	p.result(1, &r)
	if !p.modeVisibleIn(10, now.Add(time.Millisecond)) || p.mode.until != deadline {
		t.Fatal("ordinary result restarted or hid the hint")
	}
	if p.modeVisibleIn(10, deadline) {
		t.Fatal("mode did not expire")
	}
	r.Flags = 0
	p.result(1, &r)
	p.position(1, caret)
	if p.modeVisibleIn(20, now) || !p.mode.pending || !p.modeVisibleIn(10, now) {
		t.Fatal("foreground catch-up lost the mode")
	}
	r.Candidates = []ipc.Candidate{{Text: []byte("你")}}
	p.result(1, &r)
	if p.modeVisibleIn(10, now) {
		t.Fatal("mode covered candidates")
	}
	r.Candidates = nil
	r.Flags = ipc.ResultASCII
	p.result(1, &r)
	p.position(1, caret)
	if !p.modeVisibleIn(10, now) {
		t.Fatal("toggle missing")
	}
	invalid := caret
	invalid.Right = invalid.Left - 1
	p.position(1, invalid)
	if p.modeVisibleIn(10, now) || p.mode.pending {
		t.Fatal("caret movement failed to dismiss")
	}
	p.focus(1, 10, 1, true)
	p.result(1, &r)
	p.position(1, caret)
	if p.modeVisibleIn(10, now) {
		t.Fatal("secure hint appeared")
	}
}

func TestPresentationModeDoesNotAllocate(t *testing.T) {
	var p presentation
	p.init()
	now := time.Unix(100, 0)
	r := ipc.Result{Ctx: 1}
	caret := ipc.Caret{Ctx: 1, Left: 10, Top: 20, Right: 12, Bottom: 40}
	if n := testing.AllocsPerRun(1000, func() {
		p.focus(1, 10, 1, false)
		r.Flags = 0
		p.result(1, &r)
		p.position(1, caret)
		r.Flags = ipc.ResultASCII
		p.result(1, &r)
		p.position(1, caret)
		p.modeVisibleIn(10, now)
		p.hide(1)
	}); n != 0 {
		t.Fatalf("mode presentation allocates %v", n)
	}
}

func TestPresentationForeground(t *testing.T) {
	result := func(ctx, seq uint32) *ipc.Result {
		return &ipc.Result{Ctx: ctx, Seq: seq, Candidates: []ipc.Candidate{{Text: []byte("你")}}}
	}
	caret := func(ctx uint32) ipc.Caret { return ipc.Caret{Ctx: ctx, Left: 10, Top: 10, Right: 12, Bottom: 30} }
	const a, b, other = 100, 200, 300 // process IDs of apps A and B and a third foreground app
	tests := []struct {
		name  string
		steps func(p *presentation)
		// foreground to evaluate at draw time, and whether the menu shows
		fg   uint32
		want bool
	}{
		{"focus in before foreground catches up", func(p *presentation) {
			p.focus(2, b, 1, false) // foreground is still A
			p.result(2, result(1, 1))
			p.position(2, caret(1))
		}, b, true},
		{"not drawn until foreground catches up", func(p *presentation) {
			p.focus(2, b, 1, false)
			p.result(2, result(1, 1))
			p.position(2, caret(1))
		}, a, false},
		{"result from a non-focused session is dropped", func(p *presentation) {
			p.focus(1, a, 1, false)
			p.result(2, result(1, 1))
			p.position(2, caret(1))
		}, b, false},
		{"result from a non-focused foreground session is dropped", func(p *presentation) {
			p.focus(1, a, 1, false)
			p.result(2, result(1, 1))
			p.position(2, caret(1))
		}, a, false},
		{"result before any focus is dropped", func(p *presentation) {
			p.result(2, result(1, 1))
			p.position(2, caret(1))
		}, b, false},
		{"focus moves between sessions", func(p *presentation) {
			p.focus(1, a, 1, false)
			p.result(1, result(1, 1))
			p.position(1, caret(1))
			p.focus(2, b, 1, false) // same context ID in another app
			p.unfocus(1, 1)         // late FocusOut from the old session
			p.result(1, result(1, 2))
			p.result(2, result(1, 1))
			p.position(2, caret(1))
		}, b, true},
		{"old session is not drawn after focus moves", func(p *presentation) {
			p.focus(1, a, 1, false)
			p.result(1, result(1, 1))
			p.position(1, caret(1))
			p.focus(2, b, 1, false)
		}, a, false},
		{"focus out hides", func(p *presentation) {
			p.focus(1, a, 1, false)
			p.result(1, result(1, 1))
			p.position(1, caret(1))
			p.unfocus(1, 1)
		}, a, false},
		{"focus out of another context keeps the menu", func(p *presentation) {
			p.focus(1, a, 2, false)
			p.result(1, result(2, 1))
			p.position(1, caret(2))
			p.unfocus(1, 1)
		}, a, true},
		{"other foreground app", func(p *presentation) {
			p.focus(1, a, 1, false)
			p.result(1, result(1, 1))
			p.position(1, caret(1))
		}, other, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p presentation
			p.init()
			tt.steps(&p)
			if got := p.visibleIn(tt.fg); got != tt.want {
				t.Fatalf("visibleIn(%d) = %v, want %v", tt.fg, got, tt.want)
			}
		})
	}
	// The foreground catching up later shows the menu without new frames.
	var p presentation
	p.init()
	p.focus(2, b, 1, false)
	p.result(2, result(1, 1))
	p.position(2, caret(1))
	if p.visibleIn(a) || !p.visibleIn(b) {
		t.Fatal("foreground catch-up not honored at draw time")
	}
}
