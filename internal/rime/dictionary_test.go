package rime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"shixin/internal/dictionary"
)

func TestDictionaryTableRoundTrip(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lib := loadForTest(t)
	root := t.TempDir()
	lib.Setup(Traits{SharedDataDir: root, UserDataDir: root, MinLogLevel: 3, Modules: []string{"default", "levers"}})
	lib.InitializeDeployer()
	defer lib.Finalize()
	source, output := filepath.Join(root, "input.txt"), filepath.Join(root, "output.txt")
	if err := os.WriteFile(source, []byte("幸会\txing hui\t37\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := lib.DictionaryTable(source, true); err != nil || n != 1 {
		t.Fatalf("import count=%d error=%v", n, err)
	}
	if _, err := lib.DictionaryTable(output, false); err != nil {
		t.Fatal(err)
	}
	b, err := dictionary.ReadFile(output)
	if err != nil || len(b.Words) != 1 || b.Words[0] != (dictionary.Entry{Text: "幸会", Code: "xing hui", Count: 37}) {
		t.Fatalf("export did not preserve word and count: %v", err)
	}
	// Repeating the same import merges entries without doubling frequency.
	if _, err := lib.DictionaryTable(source, true); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.DictionaryTable(output, false); err != nil {
		t.Fatal(err)
	}
	b, err = dictionary.ReadFile(output)
	if err != nil || len(b.Words) != 1 || b.Words[0].Count != 37 {
		t.Fatal("duplicate import changed the learned count", err)
	}
}
