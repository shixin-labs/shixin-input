package client

import (
	"fmt"
	"net"
	"runtime"
	"strconv"
	"testing"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

// No connection goroutine drains results for this fixture: the tests control
// exactly which replies have arrived before the next main-thread key event.
func ownershipClient(t *testing.T) (*Client, *conn, *recorder) {
	t.Helper()
	a := &recorder{}
	c := New(Options{}, a)
	local, peer := net.Pipe()
	cn := &conn{c: local, out: make(chan outMsg, 128)}
	c.conn.Store(cn)
	chinese, composing := fakePolicies()
	english := &policy.Policy{ID: 3, Composing: 2, Notify: policy.NotifyShift | policy.NotifyControl}
	c.pols.Store(&map[uint16]*policy.Policy{1: chinese, 2: composing, 3: english})
	c.cur, c.spec, c.connected = english, english, true
	t.Cleanup(func() { c.timer.Stop(); local.Close(); peer.Close() })
	return c, cn, a
}

func TestPassedPrintableOwnership(t *testing.T) {
	for _, modifier := range []string{"shift", "control"} {
		for key := uint32(0x20); key <= 0x7e; key++ {
			t.Run(fmt.Sprintf("%s/%02x", modifier, key), func(t *testing.T) {
				c, cn, a := ownershipClient(t)
				c.shiftDown, c.ctrlDown = modifier == "shift", modifier == "control"
				if c.HandleKey(key, 0) {
					t.Fatal("the engine took a key owned by the app")
				}
				if len(cn.out) != 1 {
					t.Fatal("held modifier did not receive exactly one notification")
				}
				m := <-cn.out
				wantRelease := key < '0' || key > '9'
				if m.typ != ipc.TypeKey || m.key.Keysym != key || m.key.Flags != ipc.KeyNotify || (m.key.Mods&policy.ReleaseMask != 0) != wantRelease {
					t.Fatalf("wrong notification for passed key: %+v", m.key)
				}
				// A declined notification must never repair a character the app
				// already inserted. Digits retain key-downs for numeric history.
				c.applyResult(&ipc.Result{Ctx: 1, Seq: m.key.Seq, Policy: 3, Flags: ipc.ResultASCII})
				if c.keys[m.key.Seq%uint32(len(c.keys))].eaten || a.commits() != "" || c.stats.Repairs != 0 {
					t.Fatal("passed key acquired a second owner")
				}
			})
		}
	}
}

func TestQueuedRepliesChooseLatestMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		initial uint16
		replies []uint16
		ack     bool
		wantEat bool
	}{
		{"english-to-chinese", 3, []uint16{1}, false, true},
		{"chinese-to-english", 1, []uint16{3}, false, false},
		{"coalesced-chinese", 3, []uint16{1, 3, 1}, false, true},
		{"coalesced-english", 1, []uint16{3, 1, 3}, false, false},
		{"ack-ends-speculation-in-chinese", 3, []uint16{1}, true, true},
		{"ack-ends-speculation-in-english", 1, []uint16{3}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, cn, a := ownershipClient(t)
			c.cur, c.spec = c.policy(tc.initial), c.policy(tc.initial)
			if tc.ack {
				c.seq = 1
				c.arrived.Store(1)
			}
			for _, id := range tc.replies {
				flags := uint16(ipc.ResultHandled)
				if id == 3 {
					flags |= ipc.ResultASCII
				}
				if !c.push(evResult, &ipc.Result{Ctx: 1, Seq: c.seq, Policy: id, Flags: flags}) {
					t.Fatal("fixture queue full")
				}
			}
			if got := c.HandleKey('x', 0); got != tc.wantEat {
				t.Fatalf("ownership=%v, want=%v", got, tc.wantEat)
			}
			if tc.wantEat {
				if len(cn.out) != 1 {
					t.Fatal("eaten key was not sent exactly once")
				}
				m := <-cn.out
				if m.key.Flags != 0 || !c.keys[m.key.Seq%uint32(len(c.keys))].eaten {
					t.Fatal("Chinese key was treated as a passed notification")
				}
			} else if len(cn.out) != 0 {
				t.Fatal("English key was sent to the engine")
			}
			if a.commits() != "" || c.stats.Timeouts != 0 || c.stats.Asked != 0 || c.stats.Repairs != 0 {
				t.Fatal("a queued mode reply caused text insertion or a wait")
			}
		})
	}
}

func TestQueuedRepliesRespectFocusFences(t *testing.T) {
	for _, tc := range []struct {
		name    string
		secure  bool
		fence   bool
		replies []ipc.Result
	}{
		{"previous-context", false, false, []ipc.Result{{Ctx: 2, Policy: 1, Commit: []byte("old field")}}},
		{"cleared-composition", false, true, []ipc.Result{{Ctx: 1, Seq: 9, Policy: 1, Commit: []byte("late")}, {Ctx: 1, Seq: 10, Policy: 1}, {Ctx: 1, Policy: 3, Flags: ipc.ResultASCII}}},
		{"secure-field", true, false, []ipc.Result{{Ctx: 1, Policy: 1, Commit: []byte("ordinary field")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, cn, a := ownershipClient(t)
			c.secure = tc.secure
			if tc.fence {
				c.seq, c.clearSeq, c.contextFence, c.dropZero = 10, 10, 10, true
				c.arrived.Store(10)
			}
			for i := range tc.replies {
				if !c.push(evResult, &tc.replies[i]) {
					t.Fatal("fixture queue full")
				}
			}
			if c.HandleKey('x', 0) {
				t.Fatal("stale or secure reply took the app's key")
			}
			if len(cn.out) != 0 || a.commits() != "" || a.preedit != "" || c.composing {
				t.Fatal("output crossed the focus fence")
			}
			if tc.secure && c.head.Load() == c.tail.Load() {
				t.Fatal("secure key drained ordinary-field output")
			}
		})
	}
}

func TestQueuedDisconnectCannotEatNextKey(t *testing.T) {
	for _, kind := range []eventKind{evDisconnected, evUnavailable, evMigrate} {
		t.Run(strconv.Itoa(int(kind)), func(t *testing.T) {
			c, cn, _ := ownershipClient(t)
			c.cur, c.spec = c.policy(1), c.policy(1)
			c.push(kind, nil)
			if c.HandleKey('x', 0) {
				t.Fatal("disconnected client ate a spelling key")
			}
			for len(cn.out) != 0 {
				if (<-cn.out).typ == ipc.TypeKey {
					t.Fatal("sent a key after disconnect/migration")
				}
			}
		})
	}
}

// The reader reuses its input buffers immediately after publishing a result.
// The consumer runs concurrently and wraps the result ring repeatedly. Under
// -race this also checks that Drain does not read a slot after releasing it.
func TestConcurrentResultQueueOwnership(t *testing.T) {
	const count = 1024
	c, _, a := ownershipClient(t)
	c.seq = count
	wake := make(chan struct{}, 1)
	c.opt.Wake = func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stop); <-done })
	go func() {
		defer close(done)
		for i := uint32(1); i <= count; i++ {
			payload := []byte(strconv.Itoa(int(i)))
			id, flags := uint16(1), uint16(ipc.ResultHandled)
			if i%2 == 0 {
				id, flags = 3, flags|ipc.ResultASCII
			}
			r := ipc.Result{Ctx: 1, Seq: i, Policy: id, Flags: flags, Commit: payload}
			for !c.push(evResult, &r) {
				select {
				case <-stop:
					return
				default:
					runtime.Gosched()
				}
			}
			for j := range payload {
				payload[j] = '!'
			}
			c.arrived.Store(i)
		}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	finished := false
	for !finished || c.head.Load() != c.tail.Load() {
		c.Drain()
		if finished {
			continue
		}
		select {
		case <-done:
			finished = true
		case <-wake:
		case <-deadline.C:
			t.Fatal("result producer/consumer stalled")
		}
	}
	if len(a.log) != count {
		t.Fatalf("applied %d events, want %d", len(a.log), count)
	}
	for i, event := range a.log {
		if want := "commit:" + strconv.Itoa(i+1); event != want {
			t.Fatalf("event %d = %q, want %q", i, event, want)
		}
	}
	if c.cur.ID != 3 || !c.status.ASCII || c.pending() || c.stats.Repairs != 0 {
		t.Fatal("final result state was lost or a commit was repaired twice")
	}
}
