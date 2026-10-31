package main

import (
	"path/filepath"
	"testing"

	"shixin/internal/settings"
)

// TestLiveApps: the per-app rules come from the live settings file, not
// the generation's snapshot, which keeps whatever they were when it was
// built (auto spacing turned on afterwards came back off on every restart).
func TestLiveApps(t *testing.T) {
	snapshot := settings.Default()
	snapshot.AutoSpace = false
	live := settings.Default()
	live.AutoSpace = true
	live.NoAutoSpace = []string{"com.example.editor"}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := settings.Save(path, live); err != nil {
		t.Fatal(err)
	}
	if a := liveApps(path, &snapshot); !a.AutoSpace || !a.NoAutoSpace["com.example.editor"] {
		t.Errorf("live rules not used: %+v", a)
	}
	if a := liveApps(filepath.Join(t.TempDir(), "missing.json"), &snapshot); a.AutoSpace {
		t.Error("an unreadable settings file didn't fall back to the snapshot")
	}
}
