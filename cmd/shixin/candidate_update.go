package main

import (
	"shixin/internal/ipc"
	"shixin/internal/ui/candview"
)

// candidateUpdate owns the latest display data after Drain releases its IPC
// slot. Capture only copies bytes into a startup buffer. Strings are interned
// by the UI frame, so intermediate results never build or allocate a view.
type candidateUpdate struct {
	frame   []byte
	result  ipc.Result
	dirty   bool
	visible bool
}

func (u *candidateUpdate) init() {
	// MaxFrame includes the type byte, but not the four-byte length prefix.
	u.frame = make([]byte, 0, ipc.MaxFrame+4)
	u.result.Candidates = make([]ipc.Candidate, 0, 10)
}

func (u *candidateUpdate) capture(r *ipc.Result) {
	u.dirty, u.visible = true, len(r.Candidates) != 0
	u.frame = u.frame[:0]
	if !u.visible {
		return
	}
	// Use the existing codec without retaining commits or irrelevant key
	// metadata. This cannot exceed the size of the received frame.
	display := ipc.Result{Preedit: r.Preedit, Highlighted: r.Highlighted, Candidates: r.Candidates}
	u.frame = display.Append(u.frame)
}

func (u *candidateUpdate) apply(st *candview.State) {
	if !u.dirty {
		return
	}
	u.dirty = false
	if !u.visible {
		st.Clear()
		return
	}
	if err := u.result.Decode(u.frame[5:]); err != nil {
		// Only our own encoder writes this buffer. Discard an invalid display
		// rather than showing stale candidates or touching the app's text.
		u.visible = false
		st.Clear()
		return
	}
	st.Apply(&u.result)
}
