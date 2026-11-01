package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func cleanupFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireCleanupFile(t *testing.T, path string) {
	t.Helper()
	if b, err := os.ReadFile(path); err != nil || string(b) != "preserve me" {
		t.Errorf("file changed: %s: %q, %v", path, b, err)
	}
}

func requireRemoved(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("expected removed: %s: %v", path, err)
		}
	}
}

func TestGenerationCleanupPreservesData(t *testing.T) {
	user := t.TempDir()
	dir := filepath.Join(user, "generations")
	keep := []string{
		"gen-1/build/schema.yaml", "gen-1.json", "gen-1.status",
		"gen-2/build/schema.yaml", "gen-2.json",
		"notes/data", "gen-manual/data", "gen-/data", "gen-3x/data",
		"gen-7", "gen-8.json/data", "gen-9.json.backup", "gen-8.status/data", "gen-9.status.backup",
	}
	for _, name := range keep {
		cleanupFile(t, filepath.Join(dir, name))
	}
	remove := []string{"gen-3", "gen-4", "gen-5", "gen-3.json", "gen-4.json", "gen-6.json", "gen-3.status", "gen-6.status"}
	cleanupFile(t, filepath.Join(dir, "gen-3/build/schema.yaml"))
	cleanupFile(t, filepath.Join(dir, "gen-4/input/partial.yaml"))
	if err := os.Mkdir(filepath.Join(dir, "gen-5"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range remove[3:] {
		cleanupFile(t, filepath.Join(dir, name))
	}
	// Neither links in a deleted build nor generation-shaped links may
	// take cleanup into shared data or the user's learned dictionary.
	outside := filepath.Join(t.TempDir(), "shared.dict.yaml")
	live := filepath.Join(user, "rime_ice.userdb", "data")
	cleanupFile(t, outside)
	cleanupFile(t, live)
	for name, target := range map[string]string{"gen-3/shared": filepath.Dir(outside), "gen-9": filepath.Dir(live)} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	p, err := planGenerationCleanup(user, filepath.Join(dir, "gen-1"), filepath.Join(dir, "gen-2"))
	if err != nil || p == nil {
		t.Fatalf("plan: %v", err)
	}
	// Settings applies after the snapshot must survive even if background
	// deletion hasn't started yet.
	cleanupFile(t, filepath.Join(dir, "gen-10/build/new.yaml"))
	cleanupFile(t, filepath.Join(dir, "gen-10.json"))
	p.run(make(chan struct{}), t.Errorf)
	for _, name := range remove {
		requireRemoved(t, filepath.Join(dir, name))
	}
	for _, name := range append(keep, "gen-10/build/new.yaml", "gen-10.json") {
		requireCleanupFile(t, filepath.Join(dir, name))
	}
	requireCleanupFile(t, outside)
	requireCleanupFile(t, live)
	if info, err := os.Lstat(filepath.Join(dir, "gen-9")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("generation-shaped symlink changed: %v", err)
	}
}

func TestGenerationCleanupReplacedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		// It checks that the open root follows a renamed directory; Windows
		// refuses to rename a directory with open handles at all.
		t.Skip("Windows doesn't rename directories with open handles")
	}
	user := t.TempDir()
	dir := filepath.Join(user, "generations")
	cleanupFile(t, filepath.Join(dir, "gen-1/build/old.yaml"))
	p, err := planGenerationCleanup(user)
	if err != nil {
		t.Fatal(err)
	}
	// Replacement links are removed as links, without following their target.
	if err := os.RemoveAll(filepath.Join(dir, "gen-1")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "shared.dict.yaml")
	cleanupFile(t, outside)
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(dir, "gen-1")); err != nil {
		t.Fatal(err)
	}
	// The open root also pins the original directory if it is renamed.
	moved := filepath.Join(user, "moved-generations")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "gen-1/build/new.yaml")
	cleanupFile(t, replacement)
	p.run(make(chan struct{}), t.Errorf)
	requireRemoved(t, filepath.Join(moved, "gen-1"))
	requireCleanupFile(t, outside)
	requireCleanupFile(t, replacement)
}

func TestGenerationCleanupCanceled(t *testing.T) {
	user := t.TempDir()
	path := filepath.Join(user, "generations", "gen-1", "input", "partial.yaml")
	cleanupFile(t, path)
	p, err := planGenerationCleanup(user)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	close(stop)
	p.run(stop, t.Errorf)
	requireCleanupFile(t, path)
}

func TestGenerationCleanupLinkedActive(t *testing.T) {
	user := t.TempDir()
	dir := filepath.Join(user, "generations")
	path := filepath.Join(dir, "gen-1", "build", "active.yaml")
	cleanupFile(t, path)
	active := filepath.Join(dir, "gen-2")
	if err := os.Symlink("gen-1", active); err != nil {
		t.Fatal(err)
	}
	if p, err := planGenerationCleanup(user, active); p != nil || err == nil {
		t.Fatalf("cleanup planned with aliased active data: %v, %v", p, err)
	}
	requireCleanupFile(t, path)
}

func TestGenerationCleanupContinuesAfterFailure(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("requires Unix permission enforcement for an unprivileged user")
	}
	user := t.TempDir()
	dir := filepath.Join(user, "generations")
	blocked := filepath.Join(dir, "gen-1")
	cleanupFile(t, filepath.Join(blocked, "data"))
	cleanupFile(t, filepath.Join(dir, "gen-2", "data"))
	if err := os.Chmod(blocked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(blocked, 0o700) })
	p, err := planGenerationCleanup(user)
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	p.run(make(chan struct{}), func(string, ...any) { failures++ })
	if failures != 1 {
		t.Fatalf("%d cleanup failures reported, want 1", failures)
	}
	requireCleanupFile(t, filepath.Join(blocked, "data"))
	requireRemoved(t, filepath.Join(dir, "gen-2"))
}

func TestGenerationCleanupUnavailable(t *testing.T) {
	for _, kind := range []string{"missing", "file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			user := t.TempDir()
			path := filepath.Join(user, "generations")
			outside := filepath.Join(t.TempDir(), "gen-1", "data")
			cleanupFile(t, outside)
			switch kind {
			case "file":
				cleanupFile(t, path)
			case "symlink":
				if err := os.Symlink(filepath.Dir(filepath.Dir(outside)), path); err != nil {
					t.Fatal(err)
				}
			}
			p, err := planGenerationCleanup(user)
			if p != nil || (err != nil) != (kind != "missing") {
				t.Fatalf("plan: %v, %v", p, err)
			}
			requireCleanupFile(t, outside)
		})
	}
}
