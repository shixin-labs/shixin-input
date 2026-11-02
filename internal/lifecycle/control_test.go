package lifecycle

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shixin/internal/ipc"
)

// serveOnce answers one control request with reply and returns the request.
func serveOnce(t *testing.T, socket, reply string) <-chan Request {
	t.Helper()
	l, err := ipc.Listen(socket + ".control")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	got := make(chan Request, 1)
	go func() {
		c, err := ipc.Accept(l)
		if err != nil {
			close(got)
			return
		}
		defer c.Close()
		var r Request
		json.NewDecoder(io.LimitReader(c, 4096)).Decode(&r)
		got <- r
		io.WriteString(c, reply)
	}()
	return got
}

func socketPath(t *testing.T) string {
	dir, err := ipc.TempDir("sx-lc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "engine.sock")
}

func TestDictionaryRequest(t *testing.T) {
	socket := socketPath(t)
	got := serveOnce(t, socket, `{"version":1}`+"\n")
	if err := Dictionary(socket, "export", "/tmp/out.json"); err != nil {
		t.Fatal(err)
	}
	if r := <-got; r != (Request{Version: Version, Op: OpDictionary, Action: "export", Path: "/tmp/out.json"}) {
		t.Fatalf("request %+v", r)
	}
}

func TestDictionaryReplies(t *testing.T) {
	for reply, want := range map[string]string{
		`{"version":1,"error":"dictionary: busy"}`: "dictionary: busy",
		`{"version":2}`: "unsupported version",
		`not json`:      "reply failed",
		``:              "reply failed",
	} {
		socket := socketPath(t)
		serveOnce(t, socket, reply)
		err := Dictionary(socket, "clear", "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("reply %q: %v", reply, err)
		}
	}
}

func TestDictionaryRejectsBeforeConnecting(t *testing.T) {
	if err := Dictionary(socketPath(t), "delete", ""); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatal(err)
	}
	if err := Dictionary(socketPath(t), "clear", ""); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatal(err)
	}
}
