package main

import (
	"bytes"
	"net"
	"os"
	"time"

	"shixin/internal/ipc"
)

// pollStats asks the engine for its counters every second while the
// performance or input page shows, over a connection of its own
// (ipc.KindSettings, which opens no input context). It also rechecks
// 智能排序's model and shixin-ai's state.
func (a *app) pollStats() {
	var (
		conn net.Conn
		r    *ipc.Reader
		buf  = make([]byte, 0, 64)
	)
	drop := func() {
		if conn != nil {
			conn.Close()
			conn = nil
		}
	}
	for ; ; time.Sleep(time.Second) {
		if !a.statsShown.Load() {
			drop()
			continue
		}
		update(a, a.refreshModel)
		if conn == nil {
			conn, r = dialEngine()
			if conn == nil {
				update(a, func() { a.stats = nil })
				continue
			}
		}
		st, err := requestStats(conn, r, buf, a.resetHist.Swap(false))
		if err != nil {
			drop()
			update(a, func() { a.stats = nil })
			continue
		}
		now := time.Now()
		update(a, func() { a.stats, a.statsAt = st, now })
	}
}

func dialEngine() (net.Conn, *ipc.Reader) {
	dir, err := ipc.RuntimeDir()
	if err != nil {
		return nil, nil
	}
	c, err := ipc.Dial(ipc.EnginePath(dir), 300*time.Millisecond)
	if err != nil {
		return nil, nil
	}
	hello := ipc.Hello{Version: ipc.Version, Kind: ipc.KindSettings, PID: uint32(os.Getpid()), App: []byte("shixin-settings")}
	r := ipc.NewReader(c, 4096)
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write(hello.Append(nil)); err != nil {
		c.Close()
		return nil, nil
	}
	if typ, _, err := r.Next(); err != nil || typ != ipc.TypeWelcome {
		c.Close()
		return nil, nil
	}
	return c, r
}

func requestStats(c net.Conn, r *ipc.Reader, buf []byte, reset bool) (*ipc.Stats, error) {
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write((&ipc.StatsRequest{Reset: reset}).Append(buf[:0])); err != nil {
		return nil, err
	}
	for {
		typ, payload, err := r.Next()
		if err != nil {
			return nil, err
		}
		if typ != ipc.TypeStats {
			continue
		}
		st := &ipc.Stats{}
		if err := st.Decode(payload); err != nil {
			return nil, err
		}
		// Decoded byte fields alias the reader's buffer.
		st.Librime, st.Schemas = bytes.Clone(st.Librime), bytes.Clone(st.Schemas)
		return st, nil
	}
}
