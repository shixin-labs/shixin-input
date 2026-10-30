package rerank

import (
	"errors"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"shixin/internal/ipc"
)

// remoteQueue bounds the frames in flight to shixin-ai. A request that
// finds no free buffer is dropped: an order that late would be dropped by
// the engine anyway.
const remoteQueue = 32

// Remote is the engine's Scorer for a shixin-ai process (DESIGN §7.2). It
// connects to shixin-ai's socket in the background, sends contexts and
// score requests without ever blocking the engine thread, and hands the
// orders it receives to the answer function given to Run.
//
// Context and Score are called on the engine thread and don't allocate
// once warm. Each app's latest context is replayed after a reconnect, so a
// restarted shixin-ai becomes warm again.
type Remote struct {
	path     string
	strength atomic.Uint32
	logf     func(string, ...any)
	out      chan []byte
	free     chan []byte
	ready    atomic.Bool

	mu       sync.Mutex
	contexts [Rings][]byte // the last Context frame of each app
}

// SocketPath is shixin-ai's socket in Shixin's runtime directory.
func SocketPath(dir string) string { return filepath.Join(dir, "ai.sock") }

// NewRemote returns a Remote for the shixin-ai socket at path, asking for
// orders with strength (StrengthCautious, StrengthStandard).
func NewRemote(path string, strength uint8, logf func(string, ...any)) *Remote {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	r := &Remote{path: path, logf: logf,
		out: make(chan []byte, remoteQueue), free: make(chan []byte, remoteQueue)}
	r.strength.Store(uint32(strength))
	for range remoteQueue {
		r.free <- make([]byte, 0, 8192)
	}
	for i := range r.contexts {
		r.contexts[i] = make([]byte, 0, 1024)
	}
	return r
}

// SetStrength changes the strength asked for. Any goroutine.
func (r *Remote) SetStrength(strength uint8) { r.strength.Store(uint32(strength)) }

// Ready reports whether shixin-ai is connected and can score.
func (r *Remote) Ready() bool { return r.ready.Load() }

// Context sends an app's text and keeps it for a reconnect.
func (r *Remote) Context(app int, rev uint32, text []byte) {
	if app < 0 || app >= Rings {
		return
	}
	m := Context{App: uint8(app), Rev: rev, Text: text}
	r.mu.Lock()
	r.contexts[app] = m.Append(r.contexts[app][:0])
	r.mu.Unlock()
	if !r.ready.Load() {
		return
	}
	select {
	case b := <-r.free:
		r.queue(m.Append(b[:0]))
	default:
	}
}

// Score sends a request; without a ready shixin-ai it does nothing.
func (r *Remote) Score(req *Request) {
	if !r.ready.Load() {
		return
	}
	select {
	case b := <-r.free:
		b = AppendScore(b[:0], req, uint8(r.strength.Load()))
		r.queue(b)
	default:
	}
}

func (r *Remote) queue(b []byte) {
	select {
	case r.out <- b:
	default:
		r.free <- b
	}
}

// Run keeps a connection to shixin-ai until stop is closed, passing orders
// to answer (Engine.Rerank). It retries with backoff while shixin-ai isn't
// running.
func (r *Remote) Run(stop <-chan struct{}, answer func(session int, exportSeq uint32, perm []uint8)) {
	backoff := 50 * time.Millisecond
	for {
		c, err := ipc.Dial(r.path, time.Second)
		if err == nil {
			backoff = 50 * time.Millisecond
			err = r.serve(c, stop, answer)
		}
		select {
		case <-stop:
			return
		default:
		}
		if err != nil && c != nil {
			r.logf("rerank: shixin-ai connection: %v", err)
		}
		select {
		case <-stop:
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 2*time.Second)
	}
}

var errNotReady = errors.New("shixin-ai cannot score")

func (r *Remote) serve(c net.Conn, stop <-chan struct{}, answer func(int, uint32, []uint8)) error {
	defer c.Close()
	if _, err := c.Write((&Hello{Version: WireVersion}).Append(nil)); err != nil {
		return err
	}
	rd := ipc.NewReader(c, 4096)
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, payload, err := rd.Next()
	if err != nil {
		return err
	}
	var st Status
	if typ != TypeStatus || st.Decode(payload) != nil {
		return ErrMalformed
	}
	if !st.Ready {
		return errNotReady
	}
	c.SetReadDeadline(time.Time{})
	// Replay the contexts, then start taking requests.
	r.mu.Lock()
	var replay []byte
	for _, f := range r.contexts {
		replay = append(replay, f...)
	}
	r.mu.Unlock()
	if _, err := c.Write(replay); err != nil {
		return err
	}
	r.ready.Store(true)
	defer r.ready.Store(false)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-stop:
		case <-done:
		}
		c.Close()
	}()
	werr := make(chan error, 1)
	go func() {
		for {
			select {
			case b := <-r.out:
				_, err := c.Write(b)
				r.free <- b
				if err != nil {
					werr <- err
					return
				}
			case <-done:
				return
			}
		}
	}()
	var m Rerank
	for {
		typ, payload, err := rd.Next()
		if err != nil {
			select {
			case err = <-werr:
			default:
			}
			return err
		}
		if typ == TypeRerank && m.Decode(payload) == nil {
			answer(int(m.Session), m.ExportSeq, m.Perm)
		}
	}
}
