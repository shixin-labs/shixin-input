package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
