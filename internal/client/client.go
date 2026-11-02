// Package client is the Go client core used by the macOS front (DESIGN
// §3.3). It classifies keys locally with the engine's key policy, sends
// them asynchronously, and applies results when they arrive, so the app's
// thread never waits on the engine except for ASK keys and ordering
// barriers, both capped by the budget.
//
// Threading: HandleKey, FocusIn/FocusOut, SetSecure, CommitComposition,
// Select, ChangePage and Drain must be called from one thread, the app's
// main thread, and the Applier is only called from them. ResetSpacing may
// be called from any thread. A background goroutine maintains the
// connection and reads results; it calls Options.Wake to schedule Drain.
//
// The key path (HandleKey through Drain) doesn't allocate.
package client

import (
	"errors"
	"io"
	"maps"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/perf"
	"shixin/internal/policy"
)

// Applier applies engine output to the app and the UI. Called on the main
// thread only. An Applier that can read the app's text also implements
// TextReader, which auto spacing uses to check before it inserts a space.
type Applier interface {
	// Candidates shows r's candidates, or hides the window if there are
	// none. It's called before the app is touched: our window updates in
	// well under a millisecond, the app's IPC can take several.
	Candidates(r *ipc.Result)
	// Commit inserts text into the app.
	Commit(text []byte)
	// Preedit sets the app's marked text; cursor is a byte offset into
	// text. Empty text clears it.
	Preedit(text []byte, cursor int)
	// Status reports a change of connection or input mode.
	Status(s Status)
}

// Status is what the menu bar shows.
type Status struct {
	Connected bool
	ASCII     bool
}

// Options configures a Client.
type Options struct {
	Socket string
	Kind   ipc.ClientKind
	App    string // process identifier for Hello
	// PerAppContexts keeps separate engine sessions for recently focused
	// applications. The fixed-size LRU reuses context IDs, so it cannot grow
	// the engine's session count as more apps are opened.
	PerAppContexts bool
	// Budget caps every wait on the app's thread (DESIGN R2). Default 8 ms.
	Budget time.Duration
	// Wake schedules Drain on the main thread. Called from background
	// goroutines; must not block.
	Wake func()
	// Unreachable is called from the connection goroutine when the engine
	// can't be reached, e.g. to start it. Optional.
	Unreachable func(err error)
	Logf        func(format string, args ...any)
}

// Stats are client counters. Read them on the main thread.
type Stats struct {
	Keys, Eaten, Asked, Passed, Notified uint64
	// Barriers counts PASS keys that had to wait for pending results.
	Barriers uint64
	// Timeouts counts waits that hit the budget: the key passed through.
	Timeouts uint64
	// Repairs counts eaten printable keys the engine declined; the client
	// inserted them itself.
	Repairs uint64
	// Mispredicts counts eaten non-printable keys the engine declined.
	// Must be zero by construction (DESIGN §3.3).
	Mispredicts uint64
	Connects    uint64
	// KeyToCandidates and KeyToApplied measure from HandleKey to the
	// candidate window update and to the app's text update.
	KeyToCandidates, KeyToApplied perf.Hist
}

// Client is one connection's client core.
type Client struct {
	opt Options
	a   Applier

	// Main thread only.
	ctx          uint32
	seq          uint32
	app          string
	cur          *policy.Policy // policy of the last applied state
	spec         *policy.Policy // assumed policy while results are pending; nil: unknown
	composing    bool
	secure       bool
	marked       []byte // last text actually sent to Applier.Preedit
	clearSeq     uint32 // results before this fence belong to a finished composition
	contextFence uint32 // target-context Clear reply is a fence, never a usable policy
	dropZero     bool   // ignore old FocusIn results until the clear fence arrives
	shiftDown    bool
	ctrlDown     bool
	connected    bool
	status       Status
	timer        *time.Timer
	keys         [256]sentKey
	syncSeq      uint32
	syncHandled  bool
	stats        Stats
	sp           spacer
	contexts     [16]appContext
	contextTick  uint64

	// Shared with the connection goroutine, without locks: the main
	// thread never waits on the reader.
	conn     atomic.Pointer[conn]
	sent     atomic.Uint32 // last tracked seq handed to the writer
	arrived  atomic.Uint32 // seq of the last result that arrived
	arriveCh chan struct{}
	waking   atomic.Bool
	// slots is a single-producer (reader), single-consumer (Drain) ring:
	// slots[head:tail] await Drain.
	slots      [queueLen]slot
	head, tail atomic.Uint32
	// pols is replaced, never modified, when a policy arrives (rarely).
	pols atomic.Pointer[map[uint16]*policy.Policy]
}

const queueLen = 128

type sentKey struct {
	seq    uint32
	keysym uint32
	eaten  bool
	t      time.Time
}

type eventKind uint8

const (
	evResult eventKind = iota
	evConnected
	evDisconnected
	evUnavailable
	evMigrate
)

// slot is a queued event. Results are deep-copied into buf, which is
// reused, so the reader's frame buffer can be recycled at once.
type slot struct {
	kind eventKind
	res  ipc.Result
	buf  []byte
	cn   *conn
}

// New returns a client. Call Start to connect.
func New(opt Options, a Applier) *Client {
	if opt.Budget <= 0 {
		opt.Budget = 8 * time.Millisecond
	}
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	if opt.Wake == nil {
		opt.Wake = func() {}
	}
	c := &Client{
		opt:      opt,
		a:        a,
		ctx:      1,
		marked:   make([]byte, 0, 1024),
		cur:      &policy.Passthrough,
		spec:     &policy.Passthrough,
		arriveCh: make(chan struct{}, 1),
		timer:    time.NewTimer(time.Hour),
	}
	r, _ := a.(TextReader)
	c.sp = newSpacer(r)
	c.pols.Store(&map[uint16]*policy.Policy{})
	c.timer.Stop()
	for i := range c.slots {
		c.slots[i].buf = make([]byte, 0, 1024)
		c.slots[i].res.Candidates = make([]ipc.Candidate, 0, 16)
	}
	return c
}

// Start connects in the background and keeps reconnecting until stop is
// closed.
func (c *Client) Start(stop <-chan struct{}) {
	go c.connectLoop(stop)
}

// Stats returns the counters. Main thread.
func (c *Client) Stats() Stats { return c.stats }

// ResetLatency clears the latency histograms. Main thread.
func (c *Client) ResetLatency() {
	c.stats.KeyToCandidates = perf.Hist{}
	c.stats.KeyToApplied = perf.Hist{}
}

func (c *Client) pending() bool { return c.seq != c.arrived.Load() }

// HandleKey classifies a key and reports whether the app should consider
// it handled. keysym is an X11 keysym (for printable keys, the character
// typed) and mods a Rime modifier mask. Main thread.
func (c *Client) HandleKey(keysym, mods uint32) bool {
	c.stats.Keys++
	if c.secure {
		// Never send password keys to Rime, even with a stale Chinese
		// policy, and never let the spacer read or edit this field.
		c.stats.Passed++
		return false
	}
	// A mode/focus reply can arrive before its scheduled main-thread wake.
	// Classifying with the previous mode can pass the first spelling key to
	// the app even though the engine has already switched to Chinese.
	c.Drain()
	if mods&policy.ReleaseMask == 0 {
		c.sp.tick()
	}
	p := c.cur
	if c.pending() {
		if c.spec == nil {
			// The last eaten key may have ended the composition: wait for
			// the real state rather than guess.
			c.wait(0)
			c.Drain()
			if c.pending() {
				c.stats.Timeouts++
				c.stats.Passed++
				c.sp.clear() // results still to come land after this key
				return false // degraded: pass through
			}
			p = c.cur
		} else {
			p = c.spec
		}
	}
	act := p.Lookup(keysym, mods)
	switch act.Disposition() {
	case policy.PassNotify:
		release := mods&policy.ReleaseMask != 0
		switch keysym {
		case policy.KeyShiftL, policy.KeyShiftR:
			c.shiftDown = !release
		case policy.KeyControlL, policy.KeyControlR:
			c.ctrlDown = !release
		}
		c.stats.Notified++
		if keysym >= 0x20 && keysym < 0x7f && !release {
			// A character the app inserts (a digit the engine tracks for
			// 3.5): it needs the ordering barrier, like PASS.
			c.barrier()
		}
		c.sendKey(keysym, mods, ipc.KeyNotify, false)
		return c.pass(keysym, mods)

	case policy.Eat:
		if !c.sendKey(keysym, mods, 0, true) {
			c.stats.Passed++
			return c.pass(keysym, mods)
		}
		c.stats.Eaten++
		switch act.Next() {
		case policy.Same:
			c.spec = p
		case policy.Composing:
			c.spec = c.policy(p.Composing)
		default:
			c.spec = nil
		}
		return true

	case policy.Ask:
		c.stats.Asked++
		if !c.sendKey(keysym, mods, ipc.KeySync, false) {
			return c.pass(keysym, mods)
		}
		seq := c.seq
		c.spec = nil
		if !c.wait(seq) {
			c.stats.Timeouts++
			c.sp.clear()
			return false
		}
		c.Drain()
		if c.syncSeq == seq && c.syncHandled {
			return true
		}
		return c.pass(keysym, mods)
	}

	c.stats.Passed++
	c.barrier()
	// ascii_composer must see keys typed while Shift is held, or it
	// mistakes Shift+key for a Shift tap and switches modes.
	if mods&policy.ReleaseMask == 0 && (c.shiftDown && p.Notify&policy.NotifyShift != 0 || c.ctrlDown && p.Notify&policy.NotifyControl != 0) {
		c.sendKey(keysym, mods, ipc.KeyNotify, false)
	}
	return c.pass(keysym, mods)
}

// barrier is the ordering barrier for a key the app handles: text from
// keys sent earlier must reach the app before it does (e.g. "nihao" then
// Cmd-V).
func (c *Client) barrier() {
	if !c.pending() {
		return
	}
	c.stats.Barriers++
	if !c.wait(0) {
		c.stats.Timeouts++
		c.sp.clear() // late commits land after this key
	}
	c.Drain()
}

// pass handles a key that goes to the app. Auto spacing may take it and
// insert it after a space instead (DESIGN §7.1).
func (c *Client) pass(keysym, mods uint32) bool {
	if text := c.sp.key(keysym, mods); text != nil {
		c.a.Commit(text)
		return true
	}
	return false
}

// commit inserts engine output into the app, after a space where auto
// spacing calls for one.
func (c *Client) commit(text []byte) { c.a.Commit(c.sp.commit(text)) }

// ResetSpacing tells auto spacing that the text left of the caret may have
// changed without the client seeing it: a click, an app or window switch,
// sleep (DESIGN §7.1). The next decision starts from unknown. Any thread.
func (c *Client) ResetSpacing() { c.sp.reset.Store(true) }

// policy looks up a policy by ID; nil if the client doesn't have it.
func (c *Client) policy(id uint16) *policy.Policy { return (*c.pols.Load())[id] }

// wait blocks until the result for seq (or, with seq 0, for everything
// sent) has arrived, or the budget runs out.
func (c *Client) wait(seq uint32) bool {
	if seq == 0 {
		seq = c.seq
	}
	done := func() bool { return int32(c.arrived.Load()-seq) >= 0 }
	if done() {
		return true
	}
	deadline := time.Now().Add(c.opt.Budget)
	for !done() {
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		c.timer.Reset(left)
		select {
		case <-c.arriveCh:
		case <-c.timer.C:
		}
		c.timer.Stop()
	}
	return true
}

type outMsg struct {
	typ   ipc.Type
	key   ipc.Key
	index uint32
	flag  bool
	app   string
}

func (c *Client) post(m *outMsg) bool {
	cn := c.conn.Load()
	if cn == nil {
		return false
	}
	select {
	case cn.out <- *m:
		return true
	default:
		cn.kill() // writer stuck: reconnect rather than ever block
		return false
	}
}

// sendKey sends a key. Every key gets a sequence number, so barriers see
// its result.
func (c *Client) sendKey(keysym, mods uint32, flags uint8, eaten bool) bool {
	if flags&ipc.KeyNotify != 0 && keysym >= 0x20 && keysym < 0x7f && (keysym < '0' || keysym > '9') {
		// The app already owns this printable key. ascii_composer still
		// needs to cancel a held-Shift tap, but a real key-down could also
		// start Chinese composition or translate punctuation. A release
		// clears modifier tracking without inserting the character again.
		mods |= policy.ReleaseMask
	}
	m := outMsg{typ: ipc.TypeKey, key: ipc.Key{Ctx: c.ctx, Keysym: keysym, Mods: mods, Flags: flags}}
	return c.sendSeq(&m, sentKey{keysym: keysym, eaten: eaten, t: time.Now()})
}

// sendTracked sends a message. Select, ChangePage and Clear get a sequence
// number, as keys do; FocusIn and FocusOut have none.
func (c *Client) sendTracked(m *outMsg) bool {
	switch m.typ {
	case ipc.TypeSelect, ipc.TypeChangePage, ipc.TypeClear:
		return c.sendSeq(m, sentKey{})
	}
	return c.post(m)
}

// sendSeq sends m with the next sequence number and records k for its
// result.
func (c *Client) sendSeq(m *outMsg, k sentKey) bool {
	m.key.Seq = c.seq + 1
	if !c.post(m) {
		return false
	}
	c.seq++
	c.sent.Store(c.seq)
	k.seq = c.seq
	c.keys[c.seq%uint32(len(c.keys))] = k
	return true
}

// FocusIn tells the engine which app and kind of field has focus. The
// local secure guard takes effect before the asynchronous reply. Main thread.
func (c *Client) FocusIn(app string, secure bool) {
	if c.opt.PerAppContexts {
		ctx := c.appContext(app)
		if ctx != c.ctx {
			if c.composing || len(c.marked) != 0 {
				// FocusIn may already refer to the new field (including a
				// password). Only FocusOut, while the old proxy is valid,
				// may commit its marked text. Drop late engine output here.
				c.sendTracked(&outMsg{typ: ipc.TypeClear, key: ipc.Key{Ctx: c.ctx}})
			}
			c.a.Candidates(&emptyResult)
			c.ctx = ctx
			// Replies for the previous context are rejected by acceptResult.
			// Its Clear fence must not suppress this context's FocusIn reply.
			c.cur, c.spec = &policy.Passthrough, &policy.Passthrough
			c.composing = false
			c.marked = c.marked[:0]
			c.shiftDown, c.ctrlDown = false, false
			c.sp.on = false
			// Context IDs are bounded and can be reused. Fence the target
			// stream before FocusIn so a late reply from its previous owner
			// cannot insert text or install an obsolete policy in this app.
			// Keep the fence even if a full output queue kills the connection:
			// its already-read results remain queued until Drain consumes them.
			c.clearSeq, c.contextFence, c.dropZero = c.seq+1, c.seq+1, true
			c.sendTracked(&outMsg{typ: ipc.TypeClear, key: ipc.Key{Ctx: ctx}})
		}
	}
	c.sp.focus(app, c.app)
	if c.secure != secure {
		c.sp.on = false // wait for the new context's spacing setting
		c.shiftDown, c.ctrlDown = false, false
	}
	c.secure = secure
	if secure {
		// The proxy may already refer to the password field: don't clear
		// old marked text through it or apply queued composition output.
		c.composing, c.marked = false, c.marked[:0]
	}
	c.app = app
	c.post(&outMsg{typ: ipc.TypeFocusIn, key: ipc.Key{Ctx: c.ctx}, app: app, flag: secure})
}

type appContext struct {
	app  string
	used uint64
}

// appContext is a cold focus-path lookup with no allocation. IDs are stable
// while an app remains in the bounded LRU; an evicted ID gets a normal FocusIn
// for its new owner, after the old field was finalized by FocusOut.
func (c *Client) appContext(app string) uint32 {
	c.contextTick++
	oldest := 0
	for i := range c.contexts {
		s := &c.contexts[i]
		if s.used != 0 && s.app == app {
			s.used = c.contextTick
			return uint32(i + 1)
		}
		if s.used < c.contexts[oldest].used {
			oldest = i
		}
	}
	c.contexts[oldest] = appContext{app: app, used: c.contextTick}
	return uint32(oldest + 1)
}

// SetSecure rechecks the field without a new app or proxy. A change sends
// FocusIn asynchronously; an unchanged state does nothing. Main thread.
func (c *Client) SetSecure(secure bool) {
	if c.secure != secure {
		c.FocusIn(c.app, secure)
	}
}

// FocusOut commits the last displayed marked text while the app's client is
// still valid. Engine cleanup is asynchronous. Main thread.
func (c *Client) FocusOut() {
	c.CommitComposition()
	c.post(&outMsg{typ: ipc.TypeFocusOut, key: ipc.Key{Ctx: c.ctx}})
}

// CommitComposition inserts the text last shown inline, without the spaces
// Rime puts between syllables ("ni hao" commits as typed, "nihao"), through
// the usual auto spacing path. It never drains or waits for the engine: queued or
// late output for the old composition must not commit twice or restore it.
func (c *Client) CommitComposition() {
	text := c.marked[:0]
	for _, b := range c.marked {
		if b != ' ' {
			text = append(text, b)
		}
	}
	c.marked = c.marked[:0]
	c.composing = false
	c.clearSeq, c.dropZero = c.seq+1, true
	c.sendTracked(&outMsg{typ: ipc.TypeClear, key: ipc.Key{Ctx: c.ctx}})
	c.cur, c.spec = &policy.Passthrough, nil
	c.a.Candidates(&emptyResult)
	if len(text) > 0 {
		c.commit(text)
	}
	c.sp.clear()
}

// acceptResult also fences unsequenced focus replies, which are ordered on
// the same stream as Clear. Each Drain pass starts with the same fence state.
func (c *Client) acceptResult(r *ipc.Result, dropZero *bool) bool {
	if r.Ctx != c.ctx {
		return false
	}
	if r.Seq == 0 {
		return !*dropZero
	}
	if c.clearSeq != 0 && int32(r.Seq-c.clearSeq) < 0 {
		return false
	}
	*dropZero = false
	if r.Seq == c.contextFence {
		return false
	}
	return true
}

// Select picks candidate i on the current page (mouse). Main thread.
func (c *Client) Select(i int) {
	if c.secure {
		return
	}
	c.sp.tick()
	if c.sendTracked(&outMsg{typ: ipc.TypeSelect, key: ipc.Key{Ctx: c.ctx}, index: uint32(i)}) {
		c.spec = nil
	}
}

// ChangePage flips the candidate page. Main thread.
func (c *Client) ChangePage(backward bool) {
	if c.secure {
		return
	}
	c.sendTracked(&outMsg{typ: ipc.TypeChangePage, key: ipc.Key{Ctx: c.ctx}, flag: backward})
}

// Drain applies queued results. Commits are applied in order; the window
// and the marked text only for the last result (DESIGN §3.3). Main thread.
func (c *Client) Drain() {
	c.waking.Store(false)
	h, t := c.head.Load(), c.tail.Load()
	if h == t {
		return
	}
	var last *slot
	dropZero := c.dropZero
	for i := h; i != t; i++ {
		s := &c.slots[i%queueLen]
		if s.kind == evConnected {
			dropZero = false // a new connection has no old focus replies
		}
		if s.kind == evResult && c.acceptResult(&s.res, &dropZero) {
			last = s
		}
	}
	if last != nil {
		if c.secure {
			c.a.Candidates(&emptyResult)
		} else {
			c.a.Candidates(&last.res)
		}
		if k := c.sentKey(last.res.Seq); k != nil {
			c.stats.KeyToCandidates.Add(time.Since(k.t))
		}
	}
	for i := h; i != t; i++ {
		s := &c.slots[i%queueLen]
		switch s.kind {
		case evResult:
			if c.acceptResult(&s.res, &c.dropZero) {
				c.applyResult(&s.res)
			}
		case evConnected:
			c.dropZero = false
			c.clearSeq, c.contextFence = 0, 0
			c.connected = true
			c.stats.Connects++
			// Get the initial state and policy now, not on the first key:
			// after a restart the focused app may never activate us again.
			c.FocusIn(c.app, c.secure)
		case evUnavailable:
			c.cur, c.spec = &policy.Passthrough, &policy.Passthrough
		case evMigrate:
			// Main-thread boundary: no producer can post behind the
			// drain marker. The reader keeps applying earlier results.
			if s.cn != nil && c.conn.CompareAndSwap(s.cn, nil) {
				select {
				case s.cn.out <- outMsg{typ: ipc.TypeMigrate}:
				default:
					s.cn.kill()
				}
			}
			c.sp.on = false
			c.cur, c.spec = &policy.Passthrough, &policy.Passthrough
		case evDisconnected:
			c.connected, c.composing = false, false
			c.sp.on = false
			c.cur, c.spec = &policy.Passthrough, &policy.Passthrough
			c.a.Candidates(&emptyResult)
			if len(c.marked) > 0 {
				c.a.Preedit(nil, 0)
				c.marked = c.marked[:0]
			}
			last = nil
		}
	}
	if last != nil && !c.secure && (len(last.res.Preedit) > 0 || len(c.marked) > 0) {
		c.a.Preedit(last.res.Preedit, int(last.res.Cursor))
		c.marked = append(c.marked[:0], last.res.Preedit...)
	}
	if last != nil {
		if k := c.sentKey(last.res.Seq); k != nil {
			c.stats.KeyToApplied.Add(time.Since(k.t))
		}
	}
	// Snapshot the last slot before publishing head: the reader may reuse
	// every released slot immediately after this store.
	st := Status{Connected: c.connected}
	if last != nil {
		st.ASCII = last.res.Flags&ipc.ResultASCII != 0
	} else {
		st.ASCII = c.status.ASCII
	}
	st.ASCII = st.ASCII || c.secure
	c.head.Store(t)
	if !c.pending() {
		c.spec = c.cur
	}
	if st != c.status {
		c.status = st
		c.a.Status(st)
	}
}

// sentKey returns the record of the key with sequence number seq, or nil.
func (c *Client) sentKey(seq uint32) *sentKey {
	if k := &c.keys[seq%uint32(len(c.keys))]; seq != 0 && k.seq == seq && !k.t.IsZero() {
		return k
	}
	return nil
}

func (c *Client) applyResult(r *ipc.Result) {
	c.sp.on = !c.secure && r.Flags&ipc.ResultAutoSpace != 0
	if !c.secure && len(r.Commit) > 0 {
		c.commit(r.Commit)
		c.marked = c.marked[:0]
	}
	if r.Seq != 0 {
		k := &c.keys[r.Seq%uint32(len(c.keys))]
		if !c.secure && k.seq == r.Seq && k.eaten && r.Flags&ipc.ResultHandled == 0 {
			if k.keysym >= 0x20 && k.keysym < 0x7f {
				// Same effect as having passed the key through.
				c.sp.buf1[0] = byte(k.keysym)
				c.commit(c.sp.buf1[:])
				c.stats.Repairs++
			} else {
				c.stats.Mispredicts++
				c.opt.Logf("mispredict: non-printable key %#x eaten but not handled", k.keysym)
			}
		}
		c.syncSeq, c.syncHandled = r.Seq, r.Flags&ipc.ResultHandled != 0
	}
	c.composing = !c.secure && r.Flags&ipc.ResultComposing != 0
	if p := c.policy(r.Policy); p != nil {
		c.cur = p
	}
}

var emptyResult ipc.Result

// ---------------------------------------------------------------- connection

type conn struct {
	c    net.Conn
	out  chan outMsg
	once sync.Once
	next string // reader/connection goroutine only
}

func (cn *conn) kill() { cn.once.Do(func() { cn.c.Close() }) }

func (c *Client) connectLoop(stop <-chan struct{}) {
	backoff := 20 * time.Millisecond
	endpoint := c.opt.Socket
	for {
		nc, err := ipc.Dial(endpoint, time.Second)
		if err != nil {
			if c.opt.Unreachable != nil {
				c.opt.Unreachable(err)
			}
			select {
			case <-stop:
				return
			case <-time.After(backoff):
			}
			backoff = min(2*backoff, time.Second)
			continue
		}
		backoff = 20 * time.Millisecond
		cn := &conn{c: nc, out: make(chan outMsg, 256)}
		finished := make(chan struct{})
		go func() {
			select {
			case <-stop:
				cn.kill()
			case <-finished:
			}
		}()
		c.serve(cn)
		close(finished)
		if cn.next != "" {
			endpoint = cn.next
		}
		select {
		case <-stop:
			return
		default:
		}
	}
}

// serve runs one connection until it fails.
func (c *Client) serve(cn *conn) {
	hello := ipc.Hello{Version: ipc.Version, Kind: c.opt.Kind, App: []byte(c.opt.App)}
	if _, err := cn.c.Write(hello.Append(nil)); err != nil {
		cn.kill()
		return
	}
	done := make(chan struct{})
	go c.write(cn, done)
	err := c.read(cn)
	cn.kill()
	close(done)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		c.opt.Logf("engine connection: %v", err)
	}
	c.conn.CompareAndSwap(cn, nil)
	c.push(evDisconnected, nil)
	// Release any barrier waiting on results that will never come.
	c.arrived.Store(c.sent.Load())
	c.signalArrived()
}

func (c *Client) write(cn *conn, done <-chan struct{}) {
	buf := make([]byte, 0, 4096)
	for {
		var m outMsg
		select {
		case m = <-cn.out:
		case <-done:
			return
		}
		buf = buf[:0]
		for {
			if m.typ == ipc.TypeMigrate {
				if len(buf) > 0 {
					if _, err := cn.c.Write(buf); err != nil {
						cn.kill()
						return
					}
				}
				if half, ok := cn.c.(interface{ CloseWrite() error }); ok {
					if half.CloseWrite() != nil {
						cn.kill()
					}
				} else {
					cn.kill()
				}
				return
			}
			buf = encode(buf, &m)
			select {
			case m = <-cn.out:
				continue
			default:
			}
			break
		}
		if _, err := cn.c.Write(buf); err != nil {
			cn.kill()
			return
		}
	}
}

func encode(b []byte, m *outMsg) []byte {
	switch m.typ {
	case ipc.TypeKey:
		return m.key.Append(b)
	case ipc.TypeFocusIn:
		f := ipc.FocusIn{Ctx: m.key.Ctx, App: []byte(m.app)}
		if m.flag {
			f.Flags = ipc.FocusSecure
		}
		return f.Append(b)
	case ipc.TypeFocusOut:
		f := ipc.FocusOut{Ctx: m.key.Ctx}
		return f.Append(b)
	case ipc.TypeSelect:
		s := ipc.Select{Ctx: m.key.Ctx, Seq: m.key.Seq, Index: m.index}
		return s.Append(b)
	case ipc.TypeChangePage:
		p := ipc.ChangePage{Ctx: m.key.Ctx, Seq: m.key.Seq, Backward: m.flag}
		return p.Append(b)
	case ipc.TypeClear:
		cl := ipc.Clear{Ctx: m.key.Ctx, Seq: m.key.Seq}
		return cl.Append(b)
	}
	return b
}

func (c *Client) read(cn *conn) error {
	r := ipc.NewReader(cn.c, 8192)
	var res ipc.Result
	res.Candidates = make([]ipc.Candidate, 0, 16)
	for {
		typ, payload, err := r.Next()
		if err != nil {
			return err
		}
		switch typ {
		case ipc.TypeWelcome:
			var w ipc.Welcome
			if err := w.Decode(payload); err != nil {
				return err
			}
			c.conn.Store(cn)
			c.push(evConnected, nil)
		case ipc.TypePolicy:
			p := &policy.Policy{} // rare: once per policy per connection
			if err := ipc.DecodePolicy(payload, p); err != nil {
				return err
			}
			m := maps.Clone(*c.pols.Load())
			m[p.ID] = p
			c.pols.Store(&m)
		case ipc.TypeResult:
			if err := res.Decode(payload); err != nil {
				return err
			}
			if !c.push(evResult, &res) {
				return errors.New("client: result queue full (main thread stuck?)")
			}
			if res.Seq != 0 {
				c.arrived.Store(res.Seq)
			}
			c.signalArrived()
		case ipc.TypeUnavailable:
			c.push(evUnavailable, nil)
		case ipc.TypeMigrate:
			var m ipc.Migrate
			if err := m.Decode(payload); err != nil {
				return err
			}
			if len(m.Endpoint) == 0 {
				return ipc.ErrMalformed
			}
			cn.next = string(m.Endpoint)
			if !c.push(evMigrate, nil) {
				return errors.New("client: migration queue full")
			}
		}
	}
}

func (c *Client) signalArrived() {
	select {
	case c.arriveCh <- struct{}{}:
	default:
	}
}

// push queues an event for Drain and wakes the main thread. Only the
// connection goroutine calls it.
func (c *Client) push(kind eventKind, r *ipc.Result) bool {
	t := c.tail.Load()
	if t-c.head.Load() >= queueLen {
		return false
	}
	// Drain never reads at or past tail, so the slot is ours until the
	// store below publishes it.
	s := &c.slots[t%queueLen]
	s.kind = kind
	if kind == evMigrate {
		s.cn = c.conn.Load()
	}
	if r != nil {
		copyResult(s, r)
	}
	c.tail.Store(t + 1)
	if c.waking.CompareAndSwap(false, true) {
		c.opt.Wake()
	}
	return true
}

func copyResult(s *slot, r *ipc.Result) {
	b := s.buf[:0]
	str := func(src []byte) []byte {
		start := len(b)
		b = append(b, src...)
		return b[start:len(b):len(b)]
	}
	// Reserve once so the appends below don't move earlier slices.
	n := len(r.Commit) + len(r.Preedit) + len(r.Labels)
	for i := range r.Candidates {
		n += len(r.Candidates[i].Text) + len(r.Candidates[i].Comment)
	}
	if cap(b) < n {
		b = make([]byte, 0, 2*n)
	}
	d := &s.res
	cands := d.Candidates[:0]
	*d = *r
	d.Commit = str(r.Commit)
	d.Preedit = str(r.Preedit)
	d.Labels = str(r.Labels)
	for i := range r.Candidates {
		cands = append(cands, ipc.Candidate{Text: str(r.Candidates[i].Text), Comment: str(r.Candidates[i].Comment)})
	}
	d.Candidates = cands
	s.buf = b
}
