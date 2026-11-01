package ai

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"

	"shixin/internal/ipc"
	"shixin/internal/rerank"
)

// Model is what a Server needs from a Scorer.
type Model interface {
	Prefill(app int, rev uint32, text []byte) error
	Warm(app int, rev uint32) bool
	Score(app int, cands []Candidate, p rerank.Params, perm []uint8) ([]uint8, error)
}

// Server answers one engine at a time over the engine↔shixin-ai protocol
// (rerank.Hello, Context, Score → Rerank). A new connection replaces the
// previous one: after a hot swap, the new engine generation connects.
type Server struct {
	m    Model
	logf func(string, ...any)

	mu  sync.Mutex
	cur net.Conn
}

// NewServer returns a server for m.
func NewServer(m Model, logf func(string, ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{m: m, logf: logf}
}

// Serve accepts engines on l until l is closed. Connections are served one
// at a time on the calling goroutine, which owns the model; a newer one
// closes the current one.
func (s *Server) Serve(l net.Listener) error {
	conns := make(chan net.Conn, 1)
	errc := make(chan error, 1)
	go func() {
		defer close(conns)
		for {
			c, err := ipc.Accept(l)
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					errc <- err
				}
				return
			}
			s.mu.Lock()
			if s.cur != nil {
				s.cur.Close()
			}
			s.cur = c
			s.mu.Unlock()
			conns <- c
		}
	}()
	for c := range conns {
		if err := s.ServeConn(c); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
			s.logf("engine connection: %v", err)
		}
	}
	select {
	case err := <-errc:
		return err
	default:
		return nil
	}
}

// message is a decoded request, copied out of the reader's buffer.
type message struct {
	typ   ipc.Type
	ctx   rerank.Context
	score rerank.Score
}

// ServeConn serves one engine connection until it closes.
func (s *Server) ServeConn(c net.Conn) error {
	defer c.Close()
	rd := ipc.NewReader(c, 8192)
	typ, payload, err := rd.Next()
	if err != nil {
		return err
	}
	var h rerank.Hello
	if typ != rerank.TypeHello || h.Decode(payload) != nil {
		return rerank.ErrMalformed
	}
	if _, err := c.Write((&rerank.Status{Version: rerank.WireVersion, Ready: true}).Append(nil)); err != nil {
		return err
	}
	in := make(chan message, 256)
	rerr := make(chan error, 1)
	go func() {
		defer close(in)
		for {
			typ, payload, err := rd.Next()
			if err != nil {
				rerr <- err
				return
			}
			m := message{typ: typ}
			switch typ {
			case rerank.TypeContext:
				if m.ctx.Decode(payload) != nil {
					continue
				}
				m.ctx.Text = bytes.Clone(m.ctx.Text)
			case rerank.TypeScore:
				if m.score.Decode(payload) != nil {
					continue
				}
				for i := range m.score.Entries {
					m.score.Entries[i].Text = bytes.Clone(m.score.Entries[i].Text)
				}
			default:
				continue // a newer engine's message
			}
			in <- m
		}
	}()
	w := worker{s: s, c: c}
	for m := range in {
		w.take(m)
		// Newest wins: take everything that has arrived before acting.
		for more := true; more; {
			select {
			case m, ok := <-in:
				if !ok {
					more = false
					break
				}
				w.take(m)
			default:
				more = false
			}
		}
		if err := w.run(); err != nil {
			return err
		}
	}
	return <-rerr
}

// worker holds the requests taken since the last run.
type worker struct {
	s        *Server
	c        net.Conn
	contexts []rerank.Context // latest per app
	scores   []rerank.Score   // latest per session, in arrival order
	cands    []Candidate
	perm     [rerank.MaxOptions]uint8
	buf      []byte
}

func (w *worker) take(m message) {
	switch m.typ {
	case rerank.TypeContext:
		for i := range w.contexts {
			if w.contexts[i].App == m.ctx.App {
				w.contexts[i] = m.ctx
				return
			}
		}
		w.contexts = append(w.contexts, m.ctx)
	case rerank.TypeScore:
		for i := range w.scores {
			if w.scores[i].Session == m.score.Session {
				w.scores = append(w.scores[:i], w.scores[i+1:]...)
				break
			}
		}
		w.scores = append(w.scores, m.score)
	}
}

// run answers the score requests, prefilling a context first when a
// request needs it, then prefills the remaining contexts.
func (w *worker) run() error {
	m := w.s.m
	for _, sc := range w.scores {
		app := int(sc.App)
		if !m.Warm(app, sc.AppRev) {
			i := w.context(sc.App)
			if i < 0 || w.contexts[i].Rev != sc.AppRev {
				continue // stale: a newer context is coming or came
			}
			if err := m.Prefill(app, sc.AppRev, w.contexts[i].Text); err != nil {
				return err
			}
			w.contexts = append(w.contexts[:i], w.contexts[i+1:]...)
		}
		p, ok := Strengths[sc.Strength]
		if !ok {
			continue
		}
		w.cands = w.cands[:0]
		for _, e := range sc.Entries {
			w.cands = append(w.cands, Candidate{Text: e.Text, Weight: e.Weight, Flags: e.Flags})
		}
		perm, err := m.Score(app, w.cands, p, w.perm[:])
		if err != nil {
			return err
		}
		w.buf = (&rerank.Rerank{Session: sc.Session, ExportSeq: sc.ExportSeq, Perm: perm}).Append(w.buf[:0])
		if _, err := w.c.Write(w.buf); err != nil {
			return err
		}
	}
	w.scores = w.scores[:0]
	for _, c := range w.contexts {
		if err := m.Prefill(int(c.App), c.Rev, c.Text); err != nil {
			return err
		}
	}
	w.contexts = w.contexts[:0]
	return nil
}

func (w *worker) context(app uint8) int {
	for i := range w.contexts {
		if w.contexts[i].App == app {
			return i
		}
	}
	return -1
}
