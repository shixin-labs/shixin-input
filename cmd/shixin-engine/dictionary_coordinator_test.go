package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"shixin/internal/dictionary"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/settings"
)

func hasText(r *ipc.Result, text string) bool {
	for _, c := range r.Candidates {
		if string(c.Text) == text {
			return true
		}
	}
	return false
}

// Import, export and clear through the real control socket: each run stops
// the serving worker, runs the maintenance child on a private copy and
// restarts typing, leaving no journal, work folder or snapshot behind.
func TestCoordinatorDictionaryMaintenance(t *testing.T) {
	s, root := fixture(t)
	s.applied.Phrases = []settings.Phrase{{Code: "yx", Text: "邮箱"}}
	if err := settings.Save(s.c.settings, s.applied); err != nil {
		t.Fatal(err)
	}
	sig := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- runCoordinator(s.c, t.Logf, sig) }()
	t.Cleanup(func() {
		sig <- syscall.SIGTERM
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("coordinator did not stop")
		}
	})
	deadline := time.Now().Add(60 * time.Second)
	for {
		c, err := ipc.Dial(s.c.socket+".control", 10*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("coordinator not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	const word, code = "拾新输入", "shixinshuru"
	typed := func() bool {
		cl := testDial(t, s.c.socket)
		defer cl.c.Close()
		found := hasText(cl.typ(code), word)
		cl.key(0xff1b)
		return found
	}
	if typed() {
		t.Fatal("fixture already knows the test word")
	}

	// Malformed input is rejected before typing is interrupted.
	bad := filepath.Join(root, "bad.txt")
	if err := os.WriteFile(bad, []byte("私密\tbad!code\t1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Dictionary(s.c.socket, "import", bad); err == nil {
		t.Fatal("malformed import accepted")
	}
	if err := lifecycle.Dictionary(s.c.socket, "import", "relative.txt"); err == nil {
		t.Fatal("relative import path accepted")
	}

	source := filepath.Join(root, "words.txt")
	if err := os.WriteFile(source, []byte(word+"\tshi xin shu ru\t37\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Dictionary(s.c.socket, "import", source); err != nil {
		t.Fatal(err)
	}
	if !typed() {
		t.Fatal("imported word is not offered after restart")
	}

	backup := filepath.Join(root, "export.json")
	if err := lifecycle.Dictionary(s.c.socket, "export", backup); err != nil {
		t.Fatal(err)
	}
	b, err := dictionary.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range b.Words {
		found = found || e == (dictionary.Entry{Text: word, Code: "shi xin shu ru", Count: 37})
	}
	if !found || len(b.Phrases) != 1 || b.Phrases[0].Text != "邮箱" {
		t.Fatalf("export lost the learned word or custom phrases: %+v", b)
	}
	table := filepath.Join(root, "export.txt")
	if err := lifecycle.Dictionary(s.c.socket, "export", table); err != nil {
		t.Fatal(err)
	}
	if b, err := dictionary.ReadFile(table); err != nil || len(b.Words) == 0 || len(b.Phrases) != 0 {
		t.Fatalf("table export: %v %+v", err, b)
	}
	if err := lifecycle.Dictionary(s.c.socket, "export", filepath.Join(s.c.user, "export.json")); err == nil {
		t.Fatal("export into live data accepted")
	}
	if err := lifecycle.Dictionary(s.c.socket, "export", s.c.settings); err == nil {
		t.Fatal("export over settings accepted")
	}

	if err := lifecycle.Dictionary(s.c.socket, "clear", ""); err != nil {
		t.Fatal(err)
	}
	if typed() {
		t.Fatal("cleared word is still offered")
	}
	if roots, _ := filepath.Glob(filepath.Join(s.c.user, "backups", "dictionary-*", learnedDB)); len(roots) != 2 {
		t.Fatalf("import and clear should each keep a recovery copy: %q", roots)
	}
	// Restoring the backup brings the word back.
	if err := lifecycle.Dictionary(s.c.socket, "import", backup); err != nil {
		t.Fatal(err)
	}
	if !typed() {
		t.Fatal("restored word is not offered")
	}

	for _, pattern := range []string{
		filepath.Join(s.c.user, "dictionary-work-*"),
		filepath.Join(s.c.user, "dictionary-recovery.json"),
		filepath.Join(filepath.Dir(s.c.socket), "dictionary-import-*"),
		filepath.Join(root, ".shixin-dictionary-*"),
	} {
		if left, _ := filepath.Glob(pattern); len(left) != 0 {
			t.Fatalf("maintenance left %q", left)
		}
	}
}
