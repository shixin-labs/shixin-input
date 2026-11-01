package rerank

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"shixin/internal/ipc"
)

// sink is a shixin-ai that says it is ready and discards everything.
func sink(t *testing.T) string {
	dir, err := ipc.TempDir("sxr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "ai.sock")
	l, err := ipc.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := ipc.Accept(l)
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				if _, _, err := ipc.NewReader(c, 64).Next(); err != nil {
					return
				}
				c.Write((&Status{Version: WireVersion, Ready: true}).Append(nil))
				io.Copy(io.Discard, c)
			}()
		}
	}()
	return path
}

// TestRemoteDoesNotAllocate: Context and Score run on the engine thread.
func TestRemoteDoesNotAllocate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("allocates over the Windows pipe transport (KNOWN-ISSUES, Windows)")
	}
	r := NewRemote(sink(t), StrengthStandard, t.Logf)
	stop := make(chan struct{})
	defer close(stop)
	go r.Run(stop, func(int, uint32, []uint8) {})
	for deadline := time.Now().Add(5 * time.Second); !r.Ready(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("not ready")
		}
	}
	var sl Slot
	copy(sl.Arena[:], "台湾太晚")
	sl.Entries[0] = Entry{Offset: 0, Length: 6}
	sl.Entries[1] = Entry{Offset: 6, Length: 6}
	sl.Count, sl.ExportSeq = 2, 1
	req := Request{Session: 1, ExportSeq: 1, App: 2, AppRev: 3, Export: &sl}
	text := []byte("我们明天一起去")
	r.Context(2, 3, text) // grow the cached frame
	if a := testing.AllocsPerRun(200, func() {
		r.Context(2, 3, text)
		r.Score(&req)
		time.Sleep(50 * time.Microsecond) // let the writer recycle buffers
	}); a != 0 {
		t.Errorf("%v allocations per context and score", a)
	}
}
