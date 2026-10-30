package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"shixin/internal/engine"
	"shixin/internal/generation"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/settings"
)

func TestCoordinatorRebuildsOldCompiler(t *testing.T) {
	s, _ := fixture(t)
	old := mustBuild(t, s, s.applied)
	// Keep a usable old deployed schema and the same settings snapshot,
	// but simulate compiler output from before the new editor bindings.
	if err := os.WriteFile(filepath.Join(old, "input", "rime_ice.custom.yaml"), []byte("old compiler output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.c.user, "active-generation"), []byte(filepath.Base(old)+"\n"), 0o600); err != nil {
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
	deadline := time.Now().Add(30 * time.Second)
	for {
		c, err := ipc.Dial(s.c.socket+".control", 10*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		select {
		case err := <-done:
			t.Fatalf("startup failed: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("coordinator not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	name, err := os.ReadFile(filepath.Join(s.c.user, "active-generation"))
	if err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(s.c.user, "generations", strings.TrimSpace(string(name)))
	if active == old {
		t.Fatal("startup reused old compiler output")
	}
	if matches, err := generation.MatchesSettings(active, s.applied); err != nil || !matches {
		t.Fatalf("new generation incompatible: %v", err)
	}
	cl := testDial(t, s.c.socket)
	defer cl.c.Close()
	if r := cl.typ("nihao"); len(r.Candidates) == 0 {
		t.Fatal("rebuilt generation cannot type")
	}
	cl.key(0xff1b)
}

func TestCoordinatorControlAndRecovery(t *testing.T) {
	s, _ := fixture(t)
	// Reuse an existing active generation while collecting a partial build
	// left by a crashed coordinator, including its sibling settings snapshot.
	recovery := mustBuild(t, s, s.applied)
	if err := os.WriteFile(filepath.Join(s.c.user, "active-generation"), []byte(filepath.Base(recovery)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(s.c.user, "generations", "gen-0000000000")
	cleanupFile(t, filepath.Join(orphan, "input", "partial.yaml"))
	cleanupFile(t, orphan+".json")
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
	deadline := time.Now().Add(30 * time.Second)
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
	deadline = time.Now().Add(5 * time.Second)
	for {
		_, dirErr := os.Stat(orphan)
		_, snapshotErr := os.Stat(orphan + ".json")
		if os.IsNotExist(dirErr) && os.IsNotExist(snapshotErr) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup did not collect the abandoned generation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if b, err := os.ReadFile(filepath.Join(s.c.user, "active-generation")); err != nil || strings.TrimSpace(string(b)) != filepath.Base(recovery) {
		t.Fatalf("startup did not reuse the recovery generation: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(recovery, "settings.json")); err != nil {
		t.Fatalf("cleanup removed recovery data: %v", err)
	}
	if err := runCoordinator(s.c, t.Logf, make(chan os.Signal)); !errors.Is(err, errLocked) {
		t.Fatalf("second coordinator: %v", err)
	}
	owner := dbOwner(t, s.c.user)
	s.applied.PageSize = 7
	if err := settings.Save(s.c.settings, s.applied); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Apply(s.c.socket); err != nil {
		t.Fatal(err)
	}
	if dbOwner(t, s.c.user) == owner {
		t.Fatal("control apply did not swap workers")
	}
	cl := testDial(t, s.c.socket)
	if r := cl.typ("nihao"); len(r.Candidates) != 7 {
		t.Fatal("changed page size not active")
	}
	cl.key(0xff1b)
	cl.c.Close()
	// A failed builder is reported over the same Settings control request,
	// while the live generation continues serving.
	if err := os.Rename(s.c.builder, s.c.builder+".disabled"); err != nil {
		t.Fatal(err)
	}
	s.applied.PageSize = 9
	if err := settings.Save(s.c.settings, s.applied); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Apply(s.c.socket); err == nil {
		t.Fatal("control hid builder failure")
	}
	// With no focused client, a worker may have released all sessions and
	// the LevelDB lock. Shixin's lock still identifies the live process.
	pid, err := os.ReadFile(filepath.Join(s.c.user, "shixin-engine.lock"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := strconv.Atoi(strings.TrimSpace(string(pid)))
	if err != nil || old <= 1 {
		t.Fatal("invalid worker pid")
	}
	still := testDial(t, s.c.socket)
	if r := still.typ("nihao"); len(r.Candidates) != 7 {
		t.Fatal("failed build affected active worker")
	}
	still.c.Close()
	// The coordinator reuses the persisted active generation after a crash.
	p, err := os.FindProcess(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(s.c.user, "shixin-engine.lock"))
		if err == nil && string(b) != string([]byte{}) {
			c, err := ipc.Dial(s.c.socket, 10*time.Millisecond)
			if err == nil {
				c.Close()
				if got := dbOwner(t, s.c.user); got != 0 && got != old {
					break
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not recover")
		}
		time.Sleep(10 * time.Millisecond)
	}
	fresh := testDial(t, s.c.socket)
	if r := fresh.typ("nihao"); len(r.Candidates) != 7 {
		t.Fatal("recovery did not use last active settings")
	}
}

// Upgrades stop the coordinator first, so it cannot resurrect the old
// worker while the upgraded front reconnects. Also supports legacy engines
// that own only the live user lock.
func TestStopForUpgrade(t *testing.T) {
	s, _ := fixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-user", s.c.user, "-settings", s.c.settings, "-socket", s.c.socket, "-lib", s.c.lib, "-data", s.c.data, "-builder", s.c.builder, "-log", "-")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill() })
	deadline := time.Now().Add(30 * time.Second)
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
	if err := engine.StopRunning(filepath.Join(s.c.user, "supervisor"), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := engine.StopRunning(s.c.user, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("coordinator did not exit")
	}
	release, err := engine.Lock(s.c.user)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := engine.UserLocksFree(s.c.user); err != nil {
		t.Fatal(err)
	}
}
