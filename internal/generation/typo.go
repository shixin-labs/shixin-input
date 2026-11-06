package generation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"shixin/internal/rime"
)

var typoArtifacts = []string{"rime_ice.table.bin", "shixin_typo.forms.tsv", "shixin_typo.initial3", "shixin_typo.mirror"}

type typoManifest struct {
	Version int               `json:"version"`
	SHA256  map[string]string `json:"sha256"`
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func prepareTypoResources(lib *rime.Lib, dir, prebuilt string) error {
	stage := filepath.Join(dir, "build")
	table := filepath.Join(stage, "rime_ice.table.bin")
	if _, err := os.Stat(table); os.IsNotExist(err) {
		source, err := os.Open(filepath.Join(prebuilt, "rime_ice.table.bin"))
		if err != nil {
			return err
		}
		defer source.Close()
		output, err := os.OpenFile(table, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(output, source)
		closeErr := output.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else if err != nil {
		return err
	}
	model, err := os.ReadFile(filepath.Join(dir, "input", "shixin_typo.forms.tsv"))
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(stage, "shixin_typo.forms.tsv"), model, 0600); err != nil {
		return err
	}
	if err = lib.BuildTypoResources(table, filepath.Join(stage, "shixin_typo.initial3"), filepath.Join(stage, "shixin_typo.mirror")); err != nil {
		return err
	}
	manifest := typoManifest{Version: 1, SHA256: map[string]string{}}
	for _, name := range typoArtifacts {
		h, err := fileSHA256(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		manifest.SHA256[name] = h
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stage, "shixin_typo.manifest.json"), append(data, '\n'), 0600)
}

func validateTypoResources(dir string) error {
	stage := filepath.Join(dir, "build")
	data, err := os.ReadFile(filepath.Join(stage, "shixin_typo.manifest.json"))
	if err != nil {
		return fmt.Errorf("generation: typo manifest: %w", err)
	}
	var manifest typoManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Version != 1 || len(manifest.SHA256) != len(typoArtifacts) {
		return fmt.Errorf("generation: invalid typo resource manifest")
	}
	for _, name := range typoArtifacts {
		h, err := fileSHA256(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		if manifest.SHA256[name] != h {
			return fmt.Errorf("generation: typo resource hash mismatch: %s", name)
		}
	}
	return nil
}
