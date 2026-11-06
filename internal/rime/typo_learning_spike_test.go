//go:build shixin_typo_spike && (darwin || linux)

package rime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"shixin/internal/dynlib"
	"shixin/spikes/typo-recovery/bridge"
)

// Writer and exporter never overlap and use only a new private database.
func TestTypoLearningPersistenceSpike(t *testing.T) {
	root := os.Getenv("SHIXIN_TYPO_SPIKE_ROOT")
	if root == "" {
		t.Skip("set the prepared scratch runtime root")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if role := os.Getenv("SHIXIN_TYPO_LEARNING_ROLE"); role != "" {
		typoLearningChild(t, root, os.Getenv("SHIXIN_TYPO_LEARNING_DIR"), role)
		return
	}
	for _, mode := range []string{"neighbor", "native_alias", "swap", "emoji", "unselected"} {
		t.Run(mode, func(t *testing.T) {
			private := t.TempDir()
			stage := filepath.Join(private, "build")
			if err := os.MkdirAll(stage, 0700); err != nil {
				t.Fatal(err)
			}
			source := typoSpikeStage(root)
			entries, err := os.ReadDir(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if err := os.Symlink(filepath.Join(source, entry.Name()), filepath.Join(stage, entry.Name())); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(filepath.Join(source, "rime_ice_warm.schema.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			var schema map[string]any
			if err := yaml.Unmarshal(data, &schema); err != nil {
				t.Fatal(err)
			}
			schema["schema"].(map[string]any)["schema_id"] = "typo_learning"
			translator := schema["translator"].(map[string]any)
			translator["enable_user_dict"] = true
			translator["user_dict"] = "typo_learning"
			data, err = yaml.Marshal(schema)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stage, "typo_learning.schema.yaml"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.CopyFS(filepath.Join(private, "user/opencc"), os.DirFS(filepath.Join(root, "share/opencc"))); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			for _, role := range []string{"write", "export"} {
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTypoLearningPersistenceSpike$", "-test.v")
				cmd.Env = append(os.Environ(), "SHIXIN_TYPO_LEARNING_ROLE="+role, "SHIXIN_TYPO_LEARNING_DIR="+private, "SHIXIN_TYPO_LEARNING_MODE="+mode)
				if rt := os.Getenv("SHIXIN_DYLD_INSERT"); rt != "" {
					cmd.Env = append(cmd.Env, "DYLD_INSERT_LIBRARIES="+rt)
				}
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s: %v\n%s", role, err, out)
				}
				t.Logf("%s: %s", role, out)
			}
			data, err = os.ReadFile(filepath.Join(private, "export.txt"))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "#") || line == "" {
					continue
				}
				fields := strings.Split(line, "\t")
				if len(fields) < 3 {
					t.Fatalf("malformed export: %q", line)
				}
				if fields[0] == "你好" && strings.TrimSpace(fields[1]) == "ni hao" {
					found = true
					if fields[2] != "8" {
						t.Fatalf("commit count %q, want 8", fields[2])
					}
					t.Logf("persisted canonical entry: %s", line)
				}
				if strings.Contains(line, "nihso") || strings.Contains(line, "nihoa") || strings.Contains(line, "niaho") || strings.Contains(line, "👋") {
					t.Fatalf("learned raw typo or display conversion: %s", line)
				}
			}
			if mode == "unselected" {
				if found {
					t.Fatal("displaying corrections caused learning")
				}
			} else if !found {
				t.Fatalf("missing persisted canonical entry:\n%s", data)
			}
		})
	}
}

func typoLearningChild(t *testing.T, root, private, role string) {
	t.Helper()
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
	lib.Setup(Traits{SharedDataDir: filepath.Join(root, "rime-data"), UserDataDir: filepath.Join(private, "user"), PrebuiltDataDir: filepath.Join(root, "prebuilt"), StagingDir: filepath.Join(private, "build"), LogDir: private, MinLogLevel: 2, Modules: []string{"default", "shixin"}, AppName: "rime.shixin-spike-learning"})
	if role == "export" {
		lib.InitializeDeployer()
		defer lib.Finalize()
		n, err := lib.ExportUserDictionary("typo_learning", filepath.Join(private, "export.txt"))
		wantEntries := 1
		if os.Getenv("SHIXIN_TYPO_LEARNING_MODE") == "unselected" {
			wantEntries = 0
		}
		if err != nil || n != wantEntries {
			t.Fatalf("export entries=%d error=%v", n, err)
		}
		return
	}
	lib.Initialize()
	defer lib.Finalize()
	session, err := lib.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()
	if !session.SelectSchema("typo_learning") {
		t.Fatal("select private learning schema")
	}
	native, err := bridge.Attach(func(name string) (uintptr, error) { return dynlib.Symbol(lib.handle, name) })
	if err != nil {
		t.Fatal(err)
	}
	if !native.Configure(session.id, 1) {
		t.Fatal("configure")
	}
	code, want := "nihso", "你好"
	switch os.Getenv("SHIXIN_TYPO_LEARNING_MODE") {
	case "swap":
		code = "niaho"
	case "native_alias":
		code = "nihoa"
	case "emoji":
		session.SetOption("emoji", true)
		want = "👋"
	}
	for repeat := 0; repeat < 8; repeat++ {
		if os.Getenv("SHIXIN_TYPO_LEARNING_MODE") == "native_alias" {
			session.ClearComposition()
			for _, key := range code {
				session.ProcessKey(int(key), 0)
			}
			var context Context
			if !session.ReadContext(&context) {
				t.Fatal("read alias menu")
			}
			pick := -1
			for i, candidate := range context.Candidates {
				if string(candidate.Text) == want {
					pick = i
					break
				}
			}
			if pick < 0 || !session.SelectCandidateOnCurrentPage(pick) {
				t.Fatal("native spelling alias unavailable")
			}
			commit, ok := session.ReadCommit(nil)
			if !ok || string(commit) != want {
				t.Fatalf("alias commit %q", commit)
			}
			continue
		}
		var token bridge.Token
		found := false
		// A cold bounded search may safely return no corrections. Retry the
		// trace before selection, never committing an ordinary substitute.
		for attempt := 0; attempt < 3 && !found; attempt++ {
			session.ClearComposition()
			for _, key := range code {
				session.ProcessKey(int(key), 0)
			}
			var snapshot bridge.Snapshot
			if !native.Read(session.id, &snapshot) {
				t.Fatal("read correction snapshot")
			}
			for i := 0; i < snapshot.Count(); i++ {
				record, ok := snapshot.Record(i)

				if ok && string(record.Text) == want {
					token = record.Token
					found = true
					break
				}
			}
		}
		if !found {
			t.Fatalf("correction unavailable at %d", repeat)
		}
		if os.Getenv("SHIXIN_TYPO_LEARNING_MODE") == "unselected" {
			session.ClearComposition()
			continue
		}
		if !native.Select(session.id, &token) {
			t.Fatalf("correction unavailable/selection failed at %d", repeat)
		}
		commit, ok := session.ReadCommit(nil)
		if !ok || string(commit) != want {
			t.Fatalf("commit %q want %q", commit, want)
		}
	}
	// End the final learning transaction through the normal unhandled-key path.
	session.ProcessKey(0xff09, 0) // Tab, no composition
}
