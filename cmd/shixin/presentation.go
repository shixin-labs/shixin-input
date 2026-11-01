package main

import "shixin/internal/ipc"

// presentation owns a single focused menu. Connection IDs are assigned by
// the host, so two applications choosing the same context ID cannot collide.
// FocusIn is the authoritative "this session is focused" signal and is always
// taken: the OS foreground window can lag behind TSF's notification, so the
// foreground check belongs to drawing (visibleIn), not to frame arrival.
// Callers serialize access. Only the latest display is retained; commits are
// never kept in the candidate process.
type presentation struct {
	client             uint64
	pid                uint32
	ctx, seq           uint32
	secure, caretValid bool
	caret              ipc.Caret
	update             candidateUpdate
}

func (p *presentation) init() { p.update.init(); p.secure = true }
func (p *presentation) focus(client uint64, pid, ctx uint32, secure bool) {
	p.client, p.pid, p.ctx, p.seq = client, pid, ctx, 0
	p.secure, p.caretValid = secure, false
	p.update.capture(&ipc.Result{})
}
func (p *presentation) hide(client uint64) {
	if p.client == client {
		p.secure = true
		p.caretValid = false
		p.update.capture(&ipc.Result{})
	}
}

// unfocus hides the menu when the focused context of client loses focus. A
// FocusOut from another session, or for an older context, changes nothing, so
// a late FocusOut from the previous app cannot hide the new one.
func (p *presentation) unfocus(client uint64, ctx uint32) {
	if p.ctx == ctx {
		p.hide(client)
	}
}
func (p *presentation) result(client uint64, r *ipc.Result) {
	if p.client != client || p.ctx != r.Ctx || p.secure || r.Seq < p.seq {
		return
	}
	p.seq = r.Seq
	display := *r
	display.Commit = nil
	p.update.capture(&display)
	if len(r.Candidates) == 0 {
		p.caretValid = false
	}
}
func (p *presentation) position(client uint64, c ipc.Caret) {
	if p.client != client || p.ctx != c.Ctx || p.secure {
		return
	}
	p.caretValid = c.Right >= c.Left && c.Bottom >= c.Top && int64(c.Right)-int64(c.Left) < 10000 && int64(c.Bottom)-int64(c.Top) < 10000
	if p.caretValid {
		p.caret = c
		p.update.dirty = true
	}
}
func (p *presentation) visible() bool { return !p.secure && p.caretValid && p.update.visible }

// visibleIn reports whether the menu may be drawn while foreground is the
// process owning the foreground window. Callers evaluate it at draw time.
func (p *presentation) visibleIn(foreground uint32) bool {
	return p.visible() && p.pid == foreground
}
