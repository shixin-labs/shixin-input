package ipc

import (
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// go-winio allocates an operation and a channel for every Read and Write.
// pipeConn drives the same overlapped handle with one preallocated
// operation per direction, so a call allocates nothing: the engine's
// connection to shixin-ai runs on the engine thread's budget.
//
// The handle stays registered with go-winio's completion port, which
// delivers each finished operation by sending on the channel that follows
// the OVERLAPPED in its ioOperation (go-winio v0.6.4, file.go). ioOp
// mirrors that two-field prefix; every Windows pipe test exercises it, so a
// go-winio upgrade that changes the layout fails there rather than in the
// field. Anything after ch is ours.
type ioResult struct {
	bytes uint32
	err   error
}

type ioOp struct {
	o  windows.Overlapped
	ch chan ioResult // buffered: the completion port never waits for us
	n  uint32        // bytes of an operation that completed synchronously
}

// pipeSide is one direction of a pipeConn.
type pipeSide struct {
	mu sync.Mutex // one operation at a time
	op ioOp
	dl pipeDeadline
}

// pipeDeadline cancels the operation in flight when a deadline passes.
// Setting a deadline reuses one timer and doesn't allocate.
type pipeDeadline struct {
	h       windows.Handle
	mu      sync.Mutex
	timer   *time.Timer
	when    time.Time // zero without a deadline
	expired bool
	pending *windows.Overlapped // the operation in flight, if any
}

// pipeConn wraps a go-winio message-mode pipe connection. It reproduces
// go-winio's byte-stream behaviour: a zero-length message is the peer's
// half-close and reads as io.EOF, and CloseWrite sends one.
type pipeConn struct {
	net.Conn // addresses, and closing the handle
	flush    interface{ Flush() error }
	h        windows.Handle

	closeMu sync.RWMutex
	closing atomic.Bool
	wg      sync.WaitGroup // operations in flight

	rd, wr      pipeSide
	readEOF     bool // guarded by rd.mu
	writeClosed bool // guarded by wr.mu
}

// newPipeConn wraps c, a connection returned by go-winio. Anything else is
// returned as it is.
func newPipeConn(c net.Conn) net.Conn {
	f, ok := c.(interface {
		Fd() uintptr
		Flush() error
		CloseWrite() error
	})
	if !ok {
		return c
	}
	pc := &pipeConn{Conn: c, flush: f, h: windows.Handle(f.Fd())}
	for _, s := range []*pipeSide{&pc.rd, &pc.wr} {
		s.op.ch = make(chan ioResult, 1)
		s.dl.h = pc.h
	}
	return pc
}

// Fd returns the pipe handle.
func (c *pipeConn) Fd() uintptr { return uintptr(c.h) }

// do runs one overlapped operation on s, which the caller has locked.
func (c *pipeConn) do(s *pipeSide, write bool, b []byte) (int, error) {
	c.closeMu.RLock()
	if c.closing.Load() {
		c.closeMu.RUnlock()
		return 0, os.ErrClosed
	}
	c.wg.Add(1)
	c.closeMu.RUnlock()
	defer c.wg.Done()

	op, d := &s.op, &s.dl
	d.mu.Lock()
	if d.expired {
		d.mu.Unlock()
		return 0, winio.ErrTimeout
	}
	d.pending = &op.o
	d.mu.Unlock()

	op.o = windows.Overlapped{}
	op.n = 0
	var err error
	if write {
		err = windows.WriteFile(c.h, b, &op.n, &op.o)
	} else {
		err = windows.ReadFile(c.h, b, &op.n, &op.o)
	}
	if err != windows.ERROR_IO_PENDING {
		d.finish()
		return int(op.n), err
	}

	// A deadline or Close that came before the operation was issued
	// couldn't cancel it.
	d.mu.Lock()
	late := d.expired
	d.mu.Unlock()
	if late || c.closing.Load() {
		windows.CancelIoEx(c.h, &op.o)
	}
	r := <-op.ch
	d.finish()
	err = r.err
	if err == windows.ERROR_OPERATION_ABORTED {
		// Only Close and an expired deadline cancel an operation.
		if c.closing.Load() {
			err = os.ErrClosed
		} else {
			err = winio.ErrTimeout
		}
	}
	return int(r.bytes), err
}

// Read reads from the pipe.
func (c *pipeConn) Read(b []byte) (int, error) {
	s := &c.rd
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.readEOF {
		return 0, io.EOF
	}
	n, err := c.do(s, false, b)
	switch err {
	case nil:
		if n == 0 && len(b) != 0 {
			// A zero-length message: the peer's CloseWrite.
			c.readEOF = true
			return 0, io.EOF
		}
		return n, nil
	case windows.ERROR_BROKEN_PIPE:
		c.readEOF = true
		return 0, io.EOF
	case windows.ERROR_MORE_DATA:
		// The message goes on in the next read; the stream doesn't care.
		return n, nil
	}
	return n, err
}

// Write writes one message. An empty write does nothing: it is reserved
// for CloseWrite.
func (c *pipeConn) Write(b []byte) (int, error) {
	s := &c.wr
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.writeClosed {
		return 0, io.ErrClosedPipe
	}
	if len(b) == 0 {
		return 0, nil
	}
	return c.do(s, true, b)
}

// CloseWrite sends the zero-length message that reads as io.EOF.
func (c *pipeConn) CloseWrite() error {
	s := &c.wr
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.writeClosed {
		return io.ErrClosedPipe
	}
	if err := c.flush.Flush(); err != nil {
		return err
	}
	if _, err := c.do(s, true, nil); err != nil {
		return err
	}
	c.writeClosed = true
	return nil
}

// Close cancels the operations in flight, waits for them and closes the
// handle.
func (c *pipeConn) Close() error {
	c.closeMu.Lock()
	c.closing.Store(true)
	c.closeMu.Unlock()
	windows.CancelIoEx(c.h, nil)
	c.wg.Wait()
	c.rd.dl.stop()
	c.wr.dl.stop()
	return c.Conn.Close()
}

func (c *pipeConn) SetDeadline(t time.Time) error {
	c.rd.dl.set(t)
	c.wr.dl.set(t)
	return nil
}

func (c *pipeConn) SetReadDeadline(t time.Time) error {
	c.rd.dl.set(t)
	return nil
}

func (c *pipeConn) SetWriteDeadline(t time.Time) error {
	c.wr.dl.set(t)
	return nil
}

// finish ends the operation in flight.
func (d *pipeDeadline) finish() {
	d.mu.Lock()
	d.pending = nil
	d.mu.Unlock()
}

// set changes the deadline; the zero time removes it. A deadline in the
// past fails the operation in flight and every later one until it changes.
func (d *pipeDeadline) set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expired = false
	d.when = t
	if d.timer != nil {
		d.timer.Stop()
	}
	if t.IsZero() {
		return
	}
	dur := time.Until(t)
	if dur <= 0 {
		d.expire()
		return
	}
	if d.timer == nil {
		d.timer = time.AfterFunc(dur, d.fire)
	} else {
		d.timer.Reset(dur)
	}
}

// fire is the timer's function. A timer that was stopped too late to
// prevent a callback leaves one for a deadline that has since moved: it
// finds the new deadline still ahead and does nothing.
func (d *pipeDeadline) fire() {
	d.mu.Lock()
	if !d.when.IsZero() && time.Until(d.when) < time.Millisecond {
		d.expire()
	}
	d.mu.Unlock()
}

// expire is called with d.mu held.
func (d *pipeDeadline) expire() {
	d.expired = true
	if d.pending != nil {
		windows.CancelIoEx(d.h, d.pending)
	}
}

func (d *pipeDeadline) stop() {
	d.mu.Lock()
	d.when = time.Time{}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.mu.Unlock()
}
