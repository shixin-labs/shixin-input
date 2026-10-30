package main

import (
	"runtime"
	"testing"

	"github.com/ebitengine/purego"

	"shixin/internal/ipc"
)

func TestModeIndicatorResults(t *testing.T) {
	old := modeBubble.state
	defer func() { modeBubble.state = old }()
	modeBubble.state = modeIndicator{client: 1}
	observeModeResult(&ipc.Result{}) // local Clear or disconnect
	if modeBubble.state.known {
		t.Fatal("local empty result invented Chinese mode")
	}
	observeModeResult(&ipc.Result{Ctx: 1, Flags: ipc.ResultASCII})
	if !modeBubble.state.pending || !modeBubble.state.ascii {
		t.Fatal("confirmed English result was not observed")
	}
	observeModeResult(&ipc.Result{Ctx: 1, Candidates: []ipc.Candidate{{Text: []byte("你")}}})
	if modeBubble.state.pending || !modeBubble.state.until.IsZero() || modeBubble.state.ascii {
		t.Fatal("candidates did not suppress the bubble/update its mode")
	}
}

func TestModeIndicatorDispatchDoesNotAllocate(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	old := modeBubble.args
	// No GUI or client access: only queue a preallocated no-op callback.
	callback := purego.NewCallback(func(uintptr) uintptr { return 0 })
	modeBubble.args = [3]uintptr{mainQueue, 0, callback}
	defer func() { modeBubble.args = old }()
	if n := testing.AllocsPerRun(100, dispatchModeBubble); n != 0 {
		t.Fatalf("queueing a notification allocates %v times", n)
	}
}
