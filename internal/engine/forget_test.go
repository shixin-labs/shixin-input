package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"shixin/internal/ipc"
	"shixin/internal/policy"
	"shixin/internal/settings"
)

// Each stage owns its scratch dictionary in a separate process. This proves
// that forgetting reaches disk, rather than just changing a session's menu.
func TestForgetCandidatePersistence(t *testing.T) {
	if stage := os.Getenv("SHIXIN_FORGET_STAGE"); stage != "" {
		forgetProcess(t, os.Getenv("SHIXIN_FORGET_ROOT"), os.Getenv("SHIXIN_FORGET_KEY"), stage)
		return
	}
	librimePath(t)
	for _, key := range []string{"Control+Shift+BackSpace", "Control+Delete", ""} {
		t.Run(key, func(t *testing.T) {
			root := t.TempDir()
			for _, stage := range []string{"learn", "forget", "verify"} {
				cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestForgetCandidatePersistence$", "-test.v")
				cmd.Env = append(os.Environ(), "SHIXIN_FORGET_STAGE="+stage, "SHIXIN_FORGET_ROOT="+root, "SHIXIN_FORGET_KEY="+key)
				if v := os.Getenv("SHIXIN_DYLD_INSERT"); v != "" {
					cmd.Env = append(cmd.Env, "DYLD_INSERT_LIBRARIES="+v)
				}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s: %v\n%s", stage, err, out)
				} else if stage == "forget" && key != "" {
					t.Logf("%s:\n%s", stage, out)
				}
			}
		})
	}
}

func forgetProcess(t *testing.T, root, key, stage string) {
	data, _ := filepath.Abs("testdata/rime")
	user := filepath.Join(root, "user")
	release, err := Lock(user)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	s := settings.Default()
	s.Hotkeys["forget"] = key
	files, err := settings.Compile(s)
	if err != nil {
		t.Fatal(err)
	}
	var patch struct{ Patch map[string]any }
	if err := yaml.Unmarshal(files["rime_ice.custom.yaml"], &patch); err != nil {
		t.Fatal(err)
	}
	b, err := yaml.Marshal(map[string]any{"patch": map[string]any{"editor/bindings": patch.Patch["editor/bindings"]}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "test.custom.yaml"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := Start(Options{LibPath: librimePath(t), SharedDataDir: data, UserDataDir: user, LogDir: root,
		MinLogLevel: 2, Version: "forget-test", Deploy: stage == "learn", Warmup: []string{"nihao"}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	cl := dial(t, e)
	cl.setASCII(false)
	if stage == "learn" {
		if firstText(cl.typ("shiyan")) != "实验" {
			t.Fatal("unexpected static ranking")
		}
		cl.key(policy.KeyEscape, 0)
		for range 8 {
			highlightWord(t, cl, "shiyan", "试验")
			if got := string(cl.key(' ', 0).Commit); got != "试验" {
				t.Fatalf("learning commit %q", got)
			}
		}
		if firstText(cl.typ("shiyan")) != "试验" {
			t.Fatal("word was not learned")
		}
		cl.key(policy.KeyEscape, 0)
		// Build a word absent from the static dictionary by choosing its
		// first syllable, then confirming the remainder.
		for range 8 {
			highlightWord(t, cl, "nihao", "泥")
			cl.key(' ', 0)
			if got := string(cl.key(' ', 0).Commit); got != "泥好" {
				t.Fatalf("compound learning commit %q", got)
			}
		}
		if !hasCandidate(cl.typ("nihao"), "泥好") {
			t.Fatal("compound word was not learned")
		}
		cl.key(policy.KeyEscape, 0)
		return
	}
	want := "试验"
	if stage == "verify" && key != "" {
		want = "实验"
	}
	if got := firstText(cl.typ("shiyan")); got != want {
		t.Fatalf("%s: first %q, want %q", stage, got, want)
	}
	cl.key(policy.KeyEscape, 0)
	if stage == "verify" {
		if got := hasCandidate(cl.typ("nihao"), "泥好"); got != (key == "") {
			t.Fatalf("persisted compound presence %v, shortcut %q", got, key)
		}
		cl.key(policy.KeyEscape, 0)
		return
	}

	// No shortcut may consume idle/English keys or change a dictionary.
	for _, ascii := range []bool{false, true} {
		cl.setASCII(ascii)
		for _, repr := range []string{"Control+Shift+BackSpace", "Control+Delete"} {
			k, m, _ := parseKey(repr)
			if cl.policy().Lookup(k, m).Disposition() != policy.Pass {
				t.Fatalf("idle %s is not PASS", repr)
			}
			if r := cl.key(k, m); r.Flags&ipc.ResultHandled != 0 || len(r.Commit) != 0 {
				t.Fatalf("idle %s consumed", repr)
			}
		}
	}
	cl.setASCII(false)
	if key == "" {
		for _, repr := range []string{"Control+Shift+BackSpace", "Control+Delete"} {
			highlightWord(t, cl, "shiyan", "试验")
			k, m, _ := parseKey(repr)
			cl.key(k, m)
			cl.key(policy.KeyEscape, 0)
			if firstText(cl.typ("shiyan")) != "试验" {
				t.Fatalf("Off still forgets via %s", repr)
			}
			cl.key(policy.KeyEscape, 0)
		}
		return
	}
	k, m, _ := parseKey(key)
	// Forgetting a non-first highlight must leave the learned first word.
	highlightWord(t, cl, "shiyan", "实验")
	if r := cl.key(k, m); len(r.Commit) != 0 || r.Flags&ipc.ResultHandled == 0 {
		t.Fatal("forget committed text or was unhandled")
	}
	cl.key(policy.KeyEscape, 0)
	if firstText(cl.typ("shiyan")) != "试验" {
		t.Fatal("forgot the first word instead of the highlight")
	}
	cl.key(policy.KeyEscape, 0)
	// Shift+Delete must not alias Ctrl+Delete through librime's fallback.
	highlightWord(t, cl, "shiyan", "试验")
	cl.key(policy.KeyDelete, policy.ShiftMask)
	cl.key(policy.KeyEscape, 0)
	if firstText(cl.typ("shiyan")) != "试验" {
		t.Fatal("Shift+Delete forgot a word")
	}
	cl.key(policy.KeyEscape, 0)
	highlightWord(t, cl, "shiyan", "试验")
	if cl.policy().Lookup(k, m).Disposition() != policy.Ask {
		t.Fatal("composing forget key is not ASK")
	}
	if r := cl.key(k, m); len(r.Commit) != 0 || r.Flags&ipc.ResultHandled == 0 {
		t.Fatal("forget committed text or was unhandled")
	}
	cl.key(policy.KeyEscape, 0)
	if firstText(cl.typ("shiyan")) != "实验" {
		t.Fatal("forget did not remove learned ranking")
	}
	cl.key(policy.KeyEscape, 0)
	highlightWord(t, cl, "nihao", "泥好")
	cl.key(k, m)
	cl.key(policy.KeyEscape, 0)
	if hasCandidate(cl.typ("nihao"), "泥好") {
		t.Fatal("learned compound still appears after forgetting")
	}
	cl.key(policy.KeyEscape, 0)
	if !raceEnabled {
		cycle := func() {
			for _, key := range []struct{ k, m uint32 }{{'s', 0}, {'h', 0}, {'i', 0}, {'y', 0}, {'a', 0}, {'n', 0}, {k, m}, {policy.KeyEscape, 0}} {
				cl.seq++
				cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: key.k, Mods: key.m}).Append(cl.buf[:0]))
				for {
					typ, payload, err := cl.r.Next()
					if err != nil {
						t.Fatal(err)
					}
					if typ == ipc.TypeResult {
						if err := cl.res.Decode(payload); err != nil {
							t.Fatal(err)
						}
						break
					}
				}
			}
		}
		if n := testing.AllocsPerRun(20, cycle); n != 0 {
			t.Fatalf("forget cycle: %v Go allocations", n)
		}
	}
	cl.typ("shiyan")
	e.TakeStats()
	cl.key(k, m)
	stats := e.TakeStats()
	t.Logf("forget key: %v", stats.KeyTime.Max())
	if stats.KeyTime.Max() > time.Duration(float64(8*time.Millisecond)*perfTolerance()) {
		t.Fatal("forget key exceeded ASK budget")
	}
	cl.key(policy.KeyEscape, 0)
}

func checkForgetWord(t *testing.T, cl *client, shortcut, input string) {
	t.Helper()
	r := cl.typ(input)
	if len(r.Candidates) < 2 {
		t.Fatal("need two learning candidates")
	}
	original, word := string(r.Candidates[0].Text), string(r.Candidates[1].Text)
	cl.key(policy.KeyEscape, 0)
	for range 8 {
		highlightWord(t, cl, input, word)
		if got := string(cl.key(' ', 0).Commit); got != word {
			t.Fatalf("learned commit %q, want %q", got, word)
		}
	}
	if firstText(cl.typ(input)) != word {
		t.Fatalf("word %q did not learn", word)
	}
	cl.key(policy.KeyEscape, 0)
	highlightWord(t, cl, input, word)
	k, m, _ := parseKey(shortcut)
	if r := cl.key(k, m); len(r.Commit) != 0 || r.Flags&ipc.ResultHandled == 0 {
		t.Fatal("forget committed text or was unhandled")
	}
	cl.key(policy.KeyEscape, 0)
	if got := firstText(cl.typ(input)); got != original {
		t.Fatalf("forget ranking: first %q, want %q", got, original)
	}
	cl.key(policy.KeyEscape, 0)
}

func highlightWord(t *testing.T, cl *client, input, word string) {
	t.Helper()
	r := cl.typ(input)
	for range 20 {
		for i, candidate := range r.Candidates {
			if string(candidate.Text) != word {
				continue
			}
			for steps := 0; int(r.Highlighted) != i && steps < 10; steps++ {
				key := uint32(policy.KeyDown)
				if int(r.Highlighted) > i {
					key = policy.KeyUp
				}
				r = cl.key(key, 0)
			}
			if int(r.Highlighted) != i {
				t.Fatal("could not highlight word")
			}
			return
		}
		if r.Flags&ipc.ResultLastPage != 0 {
			break
		}
		r = cl.key(policy.KeyNext, 0)
	}
	t.Fatalf("candidate %q not found for %s", word, input)
}
