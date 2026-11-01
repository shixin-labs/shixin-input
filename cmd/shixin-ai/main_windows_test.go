package main

import (
	"os"
	"path/filepath"
	"testing"

	"shixin/internal/rerank"
)

func TestWindowsNoAccelerator(t *testing.T) {
	lib := os.Getenv("SHIXIN_LLAMA")
	if lib == "" || os.Getenv("SHIXIN_TEST_NO_ACCELERATOR") != "1" {
		t.Skip("requires the -NoVulkan test build")
	}
	t.Setenv("GGML_VK_VISIBLE_DEVICES", "0")
	dir := t.TempDir()
	model := filepath.Join(dir, "never-load.gguf")
	if err := os.WriteFile(model, []byte("not a model"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := run(lib, model, filepath.Join(dir, "ai.sock"), t.Logf); got != rerank.ExitNoAccelerator {
		t.Fatalf("exit %d, want no accelerator (%d); must not attempt CPU inference", got, rerank.ExitNoAccelerator)
	}
}
