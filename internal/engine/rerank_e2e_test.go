package engine

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"shixin/internal/ai"
	"shixin/internal/ipc"
	"shixin/internal/llama"
	"shixin/internal/policy"
	"shixin/internal/rerank"
	"shixin/internal/settings"
)

// TestRerankEndToEnd runs 智能排序 for real: rime-ice menus in our librime,
// the engine's Remote over a socket, and shixin-ai's server with the model
// on the GPU, all in one child process. It commits a context by typing,
// then a 简拼 code, and reports where the expected word is in rime's order
// and in the one result the code's last key gets, and how long that took. Slow; needs rime-ice, our
// librime, scripts/build-llama.sh and the model; skipped with -short.
func TestRerankEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: deploys rime-ice and loads the model")
	}
	llamaDir, _ := filepath.Abs("../../build/llama/lib")
	model, _ := filepath.Abs("../../build/models/ernie-4.5-0.3b-base-q8_0.gguf")
	if p := os.Getenv("SHIXIN_LLAMA"); p != "" {
		llamaDir = p
	}
	if p := os.Getenv("SHIXIN_MODEL"); p != "" {
		model = p
	}
	if !fileExists(model) || !fileExists(llamaDir) {
		t.Skip("model or llama.cpp missing (DESIGN §7.2)")
	}
	if os.Getenv("SHIXIN_CHILD") != "TestRerankEndToEnd" {
		runInChild(t, "TestRerankEndToEnd")
		return
	}
	lib, err := llama.Load(llamaDir)
	if err != nil {
		t.Fatal(err)
	}
	scorer, err := ai.New(lib, model)
	if err == ai.ErrNoAccelerator {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	dir, err := ipc.TempDir("sxe2e-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "ai.sock")
	l, err := ipc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go ai.NewServer(scorer, t.Logf).Serve(l)
	remote := rerank.NewRemote(sock, rerank.StrengthStandard, t.Logf)
	rec := &recordingScorer{Remote: remote}
	e := rimeIceEngine(t, settings.Default(), func(o *Options) { o.Scorer, o.Rerank = rec, true })
	defer e.Stop()
	if !e.rr.attached {
		t.Skip("librime without shixin_rerank")
	}
	stop := make(chan struct{})
	defer close(stop)
	go remote.Run(stop, e.Rerank)
	for deadline := time.Now().Add(5 * time.Second); !remote.Ready(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("shixin-ai not ready")
		}
	}
	cl := dial(t, e)
	cases := []struct{ context, code, want string }{
		{"women mingtian yiqi qu ", "tw", "台湾"},
		{"jintian xiawu keneng hui xia ", "zy", "阵雨"},
		{"wo zai xuexiao de ", "tsg", "图书馆"},
		{"zhege xiangmu de ", "jd", "进度"},
		{"qing ba wenjian fa dao wo de ", "yx", "邮箱"},
		{"ta shi yige hen youming de ", "zj", "作家"},
		{"women yao baohu ", "hj", "环境"},
		{"zhe jia canting de ", "fw", "服务"},
	}
	var waits []time.Duration
	rimeFirst, first, page := 0, 0, 0
	for i, c := range cases {
		cl.focus("e2e"+string(rune('a'+i)), 0)
		if r := cl.typ(c.context); len(r.Commit) == 0 && r.Flags&ipc.ResultComposing != 0 {
			cl.key(' ', 0)
		}
		// A person takes longer than the context's prefill to type on.
		time.Sleep(100 * time.Millisecond)
		cl.typ(c.code[:len(c.code)-1])
		t0 := time.Now()
		r := cl.key(uint32(c.code[len(c.code)-1]), 0)
		waits = append(waits, time.Since(t0))
		before, after := rec.rimePage(), texts(r)
		was, is := slices.Index(before, c.want), slices.Index(after, c.want)
		t.Logf("%-32q %-4s %s: rime %s → %s (rime #1 %s, shown #1 %s)", c.context, c.code, c.want, place(was), place(is), at(before, 0), at(after, 0))
		rimeFirst += b2i(was == 0)
		first += b2i(is == 0)
		page += b2i(is >= 0 && is < 5)
		cl.key(policy.KeyEscape, 0)
	}
	st := e.Stats().Rerank
	t.Logf("%d cases: #1 %d → %d, page 1 %d; applied %d, timeouts %d, superseded %d, dropped %d; last key → result %v; held results waited %v",
		len(cases), rimeFirst, first, page, st.Applied, st.Timeouts, st.Superseded, st.Dropped, waits, &st.Wait)
	if st.Applied == 0 || first <= rimeFirst {
		t.Errorf("reranking didn't help: #1 %d → %d, %d applied", rimeFirst, first, st.Applied)
	}
}

// recordingScorer keeps the first page of each export in rime's order.
type recordingScorer struct {
	*rerank.Remote
	mu   sync.Mutex
	page []string
}

func (r *recordingScorer) Score(req *rerank.Request) {
	r.mu.Lock()
	r.page = r.page[:0]
	for i := range min(int(req.Export.Count), 5) {
		r.page = append(r.page, string(req.Export.Text(i)))
	}
	r.mu.Unlock()
	r.Remote.Score(req)
}

func (r *recordingScorer) rimePage() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.page)
}

func place(i int) string {
	if i < 0 {
		return "off page 1"
	}
	return "#" + string(rune('1'+i))
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
