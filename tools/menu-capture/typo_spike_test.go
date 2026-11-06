package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"shixin/internal/ipc"
)

func typoSpikeLibrary(base string) string {
	if lib := os.Getenv("SHIXIN_TYPO_SPIKE_LIB"); lib != "" {
		return lib
	}
	return filepath.Join(base, "native-graph/out/lib/librime.1.dylib")
}

func typoSpikePrebuilt(base string) string {
	if path := os.Getenv("SHIXIN_TYPO_SPIKE_PREBUILT"); path != "" {
		return path
	}
	return filepath.Join(base, "prebuilt")
}
func typoSpikeGeneration(base string) string {
	if path := os.Getenv("SHIXIN_TYPO_SPIKE_GENERATION"); path != "" {
		return path
	}
	return filepath.Join(base, "native-graph/generation")
}

// Opt-in test of the unshipped native spike. The worker owns a scratch user
// directory and accepts only warm schemas with learning disabled.
func TestTypoNativeSpikeEditing(t *testing.T) {
	// dyld removes its preload variable from the test process environment.
	// Restore the explicitly requested test runtime before starting the worker.
	if rt := os.Getenv("SHIXIN_DYLD_INSERT"); rt != "" {
		t.Setenv("DYLD_INSERT_LIBRARIES", rt)
	}
	base := os.Getenv("SHIXIN_TYPO_SPIKE_ROOT")
	if base == "" {
		t.Skip("set SHIXIN_TYPO_SPIKE_ROOT to the prepared scratch runtime")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	w, err := startWorker(ctx, options{engine: filepath.Join(base, "bin/shixin-engine"), generation: typoSpikeGeneration(base), data: filepath.Join(base, "rime-data"), prebuilt: typoSpikePrebuilt(base), lib: typoSpikeLibrary(base)})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	open := func(t *testing.T) *client {
		t.Helper()
		cl, e := connect(ctx, w.ready.Socket)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { cl.c.Close() })
		return cl
	}
	key := func(t *testing.T, cl *client, sym uint32) *ipc.Result {
		t.Helper()
		cl.seq++
		if e := cl.write((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: sym}).Append(nil)); e != nil {
			t.Fatal(e)
		}
		r, e := cl.result(cl.seq)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	selectText := func(t *testing.T, cl *client, r *ipc.Result, target string, requireCorrection bool) *ipc.Result {
		t.Helper()
		var seen []string
		for page := 0; page < 10; page++ {
			for i, c := range r.Candidates {
				seen = append(seen, string(c.Text)+":"+string(c.Comment))
				if string(c.Text) == target && (!requireCorrection || strings.HasPrefix(string(c.Comment), "纠错 → ")) {
					cl.seq++
					if e := cl.write((&ipc.Select{Ctx: 1, Seq: cl.seq, Index: uint32(i)}).Append(nil)); e != nil {
						t.Fatal(e)
					}
					out, e := cl.result(cl.seq)
					if e != nil {
						t.Fatal(e)
					}
					return out
				}
			}
			if r.Flags&ipc.ResultLastPage != 0 {
				break
			}
			var e error
			r, e = cl.page()
			if e != nil {
				t.Fatal(e)
			}
		}
		t.Fatalf("corrected %q unavailable: %v", target, seen)
		return nil
	}
	for _, tc := range []struct{ raw, target string }{{"nihso", "你好"}, {"nihoa", "你好"}, {"njh", "你好"}, {"xgr", "中国人"}, {"zhr", "中国人"}, {"changshicuowj", "尝试错误"}} {
		t.Run("full_"+tc.raw, func(t *testing.T) {
			cl := open(t)
			r, e := cl.typeCode(tc.raw)
			if e != nil {
				t.Fatal(e)
			}
			r = selectText(t, cl, r, tc.target, tc.raw != "nihoa")
			if string(r.Commit) != tc.target || r.Flags&ipc.ResultComposing != 0 {
				t.Fatalf("commit=%q preedit=%q flags=%x", r.Commit, r.Preedit, r.Flags)
			}
		})
	}
	t.Run("native_symbols", func(t *testing.T) {
		cl := open(t)
		r, e := cl.typeCode("vlz")
		if e != nil {
			t.Fatal(e)
		}
		if len(r.Candidates) != 2 || string(r.Candidates[0].Text) != "ʫ" || string(r.Candidates[1].Text) != "ɮ" {
			t.Fatalf("native symbol menu changed: %v", r.Candidates)
		}
		r = key(t, cl, ' ')
		if string(r.Commit) != "ʫ" || r.Flags&ipc.ResultComposing != 0 {
			t.Fatalf("native symbol commit=%q flags=%x", r.Commit, r.Flags)
		}
	})
	t.Run("backspace", func(t *testing.T) {
		cl := open(t)
		if _, e := cl.typeCode("nihso"); e != nil {
			t.Fatal(e)
		}
		r := key(t, cl, 0xff08)
		got := strings.ReplaceAll(string(r.Preedit), " ", "")
		got = strings.ReplaceAll(got, "'", "")
		if got != "nihs" {
			t.Fatalf("backspace rewrote raw input: %q", r.Preedit)
		}
	})
	t.Run("literal_return", func(t *testing.T) {
		cl := open(t)
		if _, e := cl.typeCode("nihso"); e != nil {
			t.Fatal(e)
		}
		r := key(t, cl, 0xff0d)
		if string(r.Commit) != "nihso" || r.Flags&ipc.ResultComposing != 0 {
			t.Fatalf("literal commit=%q flags=%x", r.Commit, r.Flags)
		}
	})
	t.Run("partial", func(t *testing.T) {
		cl := open(t)
		r, e := cl.typeCode("nihsozhongguo")
		if e != nil {
			t.Fatal(e)
		}
		r = selectText(t, cl, r, "你好", true)
		letters := ""
		for _, c := range string(r.Preedit) {
			if c >= 'a' && c <= 'z' {
				letters += string(c)
			}
		}
		if letters != "zhongguo" {
			t.Fatalf("partial selection lost/duplicated raw bytes: commit=%q preedit=%q", r.Commit, r.Preedit)
		}
		t.Logf("partial commit=%q preedit=%q", r.Commit, r.Preedit)
		r = key(t, cl, ' ')
		if string(r.Commit) != "你好中国" || r.Flags&ipc.ResultComposing != 0 {
			t.Fatalf("partial completion commit=%q preedit=%q flags=%x", r.Commit, r.Preedit, r.Flags)
		}
	})
}

// Measures the engine's own histogram, not client round-trip timings. Run the
// ordinary and protected variants separately, with no concurrent capture jobs.
func TestTypoNativeSpikeLatency(t *testing.T) {
	base := os.Getenv("SHIXIN_TYPO_SPIKE_ROOT")
	if base == "" {
		t.Skip("native spike runtime not requested")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	variant := os.Getenv("SHIXIN_TYPO_SPIKE_VARIANT")
	if variant == "" {
		variant = "protected"
	}
	generation := typoSpikeGeneration(base)
	if variant == "ordinary" {
		generation = filepath.Join(base, "generation")
	} else if variant != "protected" {
		t.Fatal("variant must be ordinary or protected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	w, err := startWorker(ctx, options{engine: filepath.Join(base, "bin/shixin-engine"), generation: generation, data: filepath.Join(base, "rime-data"), prebuilt: typoSpikePrebuilt(base), lib: typoSpikeLibrary(base)})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	cl, err := connect(ctx, w.ready.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.c.Close()
	stats := func(reset bool) ipc.Stats {
		t.Helper()
		if e := cl.write((&ipc.StatsRequest{Reset: reset}).Append(nil)); e != nil {
			t.Fatal(e)
		}
		for {
			typ, p, e := cl.r.Next()
			if e != nil {
				t.Fatal(e)
			}
			if typ == ipc.TypePolicy {
				continue
			}
			var s ipc.Stats
			if typ != ipc.TypeStats {
				t.Fatalf("unexpected frame %d", typ)
			}
			if e = s.Decode(p); e != nil {
				t.Fatal(e)
			}
			return s
		}
	}
	codes := []string{"nihao", "nihso", "njh", "xgr", "zhr", "changshicuowj", "qingyuan", "sahuanr", "nihsozhongguo", "nhiao", "niaho"}
	for repeat := 0; repeat < 21; repeat++ {
		if repeat == 1 {
			stats(true)
		}
		for _, code := range codes {
			if _, e := cl.typeCode(code); e != nil {
				t.Fatal(e)
			}
			// Exercise lazy translation work on paging, then edit raw input.
			for _, sym := range []uint32{0xff56, 0xff56, 0xff08} {
				cl.seq++
				if e := cl.write((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: sym}).Append(nil)); e != nil {
					t.Fatal(e)
				}
				if _, e := cl.result(cl.seq); e != nil {
					t.Fatal(e)
				}
			}
			cl.seq++
			if e := cl.write((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: 0xff1b}).Append(nil)); e != nil {
				t.Fatal(e)
			}
			r, e := cl.result(cl.seq)
			if e != nil {
				t.Fatal(e)
			}
			if r.Flags&ipc.ResultComposing != 0 {
				t.Fatal("Escape left a composition")
			}
		}
	}
	s := stats(false)
	t.Logf("variant=%s engine_key_time=%s p99.9=%s", variant, s.KeyTime.String(), s.KeyTime.Quantile(.999))
	if s.KeyTime.Quantile(.5) > 300*time.Microsecond || s.KeyTime.Quantile(.99) > 2*time.Millisecond || s.KeyTime.Quantile(.999) > 8*time.Millisecond {
		t.Error("native spike fails the engine key latency budget")
	}
}

// Sample the engine process, including native allocations and mapped pages.
// This is not a per-key allocation count or a transient peak measurement.
func TestTypoNativeSpikeResidentMemory(t *testing.T) {
	base := os.Getenv("SHIXIN_TYPO_SPIKE_ROOT")
	if base == "" || runtime.GOOS != "darwin" {
		t.Skip("opt-in macOS scratch RSS diagnostic")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	variant := os.Getenv("SHIXIN_TYPO_SPIKE_VARIANT")
	generation := typoSpikeGeneration(base)
	if variant == "ordinary" {
		generation = filepath.Join(base, "generation")
	} else if variant != "protected" {
		t.Fatal("set ordinary or protected variant")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	w, err := startWorker(ctx, options{engine: filepath.Join(base, "bin/shixin-engine"), generation: generation, data: filepath.Join(base, "rime-data"), prebuilt: typoSpikePrebuilt(base), lib: typoSpikeLibrary(base)})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	cl, err := connect(ctx, w.ready.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.c.Close()
	sample := func() uint64 {
		out, e := exec.CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(w.cmd.Process.Pid)).Output()
		if e != nil {
			t.Fatal(e)
		}
		n, e := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		if e != nil {
			t.Fatal(e)
		}
		return n * 1024
	}
	values := []uint64{sample()}
	// Optional process-level attribution, kept outside the measured key replay.
	// vmmap may suspend the scratch worker; never enable it in latency tests.
	profile := func(phase string) {
		t.Helper()
		dir := os.Getenv("SHIXIN_TYPO_SPIKE_VMMAP_DIR")
		if dir == "" {
			return
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		out, err := exec.CommandContext(ctx, "vmmap", "-wide", "-noCoalesce", strconv.Itoa(w.cmd.Process.Pid)).CombinedOutput()
		if err != nil {
			t.Fatalf("vmmap: %v\n%s", err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, variant+"-"+phase+".txt"), out, 0600); err != nil {
			t.Fatal(err)
		}
	}
	profile("before")
	codes := []string{"nihao", "nihso", "njh", "changshicuowj", "qingyuan", "sahuanr", "nihsozhongguo", "nhiao", "xgr", "niaho"}
	compositions := 0
	for batch := 0; batch < 10; batch++ {
		for repeat := 0; repeat < 15; repeat++ {
			for _, code := range codes {
				cl.seq++
				if e := cl.write((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: 0xff1b}).Append(nil)); e != nil {
					t.Fatal(e)
				}
				if _, e := cl.result(cl.seq); e != nil {
					t.Fatal(e)
				}
				if _, e := cl.typeCode(code); e != nil {
					t.Fatal(e)
				}
				compositions++
			}
		}
		values = append(values, sample())
	}
	t.Logf("variant=%s compositions=%d sampled_rss_bytes=%v", variant, compositions, values)
	profile("after")
	if os.Getenv("SHIXIN_TYPO_SPIKE_VMMAP_DIR") != "" {
		cl.seq++
		if err := cl.write((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: 0xff1b}).Append(nil)); err != nil {
			t.Fatal(err)
		}
		result, err := cl.result(cl.seq)
		if err != nil || result.Flags&ipc.ResultComposing != 0 {
			t.Fatalf("clear before profile: composing=%v err=%v", result.Flags&ipc.ResultComposing != 0, err)
		}
		profile("cleared")
	}
}
