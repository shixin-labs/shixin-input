package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.1.10", "0.1.9", 1, true},
		{"0.1.9", "0.1.10", -1, true},
		{"v1.0.0", "1.0", 0, true},
		{"1.2", "1.2.1", -1, true},
		{"0.1.1", "0.1.1-dev", 0, true},
		{"1.0.0+5", "1.0.0", 0, true},
		{"dev", "1.0.0", 0, false},
		{"1..0", "1.0", 0, false},
		{"1.x", "1.0", 0, false},
		{"", "1.0", 0, false},
	} {
		got, ok := Compare(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestNewerAndDevelopment(t *testing.T) {
	for _, c := range []struct {
		version, current string
		newer, dev       bool
	}{
		{"0.1.2", "0.1.1", true, false},
		{"0.1.1", "0.1.1", false, false},
		{"0.1.1", "0.1.2", false, false},
		{"0.1.1", "0.1.1-dev", false, true},
		{"0.1.2", "0.1.1-dev", true, true},
		{"0.1.2", "dev", true, true},
		{"junk", "dev", false, true},
	} {
		if got := Newer(c.version, c.current); got != c.newer {
			t.Errorf("Newer(%q, %q) = %v", c.version, c.current, got)
		}
		if got := IsDevelopment(c.current); got != c.dev {
			t.Errorf("IsDevelopment(%q) = %v", c.current, got)
		}
		r := &Release{Version: c.version, Newer: c.newer}
		if got := CanInstall(r, c.current); got != (c.newer && !c.dev) {
			t.Errorf("CanInstall(%q, %q) = %v", c.version, c.current, got)
		}
	}
}

const goodDigest = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func releaseJSON(tag, digest, extra string) string {
	d := ""
	if digest != "" {
		d = fmt.Sprintf(`,"digest":%q`, digest)
	}
	return fmt.Sprintf(`{"tag_name":%q,"body":"Fixes\r\n- one","draft":false,"prerelease":false,"assets":[
		{"name":"Shixin.zip","browser_download_url":"https://x/zip","size":5},
		{"name":"Shixin.dmg","browser_download_url":"https://x/Shixin.dmg","size":1234%s}%s]}`, tag, d, extra)
}

func TestParseRelease(t *testing.T) {
	r, err := ParseRelease([]byte(releaseJSON("v0.1.2", goodDigest, "")))
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.1.2" || r.URL != "https://x/Shixin.dmg" || r.Size != 1234 || r.SHA256 != goodDigest[7:] || r.Notes != "Fixes\n- one" {
		t.Errorf("%+v", r)
	}
	r, err = ParseRelease([]byte(releaseJSON("0.1.3", "", "")))
	if err != nil || r.SHA256 != "" || r.Version != "0.1.3" {
		t.Errorf("no digest: %+v, %v", r, err)
	}
	for name, body := range map[string]string{
		"no asset":   `{"tag_name":"v1.0.0","assets":[{"name":"Shixin.zip","size":3,"browser_download_url":"https://x"}]}`,
		"draft":      `{"tag_name":"v1.0.0","draft":true,"assets":[]}`,
		"prerelease": `{"tag_name":"v1.0.0","prerelease":true,"assets":[]}`,
		"bad tag":    releaseJSON("../../etc", "", ""),
		"junk tag":   releaseJSON("nightly", "", ""),
		"md5":        releaseJSON("v1.0.0", "md5:abcd", ""),
		"short":      releaseJSON("v1.0.0", "sha256:abcd", ""),
		"zero size":  `{"tag_name":"v1.0.0","assets":[{"name":"Shixin.dmg","size":0,"browser_download_url":"https://x"}]}`,
		"not json":   `<html>`,
	} {
		if _, err := ParseRelease([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	long := strings.Repeat("字", MaxNotes+50)
	r, err = ParseRelease([]byte(fmt.Sprintf(`{"tag_name":"v1.0.0","body":%q,"assets":[{"name":"Shixin.dmg","size":1,"browser_download_url":"https://x"}]}`, long)))
	if err != nil || len([]rune(r.Notes)) != MaxNotes+1 {
		t.Errorf("notes: %d runes, %v", len([]rune(r.Notes)), err)
	}
}

func useServer(t *testing.T, h http.Handler) *httptest.Server {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old, oldBase := HTTP, APIBase
	HTTP, APIBase = srv.Client(), srv.URL
	t.Cleanup(func() { HTTP, APIBase = old, oldBase })
	return srv
}

func TestCheck(t *testing.T) {
	var gotAccept, gotUA, gotPath string
	useServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept, gotUA, gotPath = r.Header.Get("Accept"), r.Header.Get("User-Agent"), r.URL.Path
		w.Write([]byte(releaseJSON("v0.2.0", goodDigest, "")))
	}))
	r, err := Check(context.Background(), "0.1.9")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Newer || r.Version != "0.2.0" {
		t.Errorf("%+v", r)
	}
	if gotAccept != "application/vnd.github+json" || gotUA != "Shixin/0.1.9" || gotPath != "/repos/"+Repo+"/releases/latest" {
		t.Errorf("request: %q %q %q", gotAccept, gotUA, gotPath)
	}
	r, err = Check(context.Background(), "0.2.0")
	if err != nil || r.Newer {
		t.Errorf("same version: %+v, %v", r, err)
	}
}

func TestCheckErrors(t *testing.T) {
	useServer(t, http.NotFoundHandler())
	if _, err := Check(context.Background(), "0.1.0"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: %v", err)
	}
}

func release(srv *httptest.Server, body string) *Release {
	h := sha256.Sum256([]byte(body))
	return &Release{Version: "0.2.0", URL: srv.URL + "/Shixin.dmg", Size: int64(len(body)), SHA256: hex.EncodeToString(h[:])}
}

func serveBody(t *testing.T, body string) *httptest.Server {
	return useServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Shixin/0.1.0" {
			http.Error(w, "agent", http.StatusForbidden)
			return
		}
		w.Write([]byte(body))
	}))
}

func TestDownload(t *testing.T) {
	body := strings.Repeat("dmg!", 5000)
	srv := serveBody(t, body)
	root := filepath.Join(t.TempDir(), "Updates")
	// An older download is cleaned up.
	os.MkdirAll(filepath.Join(root, "0.1.5"), 0o755)
	os.WriteFile(filepath.Join(root, "0.1.5", AssetName), []byte("old"), 0o644)

	var last, total int64
	path, err := Download(context.Background(), release(srv, body), root, "0.1.0", func(d, tot int64) { last, total = d, tot })
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "0.2.0", AssetName) {
		t.Errorf("path %s", path)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Error("content differs")
	}
	if last != int64(len(body)) || total != last {
		t.Errorf("progress %d of %d", last, total)
	}
	if _, err := os.Stat(path + ".partial"); !os.IsNotExist(err) {
		t.Error("partial file left behind")
	}
	if _, err := os.Stat(filepath.Join(root, "0.1.5")); !os.IsNotExist(err) {
		t.Error("old update kept")
	}
}

func TestDownloadWithoutDigest(t *testing.T) {
	body := "small"
	srv := serveBody(t, body)
	r := release(srv, body)
	r.SHA256 = ""
	if _, err := Download(context.Background(), r, t.TempDir(), "0.1.0", nil); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRejects(t *testing.T) {
	body := strings.Repeat("dmg!", 100)
	srv := serveBody(t, body)
	for name, mod := range map[string]func(*Release){
		"digest": func(r *Release) { r.SHA256 = strings.Repeat("0", 64) },
		"short":  func(r *Release) { r.Size++ },
		"long":   func(r *Release) { r.Size-- },
		"404":    func(r *Release) { r.URL += "x"; r.Size = 1 },
		"http":   func(r *Release) { r.URL = "http://example.com/Shixin.dmg" },
		"file":   func(r *Release) { r.URL = "file:///etc/passwd" },
		"tag":    func(r *Release) { r.Version = "../x" },
	} {
		r := release(srv, body)
		mod(r)
		root := t.TempDir()
		_, err := Download(context.Background(), r, root, "0.1.0", nil)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if name == "digest" || name == "short" || name == "long" {
			if !errors.Is(err, ErrMismatch) {
				t.Errorf("%s: %v", name, err)
			}
		}
		var left []string
		filepath.WalkDir(root, func(p string, d os.DirEntry, _ error) error {
			if !d.IsDir() {
				left = append(left, p)
			}
			return nil
		})
		if len(left) > 0 {
			t.Errorf("%s: left %v", name, left)
		}
	}
}

func TestDownloadStallAndCancel(t *testing.T) {
	body := strings.Repeat("dmg!", 100)
	release_ := make(chan struct{})
	srv := useServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body[:50]))
		w.(http.Flusher).Flush()
		select {
		case <-release_:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release_) })
	old := stallTimeout
	stallTimeout = 200 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })

	root := t.TempDir()
	if _, err := Download(context.Background(), release(srv, body), root, "0.1.0", nil); err == nil {
		t.Error("stalled download succeeded")
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "0.2.0")); len(entries) > 0 {
		t.Errorf("left %v", entries)
	}

	stallTimeout = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	if _, err := Download(ctx, release(srv, body), root, "0.1.0", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "0.2.0")); len(entries) > 0 {
		t.Errorf("left %v", entries)
	}
}
