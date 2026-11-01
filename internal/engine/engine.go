// Package engine runs librime for shixin-engine (DESIGN §3.4): one goroutine
// locked to its OS thread owns librime, serves requests from client
// connections in order, and answers each with a Result and, when the client
// doesn't have it yet, the key policy for the next key.
//
// The key path is allocation-free: requests are values on a channel,
// librime output is read into reused buffers, and frames are encoded into
// per-connection buffers recycled by the connection's writer.
package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"shixin/internal/generation"
	"shixin/internal/ipc"
	"shixin/internal/perf"
	"shixin/internal/policy"
	"shixin/internal/rerank"
	"shixin/internal/rime"
)

// Options configures an Engine.
type Options struct {
	LibPath         string // librime shared library
	SharedDataDir   string
	UserDataDir     string // locked by the caller, except validated Standby warm-up
	PrebuiltDataDir string
	StagingDir      string
	// Standby permits only validated warm schemas until Promote, whose
	// caller must first hold the live user's engine lock.
	Standby     bool
	LogDir      string // "" logs to stderr
	MinLogLevel int
	Version     string
	// Modules are the librime modules to load. Defaults to {"default"},
	// plus "shixin" when librime has it. Never include "plugins" or "lua"
	// (DESIGN R9).
	Modules []string
	// Deploy runs librime's maintenance before warm-up when data changed.
	// The product's builder does this off the engine (§3.5); tests and the
	// M1 engine deploy at startup, before accepting clients.
	Deploy bool
	// Warmup is typed through each schema before serving (§3.4).
	Warmup []string
	// PoolSize is the number of ready sessions kept for new input contexts.
	PoolSize int
	// Apps are the per-app rules (DESIGN §7.1, §8); see SetApps.
	Apps AppModes
	// Scorer makes 智能排序 (DESIGN §7.2) available when librime-shixin is
	// loaded; its answers go to Rerank. Rerank is whether it starts on
	// (SetRerank changes it). RerankTimeout is how long a key's result
	// waits for an order (default DefaultRerankTimeout).
	Scorer        rerank.Scorer
	Rerank        bool
	RerankTimeout time.Duration
	// Logf receives diagnostics. It must not block. Never log typed text.
	Logf func(format string, args ...any)
}

// Engine owns librime. Create it with Start.
type Engine struct {
	opt     Options
	lib     *rime.Lib
	reqs    chan request
	stopped chan struct{}
	version string

	// Engine thread only.
	pols       []policy.Policy
	schemas    map[string]*schemaPolicies
	pool       []*rime.Session
	ctx        rime.Context
	st         rime.Status
	commit     []byte
	res        ipc.Result
	stats      Stats
	apps       AppModes
	modes      map[string]bool // each app's last mode (English), with apps.Remember
	started    time.Time
	conns      map[*Conn]bool
	realSchema string
	migrating  bool
	rr         rerankState
}

// AppModes are the per-app rules: the mode applied when an input
// context's app changes (FocusIn), and where auto spacing is on.
type AppModes struct {
	// English maps app ids to their default mode: true for English, false
	// for Chinese. Apps without an entry keep the current mode.
	English map[string]bool
	// Remember keeps each app's last mode, which then wins over its rule.
	Remember bool
	// AutoSpace turns auto spacing on (DESIGN §7.1), except in secure
	// fields and the apps in NoAutoSpace. Clients learn it from
	// ipc.ResultAutoSpace.
	AutoSpace   bool
	NoAutoSpace map[string]bool
}

// Stats are engine counters, read with Engine.Stats.
type Stats struct {
	Keys, Dropped uint64
	// KeyTime is the engine's time per key: librime plus reading and
	// encoding the result (DESIGN §3.7, "engine per key").
	KeyTime     perf.Hist
	Policies    int
	Schemas     []string
	StartupTime time.Duration
	Rerank      RerankStats
}

type reqKind uint8

const (
	reqMessage reqKind = iota // an ipc message from conn
	reqHello
	reqClosed // conn closed: destroy its sessions
	reqStop
	reqStats
	reqApps
	reqPromote
	reqMigrate
	reqDrained
	reqGone
	reqRerank
)

// request is one unit of engine work. It is passed by value so the key path
// doesn't allocate.
type request struct {
	kind   reqKind
	typ    ipc.Type
	conn   *Conn
	ctx    uint32
	seq    uint32
	keysym uint32
	mods   uint32
	flags  uint8
	index  uint32
	on     bool
	name   string // option name, app id; allocated, cold paths only
	stats  chan Stats
	apps   *AppModes
	answer chan error
}

// running is set while an Engine owns librime, which is a process-wide
// singleton: a second Setup/Initialize would silently share the first
// engine's service.
var running atomic.Bool

// Start loads librime, deploys if asked, warms up and builds the key
// policies, all on a new locked OS thread, and returns once the engine is
// ready to serve.
func Start(opt Options) (*Engine, error) {
	if opt.Standby {
		if opt.Deploy {
			return nil, errors.New("engine: standby cannot deploy")
		}
		if err := generation.Validate(filepath.Dir(opt.StagingDir)); err != nil {
			return nil, err
		}
	}
	for _, m := range opt.Modules {
		if m == "plugins" || strings.Contains(m, "lua") {
			return nil, fmt.Errorf("engine: module %q is not allowed (DESIGN R9)", m)
		}
	}
	if !running.CompareAndSwap(false, true) {
		return nil, errors.New("engine: another engine is running in this process")
	}
	if opt.PoolSize <= 0 {
		opt.PoolSize = 4
	}
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	e := &Engine{
		opt:     opt,
		reqs:    make(chan request, 1024),
		stopped: make(chan struct{}),
		schemas: map[string]*schemaPolicies{},
		commit:  make([]byte, 0, 256),
		apps:    opt.Apps,
		modes:   map[string]bool{},
		conns:   map[*Conn]bool{},
	}
	e.res.Candidates = make([]ipc.Candidate, 0, 16)
	ready := make(chan error, 1)
	go e.run(ready)
	if err := <-ready; err != nil {
		<-e.stopped
		return nil, err
	}
	return e, nil
}

func (e *Engine) run(ready chan<- error) {
	runtime.LockOSThread()
	setInteractivePriority()
	defer close(e.stopped)
	defer running.Store(false)
	if err := e.init(); err != nil {
		if e.lib != nil {
			e.lib.Finalize()
		}
		ready <- err
		return
	}
	ready <- nil
	e.loop()
	for c := range e.conns {
		c.kill()
	}
	for _, s := range e.pool {
		s.Destroy()
	}
	e.closeRerank()
	e.lib.Finalize() // closes the user dictionary
	e.unmapRerank()
}

func (e *Engine) init() error {
	t0 := time.Now()
	e.started = t0
	lib, err := rime.Load(e.opt.LibPath)
	if err != nil {
		return err
	}
	e.lib = lib
	if len(e.opt.Modules) == 0 {
		e.opt.Modules = []string{"default"}
		// Our librime build merges librime-shixin (DESIGN §3.8).
		if lib.HasModule("shixin") {
			e.opt.Modules = append(e.opt.Modules, "shixin")
		}
	}
	if err := os.MkdirAll(e.opt.UserDataDir, 0o755); err != nil {
		return fmt.Errorf("engine: user dir: %w", err)
	}
	lib.Setup(rime.Traits{
		SharedDataDir:        e.opt.SharedDataDir,
		UserDataDir:          e.opt.UserDataDir,
		PrebuiltDataDir:      e.opt.PrebuiltDataDir,
		StagingDir:           e.opt.StagingDir,
		LogDir:               e.opt.LogDir,
		MinLogLevel:          e.opt.MinLogLevel,
		DistributionName:     "Shixin",
		DistributionCodeName: "shixin",
		DistributionVersion:  e.opt.Version,
		AppName:              "rime.shixin",
		Modules:              e.opt.Modules,
	})
	lib.Initialize()
	e.version = lib.Version()
	if e.opt.Deploy {
		lib.StartMaintenance(false)
		lib.JoinMaintenanceThread()
	}

	schemas, err := e.schemaList()
	if err != nil {
		return err
	}
	e.initRerank() // before the policies, which depend on it
	e.pols = append(e.pols, conservativePolicy())
	s, err := lib.CreateSession()
	if err != nil {
		return err
	}
	p := &prober{s: s}
	for _, id := range schemas {
		sp, err := e.buildPolicies(p, id)
		if err != nil {
			return fmt.Errorf("engine: policies: %w", err)
		}
		e.schemas[id] = sp
		for _, w := range e.opt.Warmup {
			s.SimulateKeySequence(w)
			s.ClearComposition()
		}
	}
	// Selecting a schema is remembered in the user config
	// (var/previously_selected_schema) and decides the schema of new
	// sessions, so end on the first one.
	s.SelectSchema(schemas[0])
	s.Destroy()
	for len(e.pool) < e.opt.PoolSize {
		if err := e.refill(); err != nil {
			return err
		}
	}
	e.stats.Schemas = schemas
	e.stats.Policies = len(e.pols)
	e.stats.StartupTime = time.Since(t0)
	e.opt.Logf("engine ready in %v: librime %s, modules %v, schemas %v, %d policies", e.stats.StartupTime.Round(time.Millisecond), e.version, e.opt.Modules, schemas, len(e.pols))
	return nil
}

func (e *Engine) schemaList() ([]string, error) {
	cfg, err := e.lib.OpenConfig("default")
	if err != nil {
		return nil, fmt.Errorf("engine: %w (is the data deployed?)", err)
	}
	defer cfg.Close()
	var ids []string
	for i, n := 0, cfg.ListSize("schema_list"); i < n; i++ {
		if id, ok := cfg.String(fmt.Sprintf("schema_list/@%d/schema", i)); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("engine: empty schema_list")
	}
	return ids, nil
}

// refill adds one session to the pool. New sessions start on the first
// schema in schema_list.
func (e *Engine) refill() error {
	s, err := e.lib.CreateSession()
	if err != nil {
		return err
	}
	if e.realSchema != "" {
		s.ReadStatus(&e.st)
		if string(e.st.SchemaID) != e.realSchema && !s.SelectSchema(e.realSchema) {
			s.Destroy()
			return errors.New("engine: select active schema failed")
		}
	}
	e.addSession(s)
	e.warmSession(s)
	e.pool = append(e.pool, s)
	return nil
}

// warmOptions are the switches a session's warm-up composition runs with:
// Chinese mode, and every filter that loads data on first use.
var warmOptions = [...]struct {
	name string
	on   bool
}{{"ascii_mode", false}, {"emoji", true}, {"traditionalization", true}}

// warmSession runs one composition through a new session so its filters
// load their data now instead of on the user's first key: librime's
// simplifiers load their OpenCC dictionaries lazily, per session, which
// took 26 ms for 简繁 on the first key. The session's options are restored
// afterwards.
func (e *Engine) warmSession(s *rime.Session) {
	if len(e.opt.Warmup) == 0 {
		return
	}
	var saved [len(warmOptions)]bool
	for i, o := range warmOptions {
		saved[i] = s.Option(o.name)
		s.SetOption(o.name, o.on)
	}
	s.SimulateKeySequence(e.opt.Warmup[0])
	s.ReadContext(&e.ctx) // builds the menu, which runs the filters
	s.ClearComposition()
	for i, o := range warmOptions {
		s.SetOption(o.name, saved[i])
	}
}

func (e *Engine) loop() {
	for {
		var (
			r request
			a answer
		)
		select {
		case r = <-e.reqs:
		case a = <-e.rr.answers:
			e.applyRerank(&a)
			continue
		case <-e.rr.timer.C:
			e.expireHeld()
			continue
		default:
			// Idle: top up the session pool, one session per idle moment so
			// a key arriving meanwhile waits for at most one create_session.
			if len(e.pool) < e.opt.PoolSize {
				if err := e.refill(); err != nil {
					e.opt.Logf("refill session pool: %v", err)
				}
				continue
			}
			select {
			case r = <-e.reqs:
			case a = <-e.rr.answers:
				e.applyRerank(&a)
				continue
			case <-e.rr.timer.C:
				e.expireHeld()
				continue
			}
		}
		if r.kind == reqStop {
			return
		}
		e.handle(&r)
	}
}

// Stop stops the engine thread, closes all sessions and finalizes librime,
// which releases the user dictionary. It waits until that is done.
func (e *Engine) Stop() {
	select {
	case e.reqs <- request{kind: reqStop}:
	case <-e.stopped:
	}
	<-e.stopped
}

// Stats returns counters from the engine thread.
func (e *Engine) Stats() Stats { return e.stats1(false) }

// TakeStats returns the counters and resets the latency histogram.
func (e *Engine) TakeStats() Stats { return e.stats1(true) }

func (e *Engine) stats1(reset bool) Stats {
	ch := make(chan Stats, 1)
	var flags uint8
	if reset {
		flags = 1
	}
	select {
	case e.reqs <- request{kind: reqStats, stats: ch, flags: flags}:
	case <-e.stopped:
		return Stats{}
	}
	select {
	case s := <-ch:
		return s
	case <-e.stopped:
		return Stats{}
	}
}

// SetRerank turns 智能排序 on or off. Off, the filter passes candidates
// through and every app's context is forgotten.
func (e *Engine) SetRerank(on bool) {
	select {
	case e.reqs <- request{kind: reqRerank, on: on}:
	case <-e.stopped:
	}
}

// SetApps replaces the per-app rules. Remembered modes are kept while
// Remember stays on. Clients see a change of auto spacing with their next
// result.
func (e *Engine) SetApps(a AppModes) {
	select {
	case e.reqs <- request{kind: reqApps, apps: &a}:
	case <-e.stopped:
	}
}

// Native reports whether librime-shixin's components are loaded.
func (e *Engine) Native() bool {
	for _, m := range e.opt.Modules {
		if m == "shixin" {
			return true
		}
	}
	return false
}

func (e *Engine) session(c *Conn, ctx uint32) *rime.Session {
	if s := c.sessions[ctx]; s != nil {
		return s
	}
	if len(e.pool) == 0 {
		if err := e.refill(); err != nil {
			e.opt.Logf("create session: %v", err)
			return nil
		}
	}
	s := e.pool[len(e.pool)-1]
	e.pool = e.pool[:len(e.pool)-1]
	c.sessions[ctx] = s
	e.bindSession(c, ctx, s)
	return s
}

// handle serves one request on the engine thread.
func (e *Engine) handle(r *request) {
	if r.kind == reqMessage {
		e.handleMessage(r)
	} else {
		e.handleControl(r)
	}
}

// handleControl serves the engine's own requests: from its API, and a
// connection's start and end. Cold path.
func (e *Engine) handleControl(r *request) {
	c := r.conn
	switch r.kind {
	case reqStats:
		e.stats.Rerank = e.rr.stats
		e.stats.Rerank.Slow = e.rr.slow
		r.stats <- e.stats
		if r.flags != 0 {
			e.stats.KeyTime = perf.Hist{}
		}
	case reqRerank:
		e.setRerank(r.on)
	case reqApps:
		e.apps = *r.apps
		if !e.apps.Remember {
			clear(e.modes) // turning remembering off forgets
		}
	case reqHello:
		if e.migrating {
			c.kill()
			return
		}
		e.conns[c] = true
		c.sendWelcome()
	case reqPromote:
		r.answer <- e.promote(r.name)
	case reqMigrate:
		r.answer <- e.migrate(r.name)
	case reqDrained:
		if len(e.conns) != 0 {
			r.answer <- ErrClients
		} else {
			r.answer <- nil
		}
	case reqClosed:
		for ctx, s := range c.sessions {
			e.leaveApp(c.focus[ctx], s)
			e.destroySession(s)
			delete(c.sessions, ctx)
		}
		// A half-close is a sequence boundary: all earlier results are
		// ahead of this sentinel in the writer's queue.
		if !c.enqueue(nil) {
			c.kill()
		}
	case reqGone:
		delete(e.conns, c)
	}
}

// handleMessage serves a client's message. Keys take the hot path.
func (e *Engine) handleMessage(r *request) {
	c := r.conn
	if c.dead.Load() {
		return
	}
	if r.typ == ipc.TypeStatsRequest {
		c.sendStats(&e.stats, e.version, e.Native(), e.started)
		if r.on {
			e.stats.KeyTime = perf.Hist{}
		}
		return
	}
	if r.typ == ipc.TypeFocusOut {
		if s := c.sessions[r.ctx]; s != nil {
			e.flushHeld(s)
			e.leaveApp(c.focus[r.ctx], s)
			s.ClearComposition()
		}
		return
	}
	s := e.session(c, r.ctx)
	if s == nil {
		return
	}
	// A result still waiting for its order goes out first, as it is.
	e.flushHeld(s)
	handled := false
	switch r.typ {
	case ipc.TypeKey:
		e.stats.Keys++
		t0 := time.Now()
		if r.keysym != rerank.RefreshKeysym { // the engine's own key
			handled = s.ProcessKey(int(r.keysym), int(r.mods))
		}
		e.read(s)
		e.noteContext(c, r.ctx, s, r)
		if !e.hold(c, r, s, handled, t0) {
			e.send(c, r.ctx, r.seq, handled)
			e.stats.KeyTime.Add(time.Since(t0))
		}
		return
	case ipc.TypeFocusIn:
		secure := r.flags&ipc.FocusSecure != 0
		e.focus(c, r.ctx, s, r.name, secure)
		e.focusRerank(c, s, r.name, secure)
	case ipc.TypeSelect:
		handled = s.SelectCandidateOnCurrentPage(int(r.index))
	case ipc.TypeChangePage:
		handled = s.ChangePage(r.on)
	case ipc.TypeClear:
		s.ClearComposition()
	case ipc.TypeSetOption:
		s.SetOption(r.name, r.on)
	default:
		return
	}
	e.read(s)
	e.noteContext(c, r.ctx, s, r)
	e.send(c, r.ctx, r.seq, handled)
}

// focus applies the mode rules for an input context entering app (DESIGN
// §8). Secure fields (passwords) are English only, and the mode before them
// comes back after; the rules then apply as if the secure field hadn't been
// there. Cold path: it runs when the focus changes, not per key.
func (e *Engine) focus(c *Conn, ctx uint32, s *rime.Session, app string, secure bool) {
	f := c.focus[ctx] // f.app is the last app focused outside a secure field
	wasSecure := f.secure
	if secure {
		if !f.secure {
			e.leaveApp(f, s)
			f.secure, f.beforeSecure = true, s.Option("ascii_mode")
			s.ClearComposition()
			s.SetOption("ascii_mode", true)
		}
		c.focus[ctx] = f
		return
	}
	if f.secure {
		s.SetOption("ascii_mode", f.beforeSecure)
		f.secure = false
	}
	if app != f.app || c.focusedCtx != ctx {
		if previous := c.sessions[c.focusedCtx]; previous != nil {
			e.leaveApp(c.focus[c.focusedCtx], previous)
		}
		// Entering the secure field already saved the displaced app. Its
		// old mode may now be stale if that app reopened in another LRU slot.
		if app != f.app && !wasSecure {
			e.leaveApp(f, s)
		}
		english, ok := e.modes[app]
		if !ok || !e.apps.Remember {
			english, ok = e.apps.English[app]
		}
		if !ok && c.sharedModeKnown {
			english, ok = c.sharedASCII, true
		}
		if ok && english != s.Option("ascii_mode") {
			s.ClearComposition()
			s.SetOption("ascii_mode", english)
		}
		f.app = app
	}
	c.focusedCtx = ctx
	c.focus[ctx] = f
}

// leaveApp remembers the mode of the app an input context is leaving.
func (e *Engine) leaveApp(f focusState, s *rime.Session) {
	if e.apps.Remember && f.app != "" && !f.secure {
		e.modes[f.app] = s.Option("ascii_mode")
	}
}

// read takes the session's commit, menu and status into e.commit, e.ctx
// and e.st.
func (e *Engine) read(s *rime.Session) {
	e.commit, _ = s.ReadCommit(e.commit)
	s.ReadContext(&e.ctx)
	s.ReadStatus(&e.st)
}

// send sends what read took as the result of request seq. With read, it
// is the second half of the key path and must not allocate.
func (e *Engine) send(c *Conn, ctx, seq uint32, handled bool) {
	if c.focusedCtx == ctx && !c.focus[ctx].secure {
		c.sharedASCII, c.sharedModeKnown = e.st.IsASCIIMode, true
	}
	res := &e.res
	res.Ctx, res.Seq = ctx, seq
	res.Flags = 0
	if handled {
		res.Flags |= ipc.ResultHandled
	}
	if e.st.IsComposing {
		res.Flags |= ipc.ResultComposing
	}
	if e.st.IsASCIIMode {
		res.Flags |= ipc.ResultASCII
	}
	if e.st.IsFullShape {
		res.Flags |= ipc.ResultFullShape
	}
	if e.st.IsTraditional {
		res.Flags |= ipc.ResultTraditional
	}
	if e.st.IsASCIIPunct {
		res.Flags |= ipc.ResultASCIIPunct
	}
	if e.ctx.IsLastPage {
		res.Flags |= ipc.ResultLastPage
	}
	if e.apps.AutoSpace {
		if f := c.focus[ctx]; !f.secure && !e.apps.NoAutoSpace[f.app] {
			res.Flags |= ipc.ResultAutoSpace
		}
	}
	res.Policy = e.policyFor()
	res.Commit = e.commit
	res.Preedit = e.ctx.Preedit
	res.Cursor = uint32(e.ctx.CursorPos)
	res.SelStart, res.SelEnd = uint32(e.ctx.SelStart), uint32(e.ctx.SelEnd)
	res.PageNo = uint16(e.ctx.PageNo)
	res.Highlighted = uint16(e.ctx.Highlighted)
	res.Candidates = res.Candidates[:0]
	for i := range e.ctx.Candidates {
		res.Candidates = append(res.Candidates, ipc.Candidate{Text: e.ctx.Candidates[i].Text, Comment: e.ctx.Candidates[i].Comment})
	}
	res.Labels = nil

	buf := c.buffer()
	if buf == nil {
		e.stats.Dropped++
		c.kill()
		return
	}
	// Send the policy for the next key, and the one the client switches to
	// speculatively when a key starts composing (policy.Policy.Composing).
	for _, id := range [2]uint16{e.pols[res.Policy].Composing, res.Policy} {
		if !c.sent[id] {
			buf = ipc.AppendPolicy(buf, &e.pols[id])
			c.sent[id] = true
		}
	}
	buf = res.Append(buf)
	if !c.enqueue(buf) {
		e.stats.Dropped++
		c.kill()
	}
}

// policyFor picks the policy for the next key from the state just read.
func (e *Engine) policyFor() uint16 {
	sp := e.schemas[string(e.st.SchemaID)]
	if sp == nil || e.st.IsFullShape {
		return conservativeID
	}
	punct := 0
	if e.st.IsASCIIPunct {
		punct = 1
	}
	switch {
	case e.st.IsComposing:
		return sp.composing[punct]
	case e.st.IsASCIIMode:
		return sp.ascii[punct]
	}
	return sp.chinese[punct]
}

// DefaultWarmup is a short typing script for pinyin schemas.
var DefaultWarmup = []string{"nihao", "zhongguo", "shurufa", "jintiantianqi", "women", "shixin"}

// Lock takes Shixin's exclusive engine lock for a Rime user dir (DESIGN
// §3.5, single owner of the user dictionary). Only the holder may create
// sessions with a user dictionary on that dir. Validated Standby warm-up
// is the only exception. It fails at once if another engine holds
// it; release by calling the returned function or exiting.
func Lock(userDataDir string) (release func(), err error) {
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		return nil, fmt.Errorf("engine: lock: %w", err)
	}
	return lockFile(lockPath(userDataDir))
}

func lockPath(userDataDir string) string {
	return filepath.Join(userDataDir, "shixin-engine.lock")
}
