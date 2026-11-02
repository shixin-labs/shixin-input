package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
)

func TestDictionaryCommitAndRecovery(t *testing.T) {
	for _, stage := range []string{"before-rename", "old-renamed", "new-renamed", "new-database"} {
		t.Run(stage, func(t *testing.T) {
			user := t.TempDir()
			root := filepath.Join(user, "backups", "dictionary-test")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			old := filepath.Join(user, learnedDB)
			if stage == "old-renamed" || stage == "new-renamed" {
				old = filepath.Join(root, learnedDB)
			}
			if stage != "new-database" {
				if err := os.Mkdir(old, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(old, "fixture"), []byte("original"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "new-renamed" || stage == "new-database" {
				live := filepath.Join(user, learnedDB)
				if err := os.Mkdir(live, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(live, "fixture"), []byte("new"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			b, _ := json.Marshal(dictionaryRecovery{Backup: "dictionary-test", HadDB: stage != "new-database"})
			journal := filepath.Join(user, "dictionary-recovery.json")
			if err := os.WriteFile(journal, b, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := recoverDictionaryLocked(user); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(journal); !os.IsNotExist(err) {
				t.Fatal("recovery journal was not cleared")
			}
			if stage == "new-database" {
				if _, err := os.Stat(filepath.Join(user, learnedDB)); !os.IsNotExist(err) {
					t.Fatal("incomplete new database survived recovery")
				}
				return
			}
			got, err := os.ReadFile(filepath.Join(user, learnedDB, "fixture"))
			if err != nil || string(got) != "original" {
				t.Fatal("original database was not restored", err)
			}
		})
	}
}

func TestDictionaryClearKeepsRecoveryCopy(t *testing.T) {
	user, work := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(user, learnedDB), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, learnedDB, "fixture"), []byte("learned"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := commitDictionary(user, work, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(user, learnedDB)); !os.IsNotExist(err) {
		t.Fatal("clear kept the active dictionary")
	}
	paths, _ := filepath.Glob(filepath.Join(user, "backups", "dictionary-*", learnedDB, "fixture"))
	if len(paths) != 1 {
		t.Fatal("clear did not preserve a recovery copy")
	}
	got, err := os.ReadFile(paths[0])
	if err != nil || string(got) != "learned" {
		t.Fatal("recovery copy changed", err)
	}
}

func TestDictionaryCleanupPreservesExternalDirectories(t *testing.T) {
	user := t.TempDir()
	owned, external := filepath.Join(user, "dictionary-work-owned"), filepath.Join(user, "dictionary-work-external")
	for _, dir := range []string{owned, external} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(owned, ".shixin-dictionary-work"), []byte("shixin-dictionary-work-v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverDictionaryLocked(user); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatal("abandoned owned work directory survived")
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatal("external directory was removed")
	}
}

func TestDictionaryExportCannotReplaceLiveData(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "Rime")
	if err := os.Mkdir(user, 0o700); err != nil {
		t.Fatal(err)
	}
	c := config{user: user, settings: filepath.Join(root, "settings.json")}
	for _, path := range []string{filepath.Join(user, "export.json"), c.settings} {
		if checkDictionaryExportPath(c, path) == nil {
			t.Fatal("export can replace live data")
		}
	}
	if err := checkDictionaryExportPath(c, filepath.Join(root, "backup.json")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(user, alias); err != nil {
		t.Skip("symlink creation unavailable")
	}
	if checkDictionaryExportPath(c, filepath.Join(alias, "export.json")) == nil {
		t.Fatal("directory alias bypassed live-data protection")
	}
}

// writeTree creates files (relative path → content) below root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDictionaryCopyTree(t *testing.T) {
	source := filepath.Join(t.TempDir(), learnedDB)
	files := map[string]string{"CURRENT": "MANIFEST-1\n", "000001.ldb": "table", "nested/LOG": "log"}
	writeTree(t, source, files)
	target := filepath.Join(t.TempDir(), learnedDB)
	if err := copyDictionaryTree(source, target); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(target, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
	// A copy never follows links out of the database or overwrites files.
	if err := copyDictionaryTree(source, target); err == nil {
		t.Fatal("copy replaced an existing target")
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(source, "link")); err != nil {
		t.Skip("symlink creation unavailable")
	}
	if err := copyDictionaryTree(source, filepath.Join(t.TempDir(), learnedDB)); err == nil {
		t.Fatal("copy followed a symlink")
	}
}

func TestDictionaryCommitImport(t *testing.T) {
	for _, hadDB := range []bool{true, false} {
		user, work := t.TempDir(), t.TempDir()
		if hadDB {
			writeTree(t, filepath.Join(user, learnedDB), map[string]string{"fixture": "old"})
		}
		writeTree(t, filepath.Join(work, learnedDB), map[string]string{"fixture": "new"})
		if err := commitDictionary(user, work, hadDB, false); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(user, learnedDB, "fixture"))
		if err != nil || string(got) != "new" {
			t.Fatalf("hadDB=%t: import not committed: %q %v", hadDB, got, err)
		}
		if _, err := os.Stat(filepath.Join(user, "dictionary-recovery.json")); !os.IsNotExist(err) {
			t.Fatal("journal survived a complete commit")
		}
		backups, _ := filepath.Glob(filepath.Join(user, "backups", "dictionary-*", learnedDB, "fixture"))
		if hadDB != (len(backups) == 1) {
			t.Fatalf("hadDB=%t: backups %q", hadDB, backups)
		}
		// A second commit uses a fresh backup folder; the first is kept.
		writeTree(t, filepath.Join(work, learnedDB), map[string]string{"fixture": "newer"})
		if err := commitDictionary(user, work, true, false); err != nil {
			t.Fatal(err)
		}
		if roots, _ := filepath.Glob(filepath.Join(user, "backups", "dictionary-*")); len(roots) != 2 {
			t.Fatalf("hadDB=%t: backup folders %q", hadDB, roots)
		}
	}
	// A missing import leaves the journal for recovery, which restores the
	// original before any engine can attach.
	user, work := t.TempDir(), t.TempDir()
	writeTree(t, filepath.Join(user, learnedDB), map[string]string{"fixture": "original"})
	if err := commitDictionary(user, work, true, false); err == nil {
		t.Fatal("commit without an import succeeded")
	}
	if err := recoverDictionaryLocked(user); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(user, learnedDB, "fixture")); err != nil || string(got) != "original" {
		t.Fatalf("original not restored: %q %v", got, err)
	}
}

// A journal that is damaged or points outside backups must never move
// files; startup reports it instead.
func TestDictionaryRecoveryRejectsInvalidJournal(t *testing.T) {
	for _, journal := range []string{
		"not json",
		`{"backup":"../escape","had_db":true}`,
		`{"backup":"dictionary-../../x","had_db":true}`,
		`{"backup":"other-1","had_db":true}`,
		`{"backup":"","had_db":false}`,
		`{"backup":"dictionary-1","had_db":true,"pad":"` + strings.Repeat("x", 5000) + `"}`,
	} {
		user := t.TempDir()
		writeTree(t, filepath.Join(user, learnedDB), map[string]string{"fixture": "live"})
		writeTree(t, user, map[string]string{"dictionary-recovery.json": journal})
		if err := recoverDictionaryLocked(user); err == nil {
			t.Fatalf("accepted journal %.40q", journal)
		}
		if got, err := os.ReadFile(filepath.Join(user, learnedDB, "fixture")); err != nil || string(got) != "live" {
			t.Fatalf("invalid journal moved the live database: %v", err)
		}
		if _, err := os.Stat(filepath.Join(user, "dictionary-recovery.json")); err != nil {
			t.Fatal("invalid journal was discarded instead of kept for recovery")
		}
	}
}

func TestDictionaryRecoveryRecreatesMissingBackupFolder(t *testing.T) {
	user := t.TempDir()
	writeTree(t, filepath.Join(user, learnedDB), map[string]string{"fixture": "partial"})
	writeTree(t, user, map[string]string{"dictionary-recovery.json": `{"backup":"dictionary-gone","had_db":false}`})
	if err := recoverDictionaryLocked(user); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(user, learnedDB)); !os.IsNotExist(err) {
		t.Fatal("partial import stayed live")
	}
	if got, err := os.ReadFile(filepath.Join(user, "backups", "dictionary-gone", "interrupted.userdb", "fixture")); err != nil || string(got) != "partial" {
		t.Fatalf("partial import not preserved: %v", err)
	}
}

// Clearing never copies the database, so it works for any database size
// or contents, and preserves the original untouched.
func TestDictionaryClearWithoutCopy(t *testing.T) {
	root := t.TempDir()
	c := config{user: filepath.Join(root, "user"), settings: filepath.Join(root, "settings.json")}
	writeTree(t, filepath.Join(c.user, learnedDB), map[string]string{"fixture": "learned"})
	// A non-regular entry would make a copy fail.
	if err := os.Symlink("fixture", filepath.Join(c.user, learnedDB, "link")); err != nil {
		t.Skip("symlink creation unavailable")
	}
	if err := runDictionaryTask(c, "clear", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(c.user, learnedDB)); !os.IsNotExist(err) {
		t.Fatal("clear kept the live database")
	}
	if paths, _ := filepath.Glob(filepath.Join(c.user, "backups", "dictionary-*", learnedDB, "link")); len(paths) != 1 {
		t.Fatal("clear did not keep the original database")
	}
	if work, _ := filepath.Glob(filepath.Join(c.user, "dictionary-work-*")); len(work) != 0 {
		t.Fatalf("work folders left behind: %q", work)
	}
	// Clearing an empty dictionary is a successful no-op.
	if err := runDictionaryTask(c, "clear", ""); err != nil {
		t.Fatal(err)
	}
	if err := runDictionaryTask(c, "delete", ""); err == nil {
		t.Fatal("unsupported action accepted")
	}
}

func TestDictionaryExportPathVariants(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "Rime")
	if err := os.Mkdir(user, 0o700); err != nil {
		t.Fatal(err)
	}
	c := config{user: user, settings: filepath.Join(root, "settings.json")}
	for _, path := range []string{
		user, // the live folder itself
		filepath.Join(user, "sub", "..", "export.json"),
		filepath.Join(root, "RIME", "export.json"), // case-insensitive volumes
		filepath.Join(root, "SETTINGS.json"),
	} {
		if checkDictionaryExportPath(c, path) == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "Rime-backup.json"), // shares a prefix only
		filepath.Join(root, "..Rime.json"),
	} {
		if err := checkDictionaryExportPath(c, path); err != nil {
			t.Fatalf("rejected %s: %v", path, err)
		}
	}
	if checkDictionaryExportPath(c, filepath.Join(root, "missing", "export.json")) == nil {
		t.Fatal("accepted a missing folder")
	}
	// settings.json reached through a link is still protected.
	if err := os.WriteFile(filepath.Join(root, "real.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.json"), c.settings); err != nil {
		t.Skip("symlink creation unavailable")
	}
	if checkDictionaryExportPath(c, filepath.Join(root, "real.json")) == nil {
		t.Fatal("export can replace the settings link target")
	}
}

// The control socket forwards dictionary requests with their action, path
// and start-by deadline, and reports the coordinator's result.
func TestAcceptControlDictionaryRequest(t *testing.T) {
	dir, err := ipc.TempDir("sx-ctl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "engine.sock")
	l, err := ipc.Listen(socket + ".control")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	requests, stop := make(chan applyRequest), make(chan struct{})
	defer close(stop)
	go acceptControl(l, requests, stop)
	errc := make(chan error, 1)
	go func() { errc <- lifecycle.Dictionary(socket, "import", "/tmp/words.txt") }()
	var r applyRequest
	select {
	case r = <-requests:
	case <-time.After(5 * time.Second):
		t.Fatal("request not forwarded")
	}
	if r.stop || r.dictionary == nil || r.dictionary.Action != "import" || r.dictionary.Path != "/tmp/words.txt" {
		t.Fatalf("forwarded %+v", r)
	}
	if left := time.Until(r.dictionaryStartBy); left < 25*time.Second || left > 30*time.Second {
		t.Fatalf("start-by deadline %v", left)
	}
	r.done <- errors.New("dictionary: finish the current composition and retry")
	if err := <-errc; err == nil || !strings.Contains(err.Error(), "finish the current composition") {
		t.Fatalf("reply %v", err)
	}
	// With nobody receiving, the queue rejects at once instead of blocking.
	if err := lifecycle.Dictionary(socket, "clear", ""); err == nil || !strings.Contains(err.Error(), "queue full") {
		t.Fatalf("busy coordinator: %v", err)
	}
}
