package client

import (
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

func TestEnglishSwitchStartsNewSpacingBoundary(t *testing.T) {
	for _, kind := range []string{"passed", "commit-code", "repair"} {
		t.Run(kind, func(t *testing.T) {
			a := &secureRecorder{}
			c := New(Options{}, a)
			defer c.timer.Stop()
			c.applyResult(&ipc.Result{Flags: ipc.ResultAutoSpace, Commit: []byte("中文")})
			a.log = nil
			a.reads = 0
			r := ipc.Result{Flags: ipc.ResultAutoSpace | ipc.ResultASCII}
			switch kind {
			case "commit-code":
				r.Commit = []byte("hello")
			case "repair":
				r.Seq = 1
				c.keys[1] = sentKey{seq: 1, keysym: 'h', eaten: true}
			}
			c.applyResult(&r)
			if kind == "passed" && c.pass('h', 0) {
				t.Fatal("first English key gained a space")
			}
			want := map[string]string{"passed": "", "commit-code": "hello", "repair": "h"}[kind]
			if a.commits() != want || a.reads != 0 {
				t.Fatalf("commits %q, reads %d", a.commits(), a.reads)
			}
			// An ordinary ASCII result must not reset history again. Returning
			// to Chinese preserves normal spacing from the newly typed English.
			c.applyResult(&ipc.Result{Flags: ipc.ResultAutoSpace | ipc.ResultASCII})
			c.sp.reader = nil
			c.applyResult(&ipc.Result{Flags: ipc.ResultAutoSpace, Commit: []byte("你好")})
			if a.commits() != want+" 你好" {
				t.Fatalf("normal spacing lost: %q", a.commits())
			}
			// UI status has not been published between these replies. A second
			// switch still starts a new boundary.
			c.applyResult(&ipc.Result{Flags: ipc.ResultAutoSpace | ipc.ResultASCII, Commit: []byte("again")})
			if a.commits() != want+" 你好again" {
				t.Fatalf("second switch: %q", a.commits())
			}
		})
	}
}

func BenchmarkSpacingModeSwitch(b *testing.B) {
	c := New(Options{}, nopApplier{})
	defer c.timer.Stop()
	zh := ipc.Result{Flags: ipc.ResultAutoSpace, Commit: []byte("中文")}
	en := ipc.Result{Flags: ipc.ResultAutoSpace | ipc.ResultASCII, Commit: []byte("hello")}
	b.ReportAllocs()
	for b.Loop() {
		c.applyResult(&zh)
		c.applyResult(&en)
	}
}

func TestSpacingModeSwitchDoesNotAllocate(t *testing.T) {
	c := New(Options{}, nopApplier{})
	defer c.timer.Stop()
	zh := ipc.Result{Flags: ipc.ResultAutoSpace, Commit: []byte("中文")}
	en := ipc.Result{Flags: ipc.ResultAutoSpace | ipc.ResultASCII, Commit: []byte("hello")}
	if n := testing.AllocsPerRun(1000, func() { c.applyResult(&zh); c.applyResult(&en) }); n != 0 {
		t.Fatalf("%v allocations", n)
	}
}

func TestShiftSwitchSpacingWithEngine(t *testing.T) {
	for _, composing := range []bool{false, true} {
		h := newHarness(t, realEngine(t), nil)
		h.typ("nihao ")
		h.settle()
		if composing {
			h.typ("hello")
			h.settle()
		}
		h.c.HandleKey(policy.KeyShiftL, 0)
		h.c.HandleKey(policy.KeyShiftL, policy.ReleaseMask)
		h.until(func() bool { return h.rec.status.ASCII })
		want := "你好"
		if composing {
			want += "hello"
		}
		if got := h.rec.commits(); got != want {
			t.Fatalf("composing %v: commits %q, want %q", composing, got, want)
		}
		if h.c.HandleKey('a', 0) {
			t.Fatal("English key gained automatic space")
		}
	}
}
