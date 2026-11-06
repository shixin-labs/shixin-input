package rime

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// Uses an independently built warm-only generation and a fresh private user dir.
// Run alone: librime owns process-global initialization.
func TestTypoProductMenu(t *testing.T) {
	gen := os.Getenv("SHIXIN_TYPO_GENERATION")
	if gen == "" {
		t.Skip("needs prepared typo generation")
	}
	root := os.Getenv("SHIXIN_TYPO_DATA_ROOT")
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lib, err := Load(os.Getenv("SHIXIN_LIBRIME"))
	if err != nil {
		t.Fatal(err)
	}
	user := t.TempDir()
	if err := os.CopyFS(filepath.Join(user, "opencc"), os.DirFS(filepath.Join(root, "share/opencc"))); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(gen, "build")
	if baseline := os.Getenv("SHIXIN_TYPO_BASELINE"); baseline != "" {
		privateStage := filepath.Join(user, "build")
		if err := os.Mkdir(privateStage, 0700); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(stage)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if err := os.Symlink(filepath.Join(stage, entry.Name()), filepath.Join(privateStage, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
		data, err := os.ReadFile(filepath.Join(baseline, "build/rime_ice_warm.schema.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.ReplaceAll(data, []byte("schema_id: rime_ice_warm"), []byte("schema_id: typo_baseline"))
		if err := os.WriteFile(filepath.Join(privateStage, "typo_baseline.schema.yaml"), data, 0600); err != nil {
			t.Fatal(err)
		}
		stage = privateStage
	}
	lib.Setup(Traits{SharedDataDir: filepath.Join(root, "rime-data"), UserDataDir: user, PrebuiltDataDir: filepath.Join(root, "prebuilt"), StagingDir: stage, LogDir: user, MinLogLevel: 2, Modules: []string{"default", "shixin"}, AppName: "rime.shixin-typo-test"})
	lib.Initialize()
	defer lib.Finalize()
	session, err := lib.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()
	if !session.SelectSchema("rime_ice_warm") {
		t.Fatal("schema")
	}
	if os.Getenv("SHIXIN_TYPO_BASELINE") != "" {
		baseline, err := lib.CreateSession()
		if err != nil {
			t.Fatal(err)
		}
		defer baseline.Destroy()
		if !baseline.SelectSchema("typo_baseline") {
			t.Fatal("baseline schema")
		}
		var a, b Context
		for _, code := range []string{"nihao", "nihoa", "nihso", "niaho", "zhongguo", "zg", "sj", "ts", "beijing", "bj", "zhongg", "renmin", "rmb", "xian", "xi'an", "hello", "date", "123", "v", "shixin"} {
			session.ClearComposition()
			baseline.ClearComposition()
			for _, key := range code {
				session.ProcessKey(int(key), 0)
				baseline.ProcessKey(int(key), 0)
				session.ReadContext(&a)
				baseline.ReadContext(&b)
				texts := func(c *Context) string {
					var rows []string
					for _, v := range c.Candidates {
						rows = append(rows, string(v.Text)+"|"+string(v.Comment))
					}
					return strings.Join(rows, "\n")
				}
				if texts(&a) != texts(&b) {
					t.Fatalf("%s prefix %q: first page changed\non: %s\noff: %s", code, a.Preedit, texts(&a), texts(&b))
				}
			}
		}
	}
	var context Context
	typeCode := func(code string) {
		session.ClearComposition()
		for _, key := range code {
			session.ProcessKey(int(key), 0)
		}
	}
	for _, code := range []string{"nihso", "niaho"} {
		for _, click := range []bool{true, false} {
			typeCode(code)
			if !session.ReadContext(&context) {
				t.Fatal("context")
			}
			for _, c := range context.Candidates {
				t.Logf("%s page 1: %s %s", code, c.Text, c.Comment)
				if bytes.Contains(c.Comment, []byte("纠错")) {
					t.Fatal("correction on first page")
				}
			}
			session.ProcessKey(0xff56, 0) // PageDown
			if !session.ReadContext(&context) || context.PageNo != 1 {
				t.Fatalf("%s: missing page two", code)
			}
			pick := -1
			for i, c := range context.Candidates {
				t.Logf("%s page 2: %s %s", code, c.Text, c.Comment)
				if string(c.Text) == "你好" && bytes.Contains(c.Comment, []byte("纠错")) {
					pick = i
				}
			}
			if pick < 0 {
				t.Fatalf("%s: missing labelled 你好", code)
			}
			if click {
				if !session.SelectCandidateOnCurrentPage(pick) {
					t.Fatal("click")
				}
			} else {
				session.ProcessKey(int('1')+pick, 0)
			}
			var buf [256]byte
			commit, ok := session.ReadCommit(buf[:0])
			if !ok || string(commit) != "你好" {
				t.Fatalf("commit %q, %v", commit, ok)
			}
		}
	}
	// Warm native caches and the Go snapshot buffers before the allocation gate.
	for i := 0; i < 3; i++ {
		typeCode("nihso")
		session.ReadContext(&context)
	}
	alloc := testing.AllocsPerRun(100, func() { typeCode("nihso"); session.ReadContext(&context) })
	if alloc != 0 {
		t.Fatalf("Go allocations per trace: %v", alloc)
	}
	var samples []time.Duration
	for i := 0; i < 100; i++ {
		for _, code := range []string{"nihao", "nihso", "nihoa", "sj", "zg", "zhongguo"} {
			session.ClearComposition()
			for _, key := range code {
				start := time.Now()
				session.ProcessKey(int(key), 0)
				session.ReadContext(&context)
				samples = append(samples, time.Since(start))
			}
		}
	}
	slices.Sort(samples)
	p50, p99, p999 := samples[len(samples)/2], samples[len(samples)*99/100], samples[len(samples)*999/1000]
	t.Logf("%d keys: p50=%s p99=%s p99.9=%s max=%s; warm Go allocations=0", len(samples), p50, p99, p999, samples[len(samples)-1])
}
