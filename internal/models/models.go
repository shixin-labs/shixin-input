// Package models is 智能排序's model file (DESIGN §7.2): where it lives,
// how it is downloaded, and how its integrity is checked. The settings app
// downloads it on request; the engine's coordinator waits for it before
// starting shixin-ai. Nothing else touches the network.
package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"shixin/internal/netclient"
	"shixin/internal/settings"
)

// Spec is a downloadable model.
type Spec struct {
	Name string // as shown
	File string // in Dir
	// URLs are tried in order. Every download is checked against Size and
	// SHA256, so a mirror can't substitute another file.
	URLs   []string
	Size   int64
	SHA256 string
}

// ERNIE is the 8-bit GGUF of baidu/ERNIE-4.5-0.3B-Base-PT (Apache-2.0),
// converted with the pinned llama.cpp's convert_hf_to_gguf.py --outtype
// q8_0. Third-party conversions score markedly worse (DESIGN §7.2).
// huggingface.co is unreachable from mainland China without a proxy;
// hf-mirror.com serves the same repository there.
var ERNIE = Spec{
	Name: "ERNIE 4.5 0.3B",
	File: "ernie-4.5-0.3b-base-q8_0.gguf",
	URLs: []string{
		"https://huggingface.co/shixin-labs/ERNIE-4.5-0.3B-Base-GGUF/resolve/main/ernie-4.5-0.3b-base-q8_0.gguf",
		"https://hf-mirror.com/shixin-labs/ERNIE-4.5-0.3B-Base-GGUF/resolve/main/ernie-4.5-0.3b-base-q8_0.gguf",
	},
	Size:   385789184,
	SHA256: "c6c2d5bb1eac4b7a7cffc60bfb8e794af26c3d5b6700d8d9b09296b13d1669e8",
}

// Dir is the models directory, next to settings.json.
func Dir() (string, error) {
	p, err := settings.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "models"), nil
}

// Path is where m lives in dir (Dir in the product).
func (m *Spec) Path(dir string) string { return filepath.Join(dir, m.File) }

// Present reports whether m is downloaded into dir. Only verified
// downloads are renamed into place, so the size is checked, not the hash.
func (m *Spec) Present(dir string) bool {
	fi, err := os.Stat(m.Path(dir))
	return err == nil && fi.Size() == m.Size
}

// ErrMismatch means the download isn't the pinned file.
var ErrMismatch = errors.New("models: the download doesn't match its checksum")

// stallTimeout ends a download that receives nothing for this long, so the
// next URL gets its turn.
var stallTimeout = 30 * time.Second

// Download fetches m into dir from the first of its URLs that works,
// reporting progress, and keeps it only if its size and SHA-256 match. The
// file is data: llama.cpp loads it, and nothing runs it.
func (m *Spec) Download(ctx context.Context, dir string, progress func(done, total int64)) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("models: %w", err)
	}
	var errs []error
	for _, u := range m.URLs {
		err := m.fetch(ctx, u, dir, progress)
		if err == nil || ctx.Err() != nil {
			return err
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return errors.New("models: no download URL")
	}
	return errors.Join(errs...)
}

func (m *Spec) fetch(ctx context.Context, u, dir string, progress func(done, total int64)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	host := req.URL.Host
	resp, err := netclient.Client.Do(req)
	if err != nil {
		return fmt.Errorf("models: %s: %w", host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models: %s: %s", host, resp.Status)
	}
	p := m.Path(dir)
	part := p + ".partial"
	f, err := os.Create(part)
	if err != nil {
		return fmt.Errorf("models: %w", err)
	}
	defer os.Remove(part) // a no-op after the rename
	stall := time.AfterFunc(stallTimeout, cancel)
	defer stall.Stop()
	h := sha256.New()
	pw := &progressWriter{total: m.Size, report: progress, stall: stall}
	n, err := io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(resp.Body, m.Size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("models: %s: %w", host, err)
	}
	if n != m.Size || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return fmt.Errorf("%w (%s)", ErrMismatch, host)
	}
	if err := os.Rename(part, p); err != nil {
		return fmt.Errorf("models: %w", err)
	}
	return nil
}

// Remove deletes m from dir.
func (m *Spec) Remove(dir string) error {
	if err := os.Remove(m.Path(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("models: %w", err)
	}
	return nil
}

type progressWriter struct {
	done, total int64
	report      func(done, total int64)
	stall       *time.Timer
}

func (w *progressWriter) Write(b []byte) (int, error) {
	w.done += int64(len(b))
	w.stall.Reset(stallTimeout)
	if w.report != nil {
		w.report(w.done, w.total)
	}
	return len(b), nil
}
