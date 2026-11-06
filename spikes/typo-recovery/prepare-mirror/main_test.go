package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, manifest) {
	t.Helper()
	dir := t.TempDir()
	m := manifest{Version: 1, Builder: "shixin-typo-mirror-v1", BuilderSHA256: strings.Repeat("a", 64), Files: map[string]artifact{}}
	for _, role := range roles {
		data := []byte("verified-" + role)
		p := filepath.Join(dir, role)
		if e := os.WriteFile(p, data, 0600); e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(data)
		m.Files[role] = artifact{p, hex.EncodeToString(sum[:]), int64(len(data))}
	}
	p := filepath.Join(dir, "manifest.json")
	writeManifest(t, p, m)
	return p, m
}
func writeManifest(t *testing.T, p string, m manifest) {
	t.Helper()
	b, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestPrivateSnapshotUsesVerifiedCopies(t *testing.T) {
	path, m := fixture(t)
	dest := filepath.Join(t.TempDir(), "snapshot")
	t.Cleanup(func() { _ = os.Chmod(dest, 0700) })
	if e := prepare(path, dest); e != nil {
		t.Fatal(e)
	}
	saved, e := readManifest(filepath.Join(dest, "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, role := range roles {
		a := m.Files[role]
		if e := os.WriteFile(a.Path, []byte("changed after validation"), 0600); e != nil {
			t.Fatal(e)
		}
		p := saved.Files[role].Path
		if p != filepath.Join(dest, names[role]) {
			t.Fatalf("source path leaked: %q", p)
		}
		data, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != a.SHA256 {
			t.Fatal("snapshot changed with source")
		}
		info, e := os.Stat(p)
		if e != nil {
			t.Fatal(e)
		}
		if info.Mode().Perm()&0222 != 0 {
			t.Fatal("snapshot file writable")
		}
	}
}
func TestMismatchRemovesOnlyNewSnapshot(t *testing.T) {
	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			path, m := fixture(t)
			a := m.Files[role]
			data := []byte("verified-" + role)
			data[0] ^= 1
			if e := os.WriteFile(a.Path, data, 0600); e != nil {
				t.Fatal(e)
			}
			dest := filepath.Join(t.TempDir(), "snapshot")
			if e := prepare(path, dest); e == nil {
				t.Fatal("accepted changed artifact")
			}
			if _, e := os.Stat(dest); !os.IsNotExist(e) {
				t.Fatal("partial snapshot remains")
			}
			if _, e := os.Stat(a.Path); e != nil {
				t.Fatal("source removed")
			}
		})
	}
	path, _ := fixture(t)
	dest := t.TempDir()
	sentinel := filepath.Join(dest, "keep")
	if e := os.WriteFile(sentinel, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := prepare(path, dest); e == nil {
		t.Fatal("accepted existing destination")
	}
	if b, e := os.ReadFile(sentinel); e != nil || string(b) != "keep" {
		t.Fatal("existing destination changed")
	}
}
func TestManifestRejectsInvalidContract(t *testing.T) {
	cases := map[string]func(*manifest){
		"version":         func(m *manifest) { m.Version = 2 },
		"builder":         func(m *manifest) { m.Builder = "unknown" },
		"builder digest":  func(m *manifest) { m.BuilderSHA256 = "bad" },
		"missing":         func(m *manifest) { delete(m.Files, "mirror") },
		"unknown role":    func(m *manifest) { m.Files["../escape"] = m.Files["mirror"]; delete(m.Files, "mirror") },
		"relative source": func(m *manifest) { a := m.Files["table"]; a.Path = "../table"; m.Files["table"] = a },
		"bad digest":      func(m *manifest) { a := m.Files["prism"]; a.SHA256 = "bad"; m.Files["prism"] = a },
		"negative size":   func(m *manifest) { a := m.Files["initial"]; a.Bytes = -1; m.Files["initial"] = a },
		"oversize":        func(m *manifest) { a := m.Files["table"]; a.Bytes = 129 << 20; m.Files["table"] = a },
		"total size": func(m *manifest) {
			for k, a := range m.Files {
				a.Bytes = 100 << 20
				m.Files[k] = a
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p, m := fixture(t)
			change(&m)
			writeManifest(t, p, m)
			dest := filepath.Join(t.TempDir(), "snapshot")
			if e := prepare(p, dest); e == nil {
				t.Fatal("accepted invalid manifest")
			}
			if _, e := os.Stat(dest); !os.IsNotExist(e) {
				t.Fatal("created snapshot for invalid manifest")
			}
		})
	}
	p, _ := fixture(t)
	f, e := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.WriteString("{}")
	_ = f.Close()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := readManifest(p); e == nil {
		t.Fatal("accepted trailing JSON")
	}
}

func TestCanonicalSnapshotContract(t *testing.T) {
	for _, scenario := range []string{"valid", "missing canonical", "changed bytes", "missing builder", "v1 with canonical"} {
		t.Run(scenario, func(t *testing.T) {
			p, m := fixture(t)
			m.Version = 2
			m.Builder = "shixin-typo-canonical-v2"
			m.CanonicalBuilderSHA256 = strings.Repeat("b", 64)
			data := []byte("canonical table bytes")
			canonical := filepath.Join(filepath.Dir(p), "canonical")
			if err := os.WriteFile(canonical, data, 0600); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			m.Files["canonical"] = artifact{canonical, hex.EncodeToString(digest[:]), int64(len(data))}
			switch scenario {
			case "missing canonical":
				delete(m.Files, "canonical")
			case "changed bytes":
				data[0] ^= 1
				if err := os.WriteFile(canonical, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing builder":
				m.CanonicalBuilderSHA256 = ""
			case "v1 with canonical":
				m.Version = 1
				m.Builder = "shixin-typo-mirror-v1"
				m.CanonicalBuilderSHA256 = ""
			}
			writeManifest(t, p, m)
			dest := filepath.Join(t.TempDir(), "snapshot")
			t.Cleanup(func() { _ = os.Chmod(dest, 0700) })
			err := prepare(p, dest)
			if scenario != "valid" {
				if err == nil {
					t.Fatal("accepted invalid snapshot")
				}
				if _, err := os.Stat(dest); !os.IsNotExist(err) {
					t.Fatal("partial snapshot remains")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			saved, err := readManifest(filepath.Join(dest, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.Files) != 5 || saved.Files["canonical"].Path != filepath.Join(dest, names["canonical"]) {
				t.Fatal("canonical artifact not bound to private copy")
			}
			if err := os.WriteFile(canonical, []byte("changed source"), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(saved.Files["canonical"].Path)
			if err != nil || string(got) != "canonical table bytes" {
				t.Fatal("private canonical copy changed")
			}
		})
	}
}
