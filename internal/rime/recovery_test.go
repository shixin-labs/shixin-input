package rime

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Deliberately bypass Shixin's engine lock in isolated scratch processes to
// exercise librime's own protection against repairing another process's DB.
func TestUserDictionaryLockConflict(t *testing.T) {
	lib := os.Getenv("SHIXIN_LIBRIME")
	if lib == "" {
		t.Skip("requires our patched librime; set SHIXIN_LIBRIME")
	}
	if role := os.Getenv("SHIXIN_RECOVERY_ROLE"); role != "" {
		userDictionaryProcess(t, lib, os.Getenv("SHIXIN_RECOVERY_ROOT"), role)
		return
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	child := func(role string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUserDictionaryLockConflict$", "-test.v")
		cmd.Env = append(os.Environ(), "SHIXIN_RECOVERY_ROLE="+role, "SHIXIN_RECOVERY_ROOT="+root)
		if rt := os.Getenv("SHIXIN_DYLD_INSERT"); rt != "" {
			cmd.Env = append(cmd.Env, "DYLD_INSERT_LIBRARIES="+rt)
		}
		return cmd
	}
	owner := child("owner")
	var log bytes.Buffer
	owner.Stderr = &log
	input, err := owner.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := owner.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		owner.Process.Kill()
		owner.Wait()
	})
	scanner := bufio.NewScanner(output)
	ready := false
	for scanner.Scan() {
		if scanner.Text() == "userdb-owner-ready" {
			ready = true
			break
		}
	}
	if !ready {
		err := owner.Wait()
		t.Fatalf("owner did not become ready: %v: %s", err, log.String())
	}
	if out, err := child("contender").CombinedOutput(); err != nil {
		t.Fatalf("contender: %v\n%s", err, out)
	}
	for _, name := range []string{"test.userdb/lost", "test.userdb.old"} {
		if _, err := os.Stat(filepath.Join(root, "user", name)); !os.IsNotExist(err) {
			t.Fatalf("lock conflict triggered recovery: %s (%v)", name, err)
		}
	}
	if _, err := io.WriteString(input, "continue\n"); err != nil {
		t.Fatal(err)
	}
	input.Close()
	for scanner.Scan() {
		t.Log(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Wait(); err != nil {
		t.Fatalf("owner: %v\n%s", err, log.String())
	}
	if out, err := child("verify").CombinedOutput(); err != nil {
		t.Fatalf("learned word after restart: %v\n%s", err, out)
	}
}

func userDictionaryProcess(t *testing.T, lib, root, role string) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	l, err := Load(lib)
	if err != nil {
		t.Fatal(err)
	}
	data, err := filepath.Abs("../engine/testdata/rime")
	if err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(root, "user")
	logs := filepath.Join(root, role)
	for _, dir := range []string{user, logs} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	l.Setup(Traits{SharedDataDir: data, UserDataDir: user, LogDir: logs,
		AppName: "rime.shixin-recovery-test", MinLogLevel: 2, Modules: []string{"default"}})
	l.Initialize()
	defer l.Finalize()
	if role == "owner" {
		l.StartMaintenance(true)
		l.JoinMaintenanceThread()
	}
	s, err := l.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Destroy()
	s.SetOption("ascii_mode", false)
	var status Status
	if !s.ReadStatus(&status) || string(status.SchemaID) != "test" {
		t.Fatalf("unexpected schema: %q", status.SchemaID)
	}
	var c Context
	query := func() {
		s.ClearComposition()
		if !s.SimulateKeySequence("shiyan") || !s.ReadContext(&c) || len(c.Candidates) == 0 {
			t.Fatal("no candidates")
		}
	}
	learn := func() {
		for range 8 {
			query()
			pick := -1
			for i, candidate := range c.Candidates {
				if string(candidate.Text) == "试验" {
					pick = i
				}
			}
			if pick < 0 || !s.SelectCandidateOnCurrentPage(pick) {
				t.Fatal("cannot select learning candidate")
			}
			commit, ok := s.ReadCommit(nil)
			if !ok || string(commit) != "试验" {
				t.Fatal("learning candidate was not committed")
			}
		}
	}
	checkLearned := func() {
		query()
		if string(c.Candidates[0].Text) != "试验" {
			t.Fatal("learned word did not retain first rank")
		}
		s.ClearComposition()
	}
	switch role {
	case "owner":
		query()
		if string(c.Candidates[0].Text) == "试验" {
			t.Fatal("learning candidate already ranks first in the static dictionary")
		}
		learn()
		checkLearned()
		fmt.Println("userdb-owner-ready")
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		learn() // writes after the conflicting open must survive too
		checkLearned()
	case "contender":
		l.JoinMaintenanceThread() // also joins any erroneously queued recovery
		query()                   // static dictionary remains usable
		if string(c.Candidates[0].Text) == "试验" {
			t.Fatal("contender unexpectedly opened the owner's user dictionary")
		}
	case "verify":
		checkLearned()
	default:
		t.Fatalf("unknown process role %q", role)
	}
}
