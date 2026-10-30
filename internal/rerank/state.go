package rerank

import (
	"os"
	"path/filepath"
	"strings"
)

// shixin-ai's process state, which the engine's coordinator writes for the
// settings app (DESIGN §7.2). Whether the engine is connected comes with
// the engine's stats.
const (
	StateRunning       = "running"        // started
	StateNoModel       = "no-model"       // waiting for the model download
	StateNoAccelerator = "no-accelerator" // no GPU: 智能排序 is unavailable
)

// shixin-ai's exit statuses that a restart won't fix.
const (
	ExitNoAccelerator = 3 // no GPU: the model never runs on the CPU
	ExitNoModel       = 4 // the model file is missing
)

func statePath(dir string) string { return filepath.Join(dir, "ai.state") }

// WriteState records state in Shixin's runtime dir; "" removes it.
func WriteState(dir, state string) {
	if state == "" {
		os.Remove(statePath(dir))
		return
	}
	tmp := statePath(dir) + ".tmp"
	if os.WriteFile(tmp, []byte(state+"\n"), 0o600) == nil {
		os.Rename(tmp, statePath(dir))
	}
}

// ReadState returns the state WriteState recorded, or "".
func ReadState(dir string) string {
	b, err := os.ReadFile(statePath(dir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
