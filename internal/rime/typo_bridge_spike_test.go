//go:build shixin_typo_spike && (darwin || linux)

package rime

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"shixin/internal/dynlib"
	"shixin/spikes/typo-recovery/bridge"
	"slices"
	"strings"
	"testing"
	"time"
)

// Only opt-in scratch warm schemas, private user data, and no dictionary learning.
func TestTypoNativeBridgeSpike(t *testing.T) {
	root := os.Getenv("SHIXIN_TYPO_SPIKE_ROOT")
	if root == "" {
		t.Skip("set the prepared scratch runtime root")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	path := os.Getenv("SHIXIN_TYPO_SPIKE_LIB")
	if path == "" {
		path = filepath.Join(root, "native-graph/out/lib/librime.1.dylib")
	}
	lib, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	user := t.TempDir()
	if err := os.CopyFS(filepath.Join(user, "opencc"), os.DirFS(filepath.Join(root, "share/opencc"))); err != nil {
		t.Fatal(err)
	}
	lib.Setup(Traits{SharedDataDir: filepath.Join(root, "rime-data"), UserDataDir: user, PrebuiltDataDir: filepath.Join(root, "prebuilt"), StagingDir: typoSpikeStage(root), LogDir: user, MinLogLevel: 2, Modules: []string{"default", "shixin"}, AppName: "rime.shixin-spike-bridge"})
	lib.Initialize()
	defer lib.Finalize()
	native, err := bridge.Attach(func(name string) (uintptr, error) { return dynlib.Symbol(lib.handle, name) })
	if err != nil {
		t.Fatal(err)
	}
	_, err = bridge.Attach(func(name string) (uintptr, error) {
		if strings.HasSuffix(name, "_read") {
			return 0, errors.New("missing fixture symbol")
		}
		return dynlib.Symbol(lib.handle, name)
	})
	if err != bridge.ErrUnsupported {
		t.Fatalf("missing symbol fallback: %v", err)
	}
	session, err := lib.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()
	if !session.SelectSchema("rime_ice_warm") {
		t.Fatal("warm schema")
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	generation := binary.LittleEndian.Uint64(nonce[:])
	if generation == 0 {
		generation = 1
	}
	if !native.Configure(session.id, generation) {
		t.Fatal("configure")
	}
	for repeat := 0; repeat < 2; repeat++ {
		session.ClearComposition()
		for _, key := range "nihso" {
			session.ProcessKey(int(key), 0)
		}
	}
	var snapshot bridge.Snapshot
	if !native.Read(session.id, &snapshot) {
		t.Fatal("read")
	}
	var token bridge.Token
	found := false
	for i := 0; i < snapshot.Count(); i++ {
		record, ok := snapshot.Record(i)
		if !ok {
			t.Fatal("decode")
		}
		if string(record.Text) == "你好" {
			token = record.Token
			found = true
			if binary.LittleEndian.Uint64(token[:]) != generation {
				t.Fatal("generation changed")
			}
		}
	}
	if !found {
		t.Fatal("missing 你好")
	}
	failed := false
	allocations := testing.AllocsPerRun(1000, func() {
		if !native.Read(session.id, &snapshot) {
			failed = true
		}
		if _, ok := snapshot.Record(0); !ok {
			failed = true
		}
	})
	if failed || allocations != 0 {
		t.Fatalf("read allocations=%v failed=%v", allocations, failed)
	}
	var samples [1000]time.Duration
	for i := range samples {
		begin := time.Now()
		ok := native.Read(session.id, &snapshot)
		_, decoded := snapshot.Record(0)
		samples[i] = time.Since(begin)
		if !ok || !decoded {
			t.Fatal("timed read")
		}
	}
	slices.Sort(samples[:])
	t.Logf("warm native snapshot read/decode p50=%s p99=%s max=%s", samples[500], samples[990], samples[999])
	if !native.Select(session.id, &token) || native.Select(session.id, &token) {
		t.Fatal("single-use selection")
	}
	var commitBuffer [256]byte
	commit, ok := session.ReadCommit(commitBuffer[:0])
	if !ok || string(commit) != "你好" {
		t.Fatalf("commit %q ok=%v", commit, ok)
	}
	if !native.Read(session.id, &snapshot) || snapshot.Count() != 0 {
		t.Fatal("committed snapshot remains")
	}
	session.ClearComposition()
	for _, key := range "nihso" {
		session.ProcessKey(int(key), 0)
	}
	if !native.Read(session.id, &snapshot) {
		t.Fatal("second read")
	}
	record, ok := snapshot.Record(0)
	if !ok {
		t.Fatal("second frame empty")
	}
	token = record.Token
	if !native.Invalidate(session.id) || native.Select(session.id, &token) {
		t.Fatal("invalidation")
	}
	allocations = testing.AllocsPerRun(1000, func() {
		if native.Select(session.id, &token) {
			failed = true
		}
	})
	if failed || allocations != 0 {
		t.Fatalf("stale selection allocations=%v failed=%v", allocations, failed)
	}
	if native.Read(0, &snapshot) || snapshot != (bridge.Snapshot{}) {
		t.Fatal("invalid session not cleared")
	}
	t.Log("purego native snapshot read/decode and stale selection: zero Go allocations with reused buffers")
}

// The override selects only an explicitly prepared scratch generation.
func typoSpikeStage(root string) string {
	if generation := os.Getenv("SHIXIN_TYPO_SPIKE_GENERATION"); generation != "" {
		return filepath.Join(generation, "build")
	}
	return filepath.Join(root, "native-graph/separate-generation-v1/build")
}
