package generation

import (
	"crypto/sha256"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"shixin/internal/settings"
)

func TestSeedCompiledOwnsCopies(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source", "build")
	out := filepath.Join(root, "out")
	os.MkdirAll(source, 0700)
	os.WriteFile(filepath.Join(source, "dict.table.bin"), []byte("immutable dictionary"), 0600)
	os.WriteFile(filepath.Join(source, "schema.yaml"), []byte("must not reuse configuration"), 0600)
	os.WriteFile(filepath.Join(source, "LOCK"), []byte("must not touch user data"), 0600)
	if err := seedCompiled(filepath.Dir(source), out); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 || entries[0].Name() != "dict.table.bin" {
		t.Fatalf("copied unrelated files: %v", entries)
	}
	os.WriteFile(filepath.Join(out, "dict.table.bin"), []byte("compiler replacement"), 0600)
	b, _ := os.ReadFile(filepath.Join(source, "dict.table.bin"))
	if string(b) != "immutable dictionary" {
		t.Fatal("compiler modified the serving generation")
	}
}

func TestSeedCompiledRejectsLinks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source", "build")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "private.bin")
	if err := os.WriteFile(target, []byte("outside generation"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(source, "dictionary.bin")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := seedCompiled(filepath.Dir(source), filepath.Join(root, "out")); err == nil {
		t.Fatal("copied a linked resource")
	}
}

func TestReuseGeneration(t *testing.T) {
	lib := os.Getenv("SHIXIN_LIBRIME")
	data, _ := filepath.Abs("../../build/rime-data")
	if _, err := os.Stat(filepath.Join(data, "rime_ice.schema.yaml")); testing.Short() || lib == "" || err != nil {
		t.Skip("needs SHIXIN_LIBRIME and rime-ice data")
	}
	root := t.TempDir()
	builder := filepath.Join(root, "builder")
	if runtime.GOOS == "windows" {
		builder += ".exe"
	}
	if b, err := exec.Command("go", "build", "-o", builder, "../../cmd/shixin-builder").CombinedOutput(); err != nil {
		t.Fatalf("build builder: %v %s", err, b)
	}
	build := func(name, reuse string, s settings.Settings) string {
		t.Helper()
		dir := filepath.Join(root, name)
		snapshot := dir + ".json"
		if err := settings.Save(snapshot, s); err != nil {
			t.Fatal(err)
		}
		args := []string{"-lib", lib, "-data", data, "-settings", snapshot, "-output", dir}
		if reuse != "" {
			args = append(args, "-reuse-generation", reuse)
		}
		if b, err := exec.Command(builder, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", name, err, b)
		}
		if err := Validate(dir); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	hashes := func(dir string) map[string][32]byte {
		t.Helper()
		out := map[string][32]byte{}
		files, _ := filepath.Glob(filepath.Join(dir, "build", "*.bin"))
		for _, path := range files {
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				f.Close()
				t.Fatal(err)
			}
			f.Close()
			var digest [32]byte
			copy(digest[:], h.Sum(nil))
			out[filepath.Base(path)] = digest
		}
		return out
	}
	s := settings.Default()
	base := build("base", "", s)
	want := hashes(base)
	s.PageSize = 7
	page := hashes(build("page", base, s))
	for name, digest := range want {
		if page[name] != digest {
			t.Fatalf("page size rebuilt %s", name)
		}
	}
	s.Fuzzy = []string{"z=zh"}
	fuzzy := hashes(build("fuzzy", base, s))
	if fuzzy["rime_ice.prism.bin"] == want["rime_ice.prism.bin"] {
		t.Fatal("fuzzy spelling reused a stale prism")
	}
	for name, digest := range want {
		if strings.HasSuffix(name, ".table.bin") && fuzzy[name] != digest {
			t.Fatalf("fuzzy spelling changed dictionary %s", name)
		}
	}
	for name, digest := range hashes(base) {
		if digest != want[name] {
			t.Fatalf("serving generation changed: %s", name)
		}
	}
}
