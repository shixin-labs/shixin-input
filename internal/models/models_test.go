package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, body string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func spec(url, body string) Spec {
	h := sha256.Sum256([]byte(body))
	return Spec{Name: "test", File: "m.gguf", URLs: []string{url}, Size: int64(len(body)), SHA256: hex.EncodeToString(h[:])}
}

// TestDownloadFallback falls back to the next URL when a host fails,
// refuses connections or stalls, and reports every failure if all do.
func TestDownloadFallback(t *testing.T) {
	body := strings.Repeat("gguf", 1000)
	good := serve(t, body)
	refused := httptest.NewServer(http.NotFoundHandler())
	refused.Close()
	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body[:100]))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); stalled.Close() })
	old := stallTimeout
	stallTimeout = 200 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })

	for _, first := range []string{good.URL + "/nope", refused.URL + "/model", stalled.URL + "/model"} {
		dir := t.TempDir()
		m := spec(first, body)
		m.URLs = append(m.URLs, good.URL+"/model")
		if err := m.Download(context.Background(), dir, nil); err != nil || !m.Present(dir) {
			t.Errorf("first %s: %v, present %v", first, err, m.Present(dir))
		}
	}
	m := spec(good.URL+"/nope", body)
	m.URLs = append(m.URLs, refused.URL+"/model")
	err := m.Download(context.Background(), t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "refused") {
		t.Errorf("all failing: %v, want both failures", err)
	}
}

func TestParseScutilProxy(t *testing.T) {
	const both = `<dictionary> {
  ExceptionsList : <array> {
    0 : *.local
  }
  HTTPEnable : 1
  HTTPPort : 7890
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7890
  HTTPSProxy : 127.0.0.1
  ProxyAutoConfigEnable : 0
  SOCKSEnable : 1
  SOCKSPort : 7891
  SOCKSProxy : 127.0.0.1
}`
	for _, c := range []struct{ name, out, want string }{
		{"https", both, "http://127.0.0.1:7890"},
		{"socks", strings.Replace(both, "HTTPSEnable : 1", "HTTPSEnable : 0", 1), "socks5://127.0.0.1:7891"},
		{"none", "<dictionary> {\n  HTTPEnable : 0\n  ProxyAutoConfigEnable : 1\n}", ""},
	} {
		got := ""
		if u := parseScutilProxy(c.out); u != nil {
			got = u.String()
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDownload(t *testing.T) {
	body := strings.Repeat("gguf", 10000)
	srv := serve(t, body)
	dir := t.TempDir()
	m := spec(srv.URL+"/model", body)
	if m.Present(dir) {
		t.Fatal("present before the download")
	}
	var last int64
	if err := m.Download(context.Background(), dir, func(done, total int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if !m.Present(dir) || last != m.Size {
		t.Fatalf("present %v, progress %d of %d", m.Present(dir), last, m.Size)
	}
	if _, err := os.Stat(m.Path(dir) + ".partial"); !os.IsNotExist(err) {
		t.Error("partial file left behind")
	}
	if err := m.Remove(dir); err != nil || m.Present(dir) {
		t.Errorf("remove: %v, present %v", err, m.Present(dir))
	}
}

func TestDownloadRejects(t *testing.T) {
	body := strings.Repeat("gguf", 100)
	srv := serve(t, body)
	for _, c := range []struct {
		name string
		m    Spec
		want error
	}{
		{"tampered", func() Spec { m := spec(srv.URL+"/model", body); m.SHA256 = strings.Repeat("0", 64); return m }(), ErrMismatch},
		{"short", func() Spec { m := spec(srv.URL+"/model", body); m.Size++; return m }(), ErrMismatch},
		{"long", func() Spec { m := spec(srv.URL+"/model", body); m.Size--; return m }(), ErrMismatch},
		{"missing", spec(srv.URL+"/nope", body), nil},
	} {
		dir := t.TempDir()
		err := c.m.Download(context.Background(), dir, nil)
		if err == nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("%s: left %v", c.name, entries)
		}
	}
}

func TestDownloadCancel(t *testing.T) {
	srv := serve(t, strings.Repeat("x", 1<<20))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	m := spec(srv.URL+"/model", strings.Repeat("x", 1<<20))
	if err := m.Download(ctx, dir, nil); err == nil || m.Present(dir) {
		t.Errorf("cancelled download: %v, present %v", err, m.Present(dir))
	}
}

// TestPublished downloads the real model from its pinned URL and checks it
// against its size and SHA-256. Network: run with SHIXIN_TEST_NETWORK=1.
func TestPublished(t *testing.T) {
	if os.Getenv("SHIXIN_TEST_NETWORK") != "1" {
		t.Skip("downloads 385 MB; set SHIXIN_TEST_NETWORK=1")
	}
	dir := t.TempDir()
	if err := ERNIE.Download(context.Background(), dir, nil); err != nil {
		t.Fatal(err)
	}
	if !ERNIE.Present(dir) {
		t.Fatal("not present after the download")
	}
}
