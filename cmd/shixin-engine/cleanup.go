package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// generationCleanup is a fixed startup snapshot. Never rescan in its
// background goroutine: a later settings apply may already be building.
type generationCleanup struct {
	root  *os.Root
	names []string
}

func generationName(name string) bool {
	if !strings.HasPrefix(name, "gen-") || len(name) == len("gen-") {
		return false
	}
	for _, c := range name[len("gen-"):] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// planGenerationCleanup runs under the supervisor lock, after successful
// activation and before any settings apply. Only MkdirTemp generation names
// and their sibling settings snapshots belong to us; leave other files alone.
func planGenerationCleanup(user string, keep ...string) (*generationCleanup, error) {
	dir := filepath.Join(user, "generations")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("generations is not a directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		root.Close()
		return nil, err
	}
	protected := make(map[string]bool, len(keep))
	for _, path := range keep {
		if path != "" {
			name := filepath.Base(path)
			info, err := root.Lstat(name)
			if err != nil || !info.IsDir() {
				root.Close()
				// A linked active generation could refer to another entry
				// in the deletion list. Keep everything if ownership is unclear.
				return nil, fmt.Errorf("protected generation %s is not a directory", name)
			}
			protected[name] = true
		}
	}
	p := &generationCleanup{root: root}
	// Remove snapshots first, while their sibling directories still reserve
	// their names against future MkdirTemp calls.
	for _, entry := range entries {
		name := entry.Name()
		base, snapshot := strings.CutSuffix(name, ".json")
		if !snapshot {
			base, snapshot = strings.CutSuffix(name, ".status")
		}
		if snapshot && entry.Type().IsRegular() && generationName(base) && !protected[base] {
			p.names = append(p.names, name)
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() && generationName(name) && !protected[name] {
			p.names = append(p.names, name)
		}
	}
	return p, nil
}

func (p *generationCleanup) run(stop <-chan struct{}, logf func(string, ...any)) {
	defer p.root.Close()
	for _, name := range p.names {
		select {
		case <-stop:
			return
		default:
		}
		// Root confines traversal even if a directory is replaced by a
		// symlink. RemoveAll never follows links inside an abandoned build.
		if err := p.root.RemoveAll(name); err != nil {
			logf("generation cleanup failed: %v", err)
		}
	}
}

func (s *coordinator) startGenerationCleanup() func() {
	p, err := planGenerationCleanup(s.c.user, s.active.dir, s.previous)
	if err != nil {
		s.logf("generation cleanup skipped: %v", err)
	}
	if p == nil {
		return func() {}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		p.run(stop, s.logf)
	}()
	return func() {
		close(stop)
		// Finish the current removal before releasing the supervisor lock;
		// a replacement coordinator must never race this cleanup job.
		<-done
	}
}
