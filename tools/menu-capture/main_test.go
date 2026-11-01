package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"shixin/internal/ipc"
)

func TestReadCodesRejectsMalformedOrAmbiguousInput(t *testing.T) {
	for _, input := range []string{"", `{"code":"SJ"}`, `{"code":"sj","label":"时间"}`, `{"code":"sj"} {}`, "{\"code\":\"sj\"}\n{\"code\":\"sj\"}\n"} {
		path := filepath.Join(t.TempDir(), "input")
		os.WriteFile(path, []byte(input), 0o600)
		if _, err := readCodes(path); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	path := filepath.Join(t.TempDir(), "input")
	os.WriteFile(path, []byte("{\"code\":\"sj\"}\n{\"code\":\"tw\"}\n"), 0o600)
	if got, err := readCodes(path); err != nil || !reflect.DeepEqual(got, []string{"sj", "tw"}) {
		t.Fatalf("%v %v", got, err)
	}
}

type fakeEngine struct {
	listener    net.Listener
	connections atomic.Int32
	selected    atomic.Int32
	precommit   bool
	change      bool
	wg          sync.WaitGroup
}

func fakeSocket(t *testing.T, precommit, change bool) (*fakeEngine, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// The fake engine is a Unix-socket server; menu capture is an
		// offline evaluation tool run on macOS and Linux.
		t.Skip("the fake engine listens on a Unix socket")
	}
	root, err := ipc.TempDir("sx-menu-test-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "s")
	l, err := net.Listen("unix", socket)
	if err != nil {
		os.RemoveAll(root)
		t.Fatal(err)
	}
	f := &fakeEngine{listener: l, precommit: precommit, change: change}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			connection := f.connections.Add(1)
			f.wg.Add(1)
			go func() { defer f.wg.Done(); defer c.Close(); f.serve(c, connection) }()
		}
	}()
	t.Cleanup(func() { l.Close(); f.wg.Wait(); os.RemoveAll(root) })
	return f, socket
}

func (f *fakeEngine) serve(c net.Conn, connection int32) {
	reader := ipc.NewReader(c, 4096)
	page := 0
	code := ""
	for {
		typ, payload, err := reader.Next()
		if err != nil {
			return
		}
		if typ == ipc.TypeHello {
			c.Write((&ipc.Welcome{Version: ipc.Version}).Append(nil))
			continue
		}
		r := ipc.Result{Ctx: 1, Flags: ipc.ResultComposing | ipc.ResultHandled, PageNo: uint16(page)}
		switch typ {
		case ipc.TypeFocusIn:
		case ipc.TypeKey:
			var key ipc.Key
			if key.Decode(payload) != nil {
				return
			}
			r.Seq = key.Seq
			code += string(rune(key.Keysym))
			if f.precommit {
				r.Commit = []byte("unexpected")
			}
		case ipc.TypeChangePage:
			var p ipc.ChangePage
			if p.Decode(payload) != nil {
				return
			}
			r.Seq = p.Seq
			page++
			r.PageNo = uint16(page)
		case ipc.TypeSelect:
			var selectItem ipc.Select
			if selectItem.Decode(payload) != nil {
				return
			}
			r.Seq = selectItem.Seq
			f.selected.Add(1)
			texts := [][]string{{"时间", "是"}, {"世界", "数据"}}
			if page == 0 && selectItem.Index == 1 {
				r.Preedit = []byte("是 j")
			} else {
				r.Commit = []byte(texts[page][selectItem.Index])
				r.Flags = ipc.ResultHandled
			}
			c.Write(r.Append(nil))
			continue
		default:
			return
		}
		r.Preedit = []byte(code)
		if page == 0 {
			r.Candidates = []ipc.Candidate{{Text: []byte("时间"), Comment: []byte("时 间")}, {Text: []byte("是")}}
		} else {
			r.Candidates = []ipc.Candidate{{Text: []byte("世界")}, {Text: []byte("数据")}}
			r.Flags |= ipc.ResultLastPage
		}
		if f.change && connection > 1 {
			r.Candidates[0].Text = []byte("变化")
		}
		c.Write(r.Append(nil))
	}
}

func TestCapturePagesAndVerifiesEachCandidateInFreshSession(t *testing.T) {
	f, socket := fakeSocket(t, false, false)
	r, err := capture(context.Background(), socket, "sj", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Candidates) != 3 || !r.Complete || !r.Truncated || r.LastPage {
		t.Fatalf("capture prefix %+v", r)
	}
	if f.connections.Load() != 4 || f.selected.Load() != 3 {
		t.Fatalf("fresh connections %d, selections %d", f.connections.Load(), f.selected.Load())
	}
	if r.Candidates[0].Text != "时间" || r.Candidates[0].Comment != "时 间" || r.Candidates[2].Text != "世界" || r.Candidates[2].Page != 1 {
		t.Fatalf("reader buffer lifetime/order %+v", r.Candidates)
	}
	if !r.Candidates[0].FullSpan || r.Candidates[1].FullSpan || !r.Candidates[2].FullSpan {
		t.Fatalf("whole-input flags %+v", r.Candidates)
	}
	if *r.Candidates[0].SpanStart != 0 || *r.Candidates[0].SpanEnd != 2 || r.Candidates[1].SpanStart != nil {
		t.Fatal("fabricated/incorrect span")
	}
}

func TestCaptureMarksEndAndRefusesPreselectionCommits(t *testing.T) {
	_, socket := fakeSocket(t, false, false)
	r, err := capture(context.Background(), socket, "sj", 50)
	if err != nil || !r.LastPage || r.Truncated || len(r.Candidates) != 4 {
		t.Fatalf("end %+v %v", r, err)
	}
	_, unsafeSocket := fakeSocket(t, true, false)
	if _, err := capture(context.Background(), unsafeSocket, "sj", 5); err == nil || !strings.Contains(err.Error(), "before selection") {
		t.Fatalf("precommit: %v", err)
	}
}

func TestChangingCandidateIsRetainedWithoutInventingSpan(t *testing.T) {
	_, socket := fakeSocket(t, false, true)
	r, err := capture(context.Background(), socket, "sj", 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Candidates[0].Text != "时间" || r.Candidates[0].FullSpan || r.Candidates[0].Verification != "candidate_changed" {
		t.Fatalf("dynamic candidate %+v", r.Candidates[0])
	}
}

func TestCaptureRealGeneration(t *testing.T) {
	if os.Getenv("SHIXIN_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set SHIXIN_CAPTURE_INTEGRATION=1 for a real isolated generation")
	}
	repo, _ := filepath.Abs("../..")
	lib := os.Getenv("SHIXIN_LIBRIME")
	if lib == "" {
		lib = filepath.Join(repo, "build/librime/out/lib/librime.1.dylib")
	}
	if _, err := os.Stat(lib); err != nil {
		t.Skip("built librime unavailable")
	}
	data := filepath.Join(repo, "build/rime-data")
	if _, err := os.Stat(filepath.Join(data, "rime_ice.schema.yaml")); err != nil {
		t.Skip("rime-ice unavailable")
	}
	root := t.TempDir()
	build := func(pkg, name string) string {
		path := filepath.Join(root, name)
		cmd := exec.Command("go", "build", "-o", path, pkg)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, out)
		}
		return path
	}
	engine := build("./cmd/shixin-engine", "shixin-engine")
	builder := build("./cmd/shixin-builder", "shixin-builder")
	generation := filepath.Join(root, "generation")
	cmd := exec.Command(builder, "-lib", lib, "-data", data, "-output", generation)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("builder: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	w, err := startWorker(ctx, options{engine: engine, generation: generation, data: data, lib: lib})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			w.close()
		}
	}()
	if len(w.ready.Schemas) != 1 || w.ready.Schemas[0] != "rime_ice_warm" {
		t.Fatalf("promoted capture schema %v", w.ready.Schemas)
	}
	a, err := capture(ctx, w.ready.Socket, "nihao", 12)
	if err != nil {
		t.Fatal(err)
	}
	b, err := capture(ctx, w.ready.Socket, "nihao", 12)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Candidates, b.Candidates) {
		t.Fatal("probes changed read-only menu")
	}
	if len(a.Candidates) < 6 || a.Candidates[0].Text != "你好" || !a.Candidates[0].FullSpan {
		t.Fatalf("real menu %+v", a)
	}
	workerRoot := filepath.Dir(w.ready.Socket)
	err = filepath.WalkDir(workerRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(d.Name(), "userdb") {
			t.Errorf("capture opened user dictionary %s", d.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w.close()
	closed = true
	if _, err := os.Stat(workerRoot); !os.IsNotExist(err) {
		t.Fatalf("capture scratch remains: %v", err)
	}
	// The normal command emits one JSON row per code and keeps public data explicit.
	input, output := filepath.Join(root, "codes.jsonl"), filepath.Join(root, "menus.jsonl")
	os.WriteFile(input, []byte("{\"code\":\"tw\"}\n"), 0o600)
	if err := run(ctx, options{engine: engine, generation: generation, data: data, lib: lib, input: input, output: output, profile: "test-profile", limit: 5, timeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var row record
	if err := json.NewDecoder(f).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row.Profile != "test-profile" || row.Schema != "rime_ice_warm" || row.CapturedAt == "" {
		t.Fatalf("missing capture metadata %+v", row)
	}
	var extra any
	if err := json.NewDecoder(f).Decode(&extra); err != io.EOF {
		t.Fatalf("extra output: %v", err)
	}
}
