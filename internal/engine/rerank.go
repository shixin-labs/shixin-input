package engine

import (
	"strconv"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/perf"
	"shixin/internal/policy"
	"shixin/internal/rerank"
	"shixin/internal/rime"
)

// 智能排序 in the engine (DESIGN §7.2). Each session gets a rerank slot that
// librime-shixin's shixin_rerank filter exports its candidates into. When
// a key produces a new export, the engine asks the scorer for an order and
// holds the key's result. If the order comes back within the timeout, it
// is imported into the slot, the composition is refreshed so the filter
// applies it, and the result goes out reordered. Otherwise the result goes
// out in rime's order and the late order is dropped. Either way the
// client gets one result per key, and the menu never changes once shown.
// Committed text is kept per app for the scorer.
//
// The engine thread never waits: a held result is parked while other
// requests are served, and a timer releases it. Nothing here allocates
// per key.

// rerankSlots is how many sessions can rerank at once; others don't.
const rerankSlots = 256

// A scorer that misses the timeout slowMisses times in a row is too slow
// for this machine (an old GPU, a VM): results are then sent at once, and
// only one key in slowProbe is held, to notice when it is fast again.
const (
	slowMisses = 16
	slowProbe  = 32
)

// DefaultRerankTimeout is how long a key's result waits for its order: one
// 60 Hz frame. End to end, orders came back in p50 9.2 ms, p99 15.9 ms
// (DESIGN §7.2).
const DefaultRerankTimeout = 16 * time.Millisecond

// RerankStats count what happened to orders (性能 page).
type RerankStats struct {
	Requests   uint64 // exports sent to the scorer
	Applied    uint64 // results sent with the scorer's order
	Timeouts   uint64 // results sent in rime's order: no order in time
	Superseded uint64 // results sent in rime's order: a newer request came first
	Dropped    uint64 // orders that came after their result was sent
	Rejected   uint64 // orders librime refused as invalid
	// Wait is how long held results waited, for an order or the timeout.
	Wait perf.Hist
	// Slow: orders came too late here, so results aren't held (probes
	// excepted).
	Slow bool
}

// answer is an order from the scorer, passed by value.
type answer struct {
	session   int32
	exportSeq uint32
	n         uint8
	perm      [rerank.MaxOptions]uint8
}

// sessionRerank is the engine's state for one slot.
type sessionRerank struct {
	sess   *rime.Session
	conn   *Conn // nil while the session is in the pool
	ctx    uint32
	secure bool // focus is in a secure field: never enabled
	// asked is the last export sent to the scorer, or skipped.
	asked uint32
	// held: the result of key seq waits for the order of export.
	held     bool
	seq      uint32
	handled  bool
	export   uint32
	since    time.Time
	deadline time.Time
	work     time.Duration // the key's engine time before it was held
}

type rerankState struct {
	scorer   rerank.Scorer
	timeout  time.Duration
	slots    *rerank.Slots
	state    []sessionRerank
	free     []int32
	slotOf   map[*rime.Session]int32
	held     []int32 // slots with a held result
	timer    *time.Timer
	answers  chan answer
	apps     rerank.Contexts
	sent     [rerank.Rings]uint32 // the revision of each ring the scorer has
	text     []byte
	req      rerank.Request
	stats    RerankStats
	attached bool
	on       bool
	misses   int  // held results timed out in a row
	probe    int  // requests while slow
	slow     bool // too slow: hold only probes
}

// initRerank maps the slots and registers them with librime-shixin. Without
// a scorer or librime-shixin, reranking stays off.
func (e *Engine) initRerank() {
	r := &e.rr
	r.answers = make(chan answer, 64)
	r.timer = time.NewTimer(time.Hour)
	r.timer.Stop()
	if e.opt.Scorer == nil || !e.Native() {
		return
	}
	slots, err := rerank.NewSlots(rerankSlots)
	if err != nil {
		e.opt.Logf("rerank: %v", err)
		return
	}
	if !e.lib.AttachRerank(slots.Base(), slots.Len(), int(rerank.SlotSize)) {
		e.opt.Logf("rerank: librime has no shixin_rerank with this slot layout")
		slots.Close()
		return
	}
	r.scorer = e.opt.Scorer
	r.timeout = e.opt.RerankTimeout
	if r.timeout <= 0 {
		r.timeout = DefaultRerankTimeout
	}
	r.slots = slots
	r.state = make([]sessionRerank, slots.Len())
	r.slotOf = make(map[*rime.Session]int32, slots.Len())
	r.free = make([]int32, 0, slots.Len())
	for i := slots.Len() - 1; i >= 0; i-- {
		r.free = append(r.free, int32(i))
	}
	r.held = make([]int32, 0, slots.Len())
	r.text = make([]byte, 0, 4*rerank.RingRunes)
	r.attached = true
	r.on = e.opt.Rerank
}

// setRerank turns reranking on or off for every session. Off, held
// results go out as they are, imported orders are dropped, and every
// app's context is forgotten (DESIGN §7.2, privacy).
func (e *Engine) setRerank(on bool) {
	r := &e.rr
	if !r.attached || on == r.on {
		return
	}
	r.on = on
	for i := range r.state {
		st := &r.state[i]
		if st.held {
			e.release(int32(i), nil)
		}
		sl := r.slots.At(i)
		if !on {
			sl.ClearImport()
		}
		if st.conn != nil {
			sl.Enabled = 0
			if on && !st.secure {
				sl.Enabled = 1
			}
		}
	}
	if !on {
		r.apps.Reset()
		r.sent = [rerank.Rings]uint32{}
	}
}

// closeRerank detaches the slots; librime is finalized after it.
func (e *Engine) closeRerank() {
	if e.rr.attached {
		e.lib.AttachRerank(nil, 0, 0)
		e.rr.attached = false
	}
}

// unmapRerank releases the slots once librime is gone.
func (e *Engine) unmapRerank() {
	if e.rr.slots != nil {
		e.rr.slots.Close()
		e.rr.slots = nil
	}
}

// Rerank delivers the scorer's order for an export: perm[k] is the export
// entry to show at position k. It may be called from any goroutine and
// never blocks; an order that can't be queued is dropped.
func (e *Engine) Rerank(session int, exportSeq uint32, perm []uint8) {
	a := answer{session: int32(session), exportSeq: exportSeq, n: uint8(min(len(perm), rerank.MaxOptions))}
	copy(a.perm[:], perm)
	select {
	case e.rr.answers <- a:
	default:
	}
}

// addSession gives a new session a slot, telling librime-shixin its index
// through a context property. Cold path.
func (e *Engine) addSession(s *rime.Session) {
	r := &e.rr
	if !r.attached || len(r.free) == 0 {
		return
	}
	i := r.free[len(r.free)-1]
	r.free = r.free[:len(r.free)-1]
	r.slotOf[s] = i
	r.state[i] = sessionRerank{sess: s}
	sl := r.slots.At(int(i))
	sl.Enabled = 0
	sl.ClearImport()
	s.SetProperty(rerank.SlotProperty, strconv.Itoa(int(i)))
}

// destroySession frees the session's slot and destroys it.
func (e *Engine) destroySession(s *rime.Session) {
	r := &e.rr
	if i, ok := r.slotOf[s]; ok {
		r.unhold(i)
		sl := r.slots.At(int(i))
		sl.Enabled = 0
		sl.ClearImport()
		r.state[i] = sessionRerank{}
		delete(r.slotOf, s)
		r.free = append(r.free, i)
	}
	s.Destroy()
}

// bindSession assigns a pool session to an input context.
func (e *Engine) bindSession(c *Conn, ctx uint32, s *rime.Session) {
	r := &e.rr
	i, ok := r.slotOf[s]
	if !ok {
		return
	}
	r.state[i].conn, r.state[i].ctx = c, ctx
	if r.on {
		r.slots.At(int(i)).Enabled = 1
	}
}

// slot returns the session's slot index and state, or -1.
func (r *rerankState) slot(s *rime.Session) (int32, *sessionRerank, *rerank.Slot) {
	i, ok := r.slotOf[s]
	if !ok {
		return -1, nil, nil
	}
	return i, &r.state[i], r.slots.At(int(i))
}

// focusRerank follows a context's focus: a secure field turns reranking
// off and records nothing; another field of an app breaks its text.
func (e *Engine) focusRerank(c *Conn, s *rime.Session, app string, secure bool) {
	r := &e.rr
	_, st, sl := r.slot(s)
	if st == nil {
		return
	}
	sl.ClearImport()
	st.secure = secure
	if secure || !r.on {
		sl.Enabled = 0
		return
	}
	sl.Enabled = 1
	a := r.apps.Slot(app)
	r.apps.Break(a)
	e.sendContext(a)
}

// sendContext gives the scorer an app's text unless it has this revision
// already. A new app's empty context is sent too: the scorer answers only
// for contexts it has.
func (e *Engine) sendContext(app int) {
	r := &e.rr
	if r.sent[app] == r.apps.Rev(app) {
		return
	}
	r.sent[app] = r.apps.Rev(app)
	r.text = r.apps.Text(r.text[:0], app)
	r.scorer.Context(app, r.apps.Rev(app), r.text)
}

// caretKey reports whether a key passed to the app may move the caret, so
// what follows may not continue the text before it.
func caretKey(keysym uint32) bool {
	switch keysym {
	case policy.KeyLeft, policy.KeyRight, policy.KeyUp, policy.KeyDown, policy.KeyHome, policy.KeyEnd, policy.KeyPrior, policy.KeyNext, policy.KeyBackSpace, policy.KeyDelete:
		return true
	}
	return false
}

// noteContext records what read took into the app's context: committed
// text, or a soft break after a caret move passed to the app.
func (e *Engine) noteContext(c *Conn, ctx uint32, s *rime.Session, r *request) {
	rr := &e.rr
	_, st, sl := rr.slot(s)
	if st == nil || sl.Enabled == 0 {
		return
	}
	app := c.focus[ctx].app
	switch {
	case len(e.commit) > 0:
		a := rr.apps.Slot(app)
		rr.apps.Append(a, e.commit)
		e.sendContext(a)
	case r.typ == ipc.TypeKey && r.flags&ipc.KeyNotify != 0 && caretKey(r.keysym) && !e.st.IsComposing:
		a := rr.apps.Slot(app)
		rr.apps.Break(a)
		e.sendContext(a)
	}
}

// hold asks for an order of a new export on page 1 and keeps the key's
// result until the order or the timeout. It reports false, and the
// caller sends the result at once, for keys the client waits on (ASK),
// notified keys, commits, menus the scorer can't help, and while the
// scorer isn't ready.
func (e *Engine) hold(c *Conn, r *request, s *rime.Session, handled bool, t0 time.Time) bool {
	rr := &e.rr
	i, st, sl := rr.slot(s)
	if st == nil || sl.Enabled == 0 || r.flags&(ipc.KeySync|ipc.KeyNotify) != 0 || len(e.commit) > 0 || !e.st.IsComposing {
		return false
	}
	if sl.ExportSeq == st.asked {
		return false // the same menu: its order, if any, is applied already
	}
	st.asked = sl.ExportSeq
	if sl.Count < 2 || e.ctx.PageNo != 0 || e.ctx.Highlighted != 0 || !rr.scorer.Ready() {
		return false
	}
	if rr.slow {
		if rr.probe++; rr.probe%slowProbe != 0 {
			return false
		}
	}
	a := rr.apps.Slot(c.focus[r.ctx].app)
	e.sendContext(a)
	rr.req = rerank.Request{Session: int(i), ExportSeq: sl.ExportSeq, App: a, AppRev: rr.apps.Rev(a), Export: sl}
	rr.stats.Requests++
	rr.scorer.Score(&rr.req)
	rr.req.Export = nil
	st.held, st.seq, st.handled, st.export = true, r.seq, handled, sl.ExportSeq
	st.since = time.Now()
	st.deadline = st.since.Add(rr.timeout)
	rr.held = append(rr.held, i)
	rr.arm()
	st.work = time.Since(t0)
	return true
}

// release sends slot i's held result, reordered by perm if given. It
// reports false if librime refused perm.
func (e *Engine) release(i int32, perm []uint8) bool {
	t0 := time.Now()
	rr := &e.rr
	st, sl := &rr.state[i], rr.slots.At(int(i))
	rr.stats.Wait.Add(t0.Sub(st.since))
	rr.unhold(i)
	s := st.sess
	identity := true
	for k, v := range perm {
		identity = identity && v == uint8(k)
	}
	ok := true
	if !identity {
		sl.Import(perm)
		applied := sl.Applied
		s.ProcessKey(rerank.RefreshKeysym, 0)
		if sl.Applied == applied {
			sl.ClearImport()
			ok = false
		}
	}
	if st.conn != nil && !st.conn.dead.Load() {
		e.read(s)
		e.send(st.conn, st.ctx, st.seq, st.handled)
	}
	// One sample per key: its work before and after the wait.
	e.stats.KeyTime.Add(st.work + time.Since(t0))
	return ok
}

// unhold forgets slot i's held result and rearms the timer.
func (r *rerankState) unhold(i int32) {
	if !r.state[i].held {
		return
	}
	r.state[i].held = false
	for k, v := range r.held {
		if v == i {
			r.held = append(r.held[:k], r.held[k+1:]...)
			break
		}
	}
	r.arm()
}

// arm sets the timer for the earliest held deadline.
func (r *rerankState) arm() {
	r.timer.Stop()
	if len(r.held) == 0 {
		return
	}
	first := r.state[r.held[0]].deadline
	for _, i := range r.held[1:] {
		if d := r.state[i].deadline; d.Before(first) {
			first = d
		}
	}
	r.timer.Reset(max(time.Until(first), 0))
}

// flushHeld sends s's held result in rime's order: a newer request for its
// context has arrived.
func (e *Engine) flushHeld(s *rime.Session) {
	if i, st, _ := e.rr.slot(s); st != nil && st.held {
		e.rr.stats.Superseded++
		e.release(i, nil)
	}
}

// expireHeld sends the held results whose time is up, in rime's order.
func (e *Engine) expireHeld() {
	rr := &e.rr
	now := time.Now()
	for k := 0; k < len(rr.held); {
		i := rr.held[k]
		if rr.state[i].deadline.After(now) {
			k++
			continue
		}
		rr.stats.Timeouts++
		if rr.misses++; rr.misses >= slowMisses && !rr.slow {
			rr.slow, rr.probe = true, 0
			e.opt.Logf("智能排序: the model is too slow here; pausing")
		}
		e.release(i, nil) // removes rr.held[k]
	}
	rr.arm()
}

// applyRerank sends a held result with its order, or drops an order whose
// result has gone out.
func (e *Engine) applyRerank(a *answer) {
	r := &e.rr
	if !r.attached || a.session < 0 || int(a.session) >= len(r.state) {
		return
	}
	st, sl := &r.state[a.session], r.slots.At(int(a.session))
	if !st.held || st.export != a.exportSeq || sl.ExportSeq != a.exportSeq {
		r.stats.Dropped++
		return
	}
	r.misses = 0
	if r.slow {
		r.slow = false
		e.opt.Logf("智能排序: the model is fast enough again")
	}
	if e.release(a.session, a.perm[:a.n]) {
		r.stats.Applied++
	} else {
		r.stats.Rejected++
	}
}
