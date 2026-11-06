// Command prepare-mirror prepares private, verified files for the native spike.
// It is not a production generation validator and never opens a Rime session.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type manifest struct {
	Version                int                 `json:"version"`
	Builder                string              `json:"builder"`
	BuilderSHA256          string              `json:"builder_sha256"`
	CanonicalBuilderSHA256 string              `json:"canonical_builder_sha256,omitempty"`
	Files                  map[string]artifact `json:"files"`
}

var names = map[string]string{"table": "rime_ice.table.bin", "prism": "rime_ice.prism.bin", "initial": "rime_ice.table.bin.initial3", "mirror": "rime_ice.table.bin.mirror3", "canonical": "rime_ice.canonical.table.bin"}
var roles = []string{"table", "prism", "initial", "mirror"}

func (m manifest) artifactRoles() []string {
	if m.Version == 2 {
		return []string{"table", "prism", "initial", "mirror", "canonical"}
	}
	return roles
}

func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size
}
func readManifest(path string) (manifest, error) {
	var m manifest
	f, e := os.Open(path)
	if e != nil {
		return m, e
	}
	defer f.Close()
	stat, e := f.Stat()
	if e != nil {
		return m, e
	}
	if !stat.Mode().IsRegular() || stat.Size() > 64<<10 {
		return m, errors.New("manifest must be a regular file of at most 64 KiB")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 64<<10))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&m); e != nil {
		return m, e
	}
	var extra any
	if e = decoder.Decode(&extra); e != io.EOF {
		return m, errors.New("trailing manifest data")
	}
	validVersion := m.Version == 1 && m.Builder == "shixin-typo-mirror-v1" && m.CanonicalBuilderSHA256 == "" ||
		m.Version == 2 && m.Builder == "shixin-typo-canonical-v2" && validDigest(m.CanonicalBuilderSHA256)
	if !validVersion || !validDigest(m.BuilderSHA256) || len(m.Files) != len(m.artifactRoles()) {
		return m, errors.New("invalid manifest identity or roles")
	}
	var total int64
	for _, role := range m.artifactRoles() {
		a, ok := m.Files[role]
		if !ok || !filepath.IsAbs(a.Path) || !validDigest(a.SHA256) || a.Bytes <= 0 || a.Bytes > 128<<20 {
			return m, fmt.Errorf("invalid %s artifact", role)
		}
		total += a.Bytes
	}
	if total > 160<<20 {
		return m, errors.New("artifact total exceeds 160 MiB")
	}
	return m, nil
}

// prepare verifies bytes while copying into a newly created private directory.
// The engine must use these copies, never the source paths checked beforehand.
func prepare(manifestPath, destination string) (err error) {
	m, err := readManifest(manifestPath)
	if err != nil {
		return err
	}
	if destination == "" {
		return errors.New("destination is required")
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = os.Chmod(destination, 0700)
			_ = os.RemoveAll(destination)
		}
	}()
	buffer := make([]byte, 64<<10)
	for _, role := range m.artifactRoles() {
		a := m.Files[role]
		dest := filepath.Join(destination, names[role])
		if err = copyArtifact(a, dest, buffer); err != nil {
			return fmt.Errorf("%s: %w", role, err)
		}
		a.Path = dest
		m.Files[role] = a
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(destination, "manifest.json"), append(data, '\n'), 0400); err != nil {
		return err
	}
	if err = os.Chmod(destination, 0500); err != nil {
		return err
	}
	done = true
	return nil
}
func copyArtifact(a artifact, destination string, buffer []byte) (err error) {
	source, err := os.Open(a.Path)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != a.Bytes {
		return errors.New("source type/size mismatch")
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if e := out.Close(); err == nil {
			err = e
		}
	}()
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(out, hash), io.LimitReader(source, a.Bytes+1), buffer)
	if err != nil {
		return err
	}
	if n != a.Bytes || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
		return errors.New("copied artifact digest/size mismatch")
	}
	if err = out.Sync(); err != nil {
		return err
	}
	return out.Chmod(0400)
}
func main() {
	manifestPath := flag.String("manifest", "", "offline builder's reviewed artifact manifest")
	dest := flag.String("output", "", "new private snapshot directory (must not exist)")
	flag.Parse()
	if err := prepare(*manifestPath, *dest); err != nil {
		fmt.Fprintln(os.Stderr, "prepare-mirror:", err)
		os.Exit(1)
	}
}
