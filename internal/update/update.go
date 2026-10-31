// Package update finds and downloads Shixin's newer releases (DESIGN §5.5,
// "In-app update"). Nothing here runs by itself: the settings app calls it
// when the user asks, and it only talks to GitHub. A download is untrusted
// data: this package checks its size and checksum, and the front verifies
// its signatures before installing anything (cmd/shixin).
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"shixin/internal/netclient"
)

const (
	// Repo is the GitHub repository whose latest release is offered.
	Repo = "shixin-labs/shixin-input"
	// AssetName is the disk image attached to every release.
	AssetName = "Shixin.dmg"
	// MaxNotes bounds the release notes kept, in characters.
	MaxNotes = 1500
)

var (
	// APIBase is where releases are looked up; tests point it elsewhere.
	APIBase = "https://api.github.com"
	// HTTP is the client used for both the lookup and the download.
	HTTP = netclient.Client
	// stallTimeout ends a download that receives nothing for this long.
	stallTimeout = 30 * time.Second
)

// ErrMismatch means the download isn't the file the release describes.
var ErrMismatch = errors.New("update: the download doesn't match its published size or checksum")

// Release is the latest published release.
type Release struct {
	Version string // 0.1.2, from the tag v0.1.2
	Notes   string // plain text, trimmed
	URL     string // the disk image
	Size    int64
	SHA256  string // hex, "" if GitHub gave no digest
	Newer   bool   // strictly newer than the running version
}

type apiRelease struct {
	Tag        string `json:"tag_name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func userAgent(current string) string { return "Shixin/" + current }

// Check asks GitHub for the latest release. Newer says whether it is
// strictly newer than current; whether this build may install it is up to
// CanInstall.
func Check(ctx context.Context, current string) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent(current))
	resp, err := HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: %s: %s", req.URL.Host, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	r, err := ParseRelease(body)
	if err != nil {
		return nil, err
	}
	r.Newer = Newer(r.Version, current)
	return r, nil
}

// ParseRelease reads GitHub's release JSON.
func ParseRelease(body []byte) (*Release, error) {
	var a apiRelease
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("update: release: %w", err)
	}
	if a.Draft || a.Prerelease {
		return nil, errors.New("update: the latest release is not final")
	}
	ver := strings.TrimPrefix(a.Tag, "v")
	if !validVersion(ver) {
		return nil, fmt.Errorf("update: unusable release tag %q", a.Tag)
	}
	r := &Release{Version: ver, Notes: trimNotes(a.Body)}
	for _, as := range a.Assets {
		if as.Name != AssetName {
			continue
		}
		if as.Size <= 0 || as.URL == "" {
			return nil, fmt.Errorf("update: release %s: unusable %s entry", ver, AssetName)
		}
		r.URL, r.Size = as.URL, as.Size
		if as.Digest != "" {
			h, ok := strings.CutPrefix(as.Digest, "sha256:")
			if b, err := hex.DecodeString(h); !ok || err != nil || len(b) != sha256.Size {
				return nil, fmt.Errorf("update: release %s: unsupported digest %q", ver, as.Digest)
			}
			r.SHA256 = strings.ToLower(h)
		}
		return r, nil
	}
	return nil, fmt.Errorf("update: release %s has no %s", ver, AssetName)
}

func trimNotes(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if utf8.RuneCountInString(s) <= MaxNotes {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:MaxNotes])) + "…"
}

// validVersion accepts what a tag may hold and a directory name may be:
// dotted numbers with an optional -suffix of letters, digits and dots.
func validVersion(v string) bool {
	if _, _, ok := parseVersion(v); !ok {
		return false
	}
	for _, r := range v {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '-' || r == '+') {
			return false
		}
	}
	return true
}

// parseVersion reads 1.2.3 (a leading v is fine). pre reports a suffix
// after a hyphen (0.1.1-dev), which marks a development build.
func parseVersion(v string) (nums []int, pre bool, ok bool) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], true
	}
	if v == "" {
		return nil, false, false
	}
	for _, f := range strings.Split(v, ".") {
		if f == "" || len(f) > 9 {
			return nil, false, false
		}
		n := 0
		for _, r := range f {
			if r < '0' || r > '9' {
				return nil, false, false
			}
			n = n*10 + int(r-'0')
		}
		nums = append(nums, n)
	}
	return nums, pre, true
}

// Compare orders dotted numeric versions: -1, 0 or 1; ok is false if
// either isn't one. A pre-release suffix is ignored (0.1.1-dev is 0.1.1).
func Compare(a, b string) (c int, ok bool) {
	x, _, ok1 := parseVersion(a)
	y, _, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := 0; i < max(len(x), len(y)); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			if p < q {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// Newer reports whether version is strictly newer than current. An
// unparseable current (dev) is older than any release.
func Newer(version, current string) bool {
	if _, _, ok := parseVersion(current); !ok {
		_, _, ok := parseVersion(version)
		return ok
	}
	c, ok := Compare(version, current)
	return ok && c > 0
}

// IsDevelopment reports whether current is a development build: "dev", or
// a version with a pre-release suffix (0.1.1-dev). Such builds can check
// but don't install.
func IsDevelopment(current string) bool {
	_, pre, ok := parseVersion(current)
	return !ok || pre
}

// CanInstall reports whether this build may install r.
func CanInstall(r *Release, current string) bool {
	return r != nil && r.Newer && !IsDevelopment(current)
}

// Dir is where updates are downloaded, in the user's caches.
func Dir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "Shixin", "Updates"), nil
}

// Download fetches r's disk image into root/<version>/Shixin.dmg, after
// removing everything older under root, and returns its path. It keeps the
// file only if its size matches the release and, when GitHub gave one, its
// SHA-256. It doesn't open or run the file.
func Download(ctx context.Context, r *Release, root, current string, progress func(done, total int64)) (string, error) {
	if r == nil || !validVersion(r.Version) || r.Size <= 0 {
		return "", errors.New("update: no release to download")
	}
	if err := checkURL(r.URL); err != nil {
		return "", err
	}
	if err := os.RemoveAll(root); err != nil {
		return "", fmt.Errorf("update: %w", err)
	}
	dir := filepath.Join(root, r.Version)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("update: %w", err)
	}
	path := filepath.Join(dir, AssetName)
	if err := fetch(ctx, r, path, current, progress); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return path, nil
}

// checkURL allows https, and plain http only to this machine (tests).
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if h := u.Hostname(); u.Scheme == "http" && (h == "localhost" || net.ParseIP(h).IsLoopback()) {
		return nil
	}
	return fmt.Errorf("update: refusing to download from %s", u.Redacted())
}

func fetch(ctx context.Context, r *Release, path, current string, progress func(done, total int64)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent(current))
	host := req.URL.Host
	resp, err := HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("update: %s: %w", host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update: %s: %s", host, resp.Status)
	}
	part := path + ".partial"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	defer os.Remove(part) // a no-op after the rename
	stall := time.AfterFunc(stallTimeout, cancel)
	defer stall.Stop()
	h := sha256.New()
	pw := &progressWriter{total: r.Size, report: progress, stall: stall}
	n, err := io.Copy(io.MultiWriter(f, h, pw), io.LimitReader(resp.Body, r.Size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("update: %s: %w", host, err)
	}
	if n != r.Size || r.SHA256 != "" && hex.EncodeToString(h.Sum(nil)) != r.SHA256 {
		return fmt.Errorf("%w (%s)", ErrMismatch, host)
	}
	if err := os.Rename(part, path); err != nil {
		return fmt.Errorf("update: %w", err)
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
