//go:build shixin_typo_spike && (darwin || linux)

package rime

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"shixin/internal/dynlib"
	"shixin/spikes/typo-recovery/bridge"
)

// Offline public-code capture only. No selections, AI or learning.
func TestTypoCorrectionCorpusSpike(t *testing.T) {
	root, output := os.Getenv("SHIXIN_TYPO_SPIKE_ROOT"), os.Getenv("SHIXIN_TYPO_CORPUS_OUTPUT")
	if root == "" || output == "" {
		t.Skip("set scratch root and new corpus output")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(filepath.Join(root, "native-graph/staged-evaluation/codes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lib, err := Load(filepath.Join(root, "native-graph/out/lib/librime.1.dylib"))
	if err != nil {
		t.Fatal(err)
	}
	user := t.TempDir()
	if err := os.CopyFS(filepath.Join(user, "opencc"), os.DirFS(filepath.Join(root, "share/opencc"))); err != nil {
		t.Fatal(err)
	}
	lib.Setup(Traits{SharedDataDir: filepath.Join(root, "rime-data"), UserDataDir: user, PrebuiltDataDir: filepath.Join(root, "prebuilt"), StagingDir: typoSpikeStage(root), LogDir: user, MinLogLevel: 2, Modules: []string{"default", "shixin"}, AppName: "rime.shixin-spike-corpus"})
	lib.Initialize()
	defer lib.Finalize()
	session, err := lib.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()
	if !session.SelectSchema("rime_ice_warm") {
		t.Fatal("select warm schema")
	}
	native, err := bridge.Attach(func(name string) (uintptr, error) { return dynlib.Symbol(lib.handle, name) })
	if err != nil || !native.Configure(session.id, 1) {
		t.Fatalf("configure bridge: %v", err)
	}
	scanner := bufio.NewScanner(input)
	count := 0
	for scanner.Scan() {
		var row struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if len(row.Code) == 0 || len(row.Code) > 32 {
			t.Fatal("invalid corpus code")
		}
		for _, b := range []byte(row.Code) {
			if b < 'a' || b > 'z' {
				t.Fatal("non-lowercase public code")
			}
		}
		for repeat := 0; repeat < 2; repeat++ {
			session.ClearComposition()
			for _, key := range row.Code {
				session.ProcessKey(int(key), 0)
			}
			result := struct {
				Code        string   `json:"code"`
				Repeat      int      `json:"repeat"`
				Ordinary    []string `json:"ordinary"`
				Corrections []string `json:"corrections"`
			}{Code: row.Code, Repeat: repeat}
			var snapshot bridge.Snapshot
			if !native.Read(session.id, &snapshot) {
				t.Fatal("read snapshot")
			}
			for i := 0; i < snapshot.Count(); i++ {
				record, ok := snapshot.Record(i)
				if !ok {
					t.Fatal("decode snapshot")
				}
				if record.Begin == 0 && record.End == uint32(len(row.Code)) {
					result.Corrections = append(result.Corrections, string(record.Text))
				}
			}
			var context Context
			if session.ReadContext(&context) {
				for i, candidate := range context.Candidates {
					if i == 5 {
						break
					}
					result.Ordinary = append(result.Ordinary, string(candidate.Text))
				}
			}
			if err := encoder.Encode(result); err != nil {
				t.Fatal(err)
			}
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("empty corpus")
	}
	t.Logf("captured %d public codes, two traces each; ordinary first five plus full-span separate corrections", count)
}
