package llama

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

// Exercise aggregate returns AND arguments on both Windows ABIs, even on a
// CI runner without a GPU/model. These calls previously had no native coverage.
func TestWindowsBatchABI(t *testing.T) {
	l := testLib(t)
	b := l.batchInit(4, 0, 2)
	if b.nTokens != 0 || b.token == 0 || b.pos == 0 || b.nSeqID == 0 || b.seqID == 0 || b.logits == 0 {
		t.Fatalf("batch returned incorrectly: %+v", b)
	}
	l.batchFree(b)
	s := l.samplerChainInit(chainParams{noPerf: true})
	if s == 0 {
		t.Fatal("sampler chain creation failed")
	}
	l.samplerFree(s)
}

func TestWindowsBackendSearch(t *testing.T) {
	lib := os.Getenv("SHIXIN_LLAMA")
	if lib == "" {
		t.Skip("requires SHIXIN_LLAMA pointing to the native build")
	}
	lib, err := filepath.Abs(lib)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIXIN_LLAMA", lib)
	// A broken backend in CWD or GGML_BACKEND_PATH must never be opened.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ggml-vulkan.dll"), []byte("not a DLL"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("GGML_BACKEND_PATH", filepath.Join(dir, "ggml-vulkan.dll"))
	t.Setenv("GGML_VK_VISIBLE_DEVICES", "0")
	if err := windowsCRTEnvironment("GGML_VK_VISIBLE_DEVICES", "0"); err != nil {
		t.Fatal(err)
	}
	l := testLib(t)
	if _, set := os.LookupEnv("GGML_VK_VISIBLE_DEVICES"); set {
		t.Fatal("software Vulkan device override was retained")
	}
	getenv := windows.NewLazySystemDLL("ucrtbase.dll").NewProc("getenv")
	if err := getenv.Find(); err != nil {
		t.Fatal(err)
	}
	var get func(string) uintptr
	purego.RegisterFunc(&get, getenv.Addr())
	if get("GGML_VK_VISIBLE_DEVICES") != 0 {
		t.Fatal("CRT retained the software-device override")
	}
	if os.Getenv("SHIXIN_TEST_NO_ACCELERATOR") == "1" {
		if _, ok := l.Accelerator(); ok {
			t.Fatal("CPU-only fixture exposed an accelerator")
		}
	}
}

// A vocab-only load validates the by-value model-parameter ABI without ever
// running the model on the CPU. Use the same pinned GGUF as the GPU tests.
func TestWindowsModelABI(t *testing.T) {
	l := testLib(t)
	path := os.Getenv("SHIXIN_MODEL")
	if path == "" {
		path, _ = filepath.Abs("../../build/models/ernie-4.5-0.3b-base-q8_0.gguf")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("model missing")
	}
	p := l.modelDefaultParams()
	p.vocabOnly = true
	m := l.modelLoadFromFile(path, p)
	if m == 0 {
		t.Fatal("vocab-only load failed")
	}
	defer l.modelFree(m)
	v := l.modelGetVocab(m)
	if got := l.vocabNTokens(v); got < 100000 {
		t.Fatalf("vocabulary size %d", got)
	}
}
