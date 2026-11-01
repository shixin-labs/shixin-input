package engine

import (
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/rime"
	"shixin/internal/settings"
)

// queueDepth bounds the frames in flight to one client. A client that falls
// this far behind is stuck, and is disconnected rather than ever blocking
// the engine thread.
const queueDepth = 64

// Conn is one client connection.
type Conn struct {
	e    *Engine
	c    net.Conn
	out  chan []byte // frames for the writer
	free chan []byte // recycled frame buffers
	done chan struct{}
	dead atomic.Bool
	once sync.Once

	// Engine thread only.
	sessions        map[uint32]*rime.Session
	focus           map[uint32]focusState
	sent            []bool // policies this client has
	focusedCtx      uint32
	sharedASCII     bool // no-Remember mode shared by this front's contexts
	sharedModeKnown bool
}

// focusState is what an input context last got focus in.
type focusState struct {
	app          string
	secure       bool
	beforeSecure bool // ascii_mode before the secure field
}

// Serve accepts clients on l until l is closed. Each connection gets a
// reader and a writer goroutine; all librime work happens on the engine
// thread.
func (e *Engine) Serve(l net.Listener) error {
	for {
		c, err := ipc.Accept(l)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		e.ServeConn(c)
	}
}

// ServeConn serves one connection in the background.
func (e *Engine) ServeConn(c net.Conn) *Conn {
	conn := &Conn{
		e:        e,
		c:        c,
		out:      make(chan []byte, queueDepth),
		free:     make(chan []byte, queueDepth),
		done:     make(chan struct{}),
		sessions: map[uint32]*rime.Session{},
		focus:    map[uint32]focusState{},
		sent:     make([]bool, len(e.pols)),
	}
	for i := 0; i < queueDepth; i++ {
		conn.free <- make([]byte, 0, 2048)
	}
	go conn.write()
	go conn.read()
	return conn
}

func (c *Conn) kill() {
	c.once.Do(func() {
		c.dead.Store(true)
		c.c.Close()
		close(c.done)
	})
}

// buffer takes a free frame buffer, or nil if the client is stuck.
func (c *Conn) buffer() []byte {
	select {
	case b := <-c.free:
		return b[:0]
	default:
		return nil
	}
}

// enqueue hands a frame to the writer without blocking.
func (c *Conn) enqueue(b []byte) bool {
	if c.dead.Load() {
		return false
	}
	select {
	case c.out <- b:
		return true
	default:
		return false
	}
}

func (c *Conn) sendWelcome() {
	if b := c.buffer(); b != nil {
		w := ipc.Welcome{Version: ipc.Version}
		c.enqueue(w.Append(b))
	}
}

// sendStats answers a StatsRequest. Cold path: it may allocate.
func (c *Conn) sendStats(st *Stats, librime string, native bool, started time.Time) {
	b := c.buffer()
	if b == nil {
		return
	}
	m := ipc.Stats{
		Keys:      st.Keys,
		Dropped:   st.Dropped,
		StartupMS: uint32(st.StartupTime / time.Millisecond),
		UptimeS:   uint32(time.Since(started) / time.Second),
		Librime:   []byte(librime),
		Native:    native,
		Schemas:   []byte(strings.Join(st.Schemas, ",")),
		KeyTime:   st.KeyTime,
	}
	if rr := &c.e.rr; rr.attached {
		r := &rr.stats
		m.Rerank = &ipc.RerankStats{On: rr.on, Ready: rr.scorer.Ready(), Slow: rr.slow,
			Requests: r.Requests, Applied: r.Applied, Timeouts: r.Timeouts,
			Superseded: r.Superseded, Dropped: r.Dropped, Rejected: r.Rejected, Wait: r.Wait}
	}
	c.enqueue(m.Append(b))
}

func (c *Conn) write() {
	defer func() {
		select {
		case c.e.reqs <- request{kind: reqGone, conn: c}:
		case <-c.e.stopped:
		}
	}()
	for {
		select {
		case b := <-c.out:
			if b == nil {
				c.kill()
				return
			}
			if _, err := c.c.Write(b); err != nil {
				c.kill()
				return
			}
			c.free <- b
		case <-c.done:
			return
		}
	}
}

func (c *Conn) read() {
	graceful := false
	defer func() {
		if !graceful {
			c.kill()
		}
		select {
		case c.e.reqs <- request{kind: reqClosed, conn: c}:
		case <-c.e.stopped:
		}
	}()
	r := ipc.NewReader(c.c, 4096)
	var (
		hello   ipc.Hello
		key     ipc.Key
		focus   ipc.FocusIn
		out     ipc.FocusOut
		sel     ipc.Select
		page    ipc.ChangePage
		clr     ipc.Clear
		opt     ipc.SetOption
		stats   ipc.StatsRequest
		helloOK bool
	)
	for {
		typ, payload, err := r.Next()
		if err != nil {
			graceful = errors.Is(err, io.EOF)
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !errors.Is(err, os.ErrDeadlineExceeded) {
				c.e.opt.Logf("client read: %v", err)
			}
			return
		}
		req := request{kind: reqMessage, typ: typ, conn: c}
		switch typ {
		case ipc.TypeHello:
			if hello.Decode(payload) != nil {
				return
			}
			helloOK = true
			req.kind = reqHello
		case ipc.TypeKey:
			if key.Decode(payload) != nil {
				return
			}
			req.ctx, req.seq, req.keysym, req.mods, req.flags = key.Ctx, key.Seq, key.Keysym, key.Mods, key.Flags
		case ipc.TypeFocusIn:
			if focus.Decode(payload) != nil {
				return
			}
			req.ctx, req.flags, req.name = focus.Ctx, focus.Flags, settings.AppKey(string(focus.App))
		case ipc.TypeFocusOut:
			if out.Decode(payload) != nil {
				return
			}
			req.ctx = out.Ctx
		case ipc.TypeSelect:
			if sel.Decode(payload) != nil {
				return
			}
			req.ctx, req.seq, req.index = sel.Ctx, sel.Seq, sel.Index
		case ipc.TypeChangePage:
			if page.Decode(payload) != nil {
				return
			}
			req.ctx, req.seq, req.on = page.Ctx, page.Seq, page.Backward
		case ipc.TypeClear:
			if clr.Decode(payload) != nil {
				return
			}
			req.ctx, req.seq = clr.Ctx, clr.Seq
		case ipc.TypeSetOption:
			if opt.Decode(payload) != nil {
				return
			}
			req.ctx, req.seq, req.on, req.name = opt.Ctx, opt.Seq, opt.On, string(opt.Name)
		case ipc.TypeStatsRequest:
			if stats.Decode(payload) != nil {
				return
			}
			req.on = stats.Reset
		default:
			continue // a newer client's message; ignore
		}
		if !helloOK {
			return // Hello must come first
		}
		select {
		case c.e.reqs <- req:
		case <-c.e.stopped:
			return
		}
	}
}
