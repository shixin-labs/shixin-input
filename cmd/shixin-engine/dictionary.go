package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shixin/internal/dictionary"
	"shixin/internal/engine"
	"shixin/internal/lifecycle"
	"shixin/internal/rime"
	"shixin/internal/settings"
)

const learnedDB = "rime_ice.userdb"

// dictionary is serialized with generation builds. Once the serving worker
// exits, a separate locked-thread child operates on a private database copy.
// Clients reconnect normally and PASS while maintenance owns the database.
func (s *coordinator) dictionary(req lifecycle.Request) error {
	if req.Action != "import" && req.Action != "export" && req.Action != "clear" {
		return errors.New("dictionary: unsupported operation")
	}
	if req.Action != "clear" {
		if !filepath.IsAbs(req.Path) || len(req.Path) > 2048 {
			return errors.New("dictionary: choose an absolute file path")
		}
		// An export must never replace live engine/configuration files.
		path, err := filepath.Abs(req.Path)
		if err != nil {
			return errors.New("dictionary: invalid path")
		}
		if req.Action == "export" {
			if err := checkDictionaryExportPath(s.c, path); err != nil {
				return err
			}
		}
		if req.Action == "import" {
			// Reject bad input before interrupting typing; snapshot the file so
			// changing/removing the selected source cannot affect maintenance.
			b, err := dictionary.ReadFile(req.Path)
			if err != nil {
				return err
			}
			if len(b.Words) == 0 {
				return errors.New("dictionary: no learned words to import")
			}
			f, err := os.CreateTemp(filepath.Dir(s.c.socket), "dictionary-import-")
			if err != nil {
				return errors.New("dictionary: cannot prepare import")
			}
			defer os.Remove(f.Name())
			err = dictionary.WriteTable(f, b.Words)
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				return errors.New("dictionary: cannot prepare import")
			}
			req.Path = f.Name()
		}
	}
	old := s.active
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := old.call(lifecycle.OpMigrate)
		if err == nil {
			break
		}
		if !errors.Is(err, engine.ErrComposing) || time.Now().After(deadline) {
			return errors.New("dictionary: finish the current composition and retry")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Give clients the same bounded drain window as a generation handoff,
	// so commits preceding the migration marker reach their text field.
	deadline = time.Now().Add(2 * time.Second)
	for {
		_, err := old.call(lifecycle.OpDrained)
		if err == nil || !errors.Is(err, engine.ErrClients) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	old.stop() // waits even when a worker has to be killed
	s.active = nil
	exe, err := os.Executable()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		cmd := exec.CommandContext(ctx, exe, "-dictionary-task", req.Action, "-dictionary-path", req.Path,
			"-lib", s.c.lib, "-data", s.c.data, "-user", s.c.user, "-settings", s.c.settings, "-log", "-")
		// Native diagnostics may quote words or source paths. Never copy
		// maintenance stderr/stdout to product logs or the lifecycle reply.
		err = cmd.Run()
		cancel()
	}
	operationErr := err
	// A crash/timeout between the two renames restores the original before
	// any process is allowed to attach a session again.
	if err := recoverDictionaryLocked(s.c.user); err != nil {
		return err
	}
	replacement, restartErr := startChild(s.c, old.dir)
	if restartErr == nil {
		restartErr = s.activate(replacement)
	}
	if restartErr != nil {
		if replacement != nil {
			replacement.kill()
		}
		return errors.New("dictionary: engine restart failed; restart Shixin")
	}
	s.active = replacement
	if operationErr != nil {
		return errors.New("dictionary: operation failed; the original dictionary was preserved")
	}
	return nil
}

type dictionaryRecovery struct {
	Backup string `json:"backup"`
	HadDB  bool   `json:"had_db"`
}

// recoverDictionaryLocked is also called before coordinator startup. It uses
// the same lock as real sessions, including after a maintenance child dies.
func recoverDictionaryLocked(user string) error {
	release, err := engine.Lock(user)
	if err != nil {
		return err
	}
	defer release()
	if err := engine.UserLocksFree(user); err != nil {
		return err
	}
	if err := recoverDictionary(user); err != nil {
		return err
	}
	// The helper's defer cannot run after SIGKILL. Only remove workspaces
	// carrying our ownership marker; preserve similarly named external files.
	entries, err := os.ReadDir(user)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "dictionary-work-") {
			continue
		}
		root := filepath.Join(user, entry.Name())
		f, err := os.Open(filepath.Join(root, ".shixin-dictionary-work"))
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(f, 64))
		f.Close()
		if err == nil && string(b) == "shixin-dictionary-work-v1\n" {
			if err := os.RemoveAll(root); err != nil {
				return err
			}
		}
	}
	return nil
}

func recoverDictionary(user string) error {
	journal := filepath.Join(user, "dictionary-recovery.json")
	b, err := os.ReadFile(journal)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("dictionary: cannot read recovery journal")
	}
	var r dictionaryRecovery
	if len(b) > 4096 || json.Unmarshal(b, &r) != nil || filepath.Base(r.Backup) != r.Backup || !strings.HasPrefix(r.Backup, "dictionary-") {
		return errors.New("dictionary: invalid recovery journal; original backup needs recovery")
	}
	root := filepath.Join(user, "backups", r.Backup)
	backup, live := filepath.Join(root, learnedDB), filepath.Join(user, learnedDB)
	_, backupErr := os.Lstat(backup)
	if r.HadDB && errors.Is(backupErr, os.ErrNotExist) {
		// The old database rename had not happened yet.
		if _, err := os.Lstat(live); err != nil {
			return errors.New("dictionary: recovery copy is missing")
		}
		return os.Remove(journal)
	}
	if r.HadDB && backupErr != nil {
		return errors.New("dictionary: cannot inspect recovery copy")
	}
	if _, err := os.Lstat(live); err == nil {
		// The backup folder may have been removed by hand; recreate it so
		// a partial import is still preserved instead of blocking startup.
		if err := os.MkdirAll(root, 0o700); err != nil {
			return errors.New("dictionary: cannot preserve interrupted import")
		}
		if err := os.Rename(live, filepath.Join(root, "interrupted.userdb")); err != nil {
			return errors.New("dictionary: cannot preserve interrupted import")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("dictionary: cannot inspect current dictionary")
	}
	if r.HadDB {
		if err := os.Rename(backup, live); err != nil {
			return errors.New("dictionary: cannot restore original dictionary")
		}
	}
	return os.Remove(journal)
}

func copyDictionaryTree(source, target string) error {
	var size int64
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.Mkdir(dst, 0o700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("dictionary: unsupported database file")
		}
		size += info.Size()
		if size > 1<<30 {
			return errors.New("dictionary: database exceeds maintenance limit")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, io.LimitReader(in, (1<<30)+1))
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

// commitDictionary keeps the previous database in backups. The recovery
// journal makes the pair of renames recoverable after process termination.
func commitDictionary(user, work string, hadDB, clear bool) error {
	backups := filepath.Join(user, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		return err
	}
	root, err := os.MkdirTemp(backups, "dictionary-")
	if err != nil {
		return err
	}
	r := dictionaryRecovery{Backup: filepath.Base(root), HadDB: hadDB}
	b, _ := json.Marshal(r)
	journal := filepath.Join(user, "dictionary-recovery.json")
	f, err := os.OpenFile(journal, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(journal)
		return errors.New("dictionary: cannot record recovery")
	}
	live := filepath.Join(user, learnedDB)
	if hadDB {
		if err := os.Rename(live, filepath.Join(root, learnedDB)); err != nil {
			return err
		}
	}
	if !clear {
		if err := os.Rename(filepath.Join(work, learnedDB), live); err != nil {
			return err
		}
	}
	return os.Remove(journal)
}

func runDictionaryTask(c config, action, path string) error {
	if action != "import" && action != "export" && action != "clear" {
		return errors.New("dictionary: unsupported operation")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	release, err := engine.Lock(c.user)
	if err != nil {
		return err
	}
	defer release()
	if err := engine.UserLocksFree(c.user); err != nil {
		return err
	}
	if err := recoverDictionary(c.user); err != nil {
		return err
	}
	if action == "export" {
		if err := checkDictionaryExportPath(c, path); err != nil {
			return err
		}
	}
	work, err := os.MkdirTemp(c.user, "dictionary-work-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := os.WriteFile(filepath.Join(work, ".shixin-dictionary-work"), []byte("shixin-dictionary-work-v1\n"), 0o600); err != nil {
		return err
	}
	live := filepath.Join(c.user, learnedDB)
	_, err = os.Lstat(live)
	hadDB := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if action == "clear" {
		// Clearing only moves the database aside; it never needs a copy.
		return commitDictionary(c.user, work, hadDB, true)
	}
	if hadDB {
		if err := copyDictionaryTree(live, filepath.Join(work, learnedDB)); err != nil {
			return err
		}
	}
	var imported dictionary.Backup
	if action == "import" {
		imported, err = dictionary.ReadFile(path)
		if err != nil {
			return err
		}
	}
	table := filepath.Join(work, "transfer.txt")
	// Deployer initialization loads only explicitly named native modules;
	// it never schedules maintenance, creates sessions or opens live data.
	lib, err := rime.Load(c.lib)
	if err != nil {
		return err
	}
	lib.Setup(rime.Traits{SharedDataDir: c.data, UserDataDir: work, AppName: "rime.shixin", MinLogLevel: 3, Modules: []string{"default", "levers"}})
	lib.InitializeDeployer()
	if action == "import" {
		f, err := os.Create(table)
		if err != nil {
			lib.Finalize()
			return err
		}
		err = dictionary.WriteTable(f, imported.Words)
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			_, err = lib.DictionaryTable(table, true)
		}
		lib.Finalize()
		if err != nil {
			return err
		}
		return commitDictionary(c.user, work, hadDB, false)
	}
	var words []dictionary.Entry
	if hadDB {
		_, err = lib.DictionaryTable(table, false)
		if err == nil {
			// An empty export is valid; Read rejects empty imports, so use
			// the exported comment-only file as an empty word list here.
			b, readErr := os.ReadFile(table)
			if readErr != nil {
				err = readErr
			} else {
				var decoded dictionary.Backup
				decoded, err = dictionary.Read(strings.NewReader(string(b)))
				if err != nil && !errors.Is(err, dictionary.ErrEmpty) {
					lib.Finalize()
					return err
				}
				words, err = decoded.Words, nil
			}
		}
	}
	lib.Finalize()
	if err != nil {
		return err
	}
	s, err := settings.Load(c.settings)
	if err != nil {
		return errors.New("dictionary: cannot load custom phrases and pins")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".shixin-dictionary-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if strings.EqualFold(filepath.Ext(path), ".txt") {
		err = dictionary.WriteTable(f, words)
	} else {
		err = dictionary.WriteBackup(f, dictionary.Backup{Words: words, Phrases: s.Phrases, Pins: s.Pins})
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

// Resolve parent aliases before checking destinations. A Save dialog must not
// be able to overwrite a live database/configuration through a directory link.
func checkDictionaryExportPath(c config, path string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return errors.New("dictionary: choose an existing export folder")
	}
	user, err := filepath.EvalSymlinks(c.user)
	if err != nil {
		return errors.New("dictionary: cannot resolve live data folder")
	}
	destination := filepath.Join(parent, filepath.Base(path))
	rel, err := filepath.Rel(strings.ToLower(user), strings.ToLower(destination))
	if err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("dictionary: choose a file outside Shixin's live data")
	}
	settingsParent, err := filepath.EvalSymlinks(filepath.Dir(c.settings))
	if err != nil {
		return errors.New("dictionary: cannot resolve configuration folder")
	}
	if strings.EqualFold(destination, filepath.Join(settingsParent, filepath.Base(c.settings))) {
		return errors.New("dictionary: export cannot replace settings")
	}
	if target, err := filepath.EvalSymlinks(c.settings); err == nil && strings.EqualFold(destination, target) {
		return errors.New("dictionary: export cannot replace settings")
	}
	return nil
}
