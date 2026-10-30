package ai

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/rerank"
)

// fakeModel reverses the candidates and records what it was given.
type fakeModel struct {
	mu       sync.Mutex
	warm     map[int]uint32
	prefills []string
	scored   []string // first candidate of each request
}

func (f *fakeModel) Prefill(app int, rev uint32, text []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warm[app] = rev
	f.prefills = append(f.prefills, string(text))
	return nil
}

func (f *fakeModel) Warm(app int, rev uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.warm[app]
	return ok && r == rev
}

func (f *fakeModel) Score(app int, cands []Candidate, p rerank.Params, perm []uint8) ([]uint8, error) {
	f.mu.Lock()
	f.scored = append(f.scored, string(cands[0].Text))
	f.mu.Unlock()
	perm = perm[:len(cands)]
	for i := range perm {
		perm[i] = uint8(len(perm) - 1 - i)
	}
	return perm, nil
}

func (f *fakeModel) snapshot() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.prefills), slices.Clone(f.scored)
}

type answer struct {
	session   int
	exportSeq uint32
	perm      []uint8
}

func export(seq uint32, words ...string) *rerank.Slot {
	sl := &rerank.Slot{ExportSeq: seq, Count: uint16(len(words))}
	used := 0
	for i, w := range words {
		copy(sl.Arena[used:], w)
		sl.Entries[i] = rerank.Entry{Offset: uint16(used), Length: uint16(len(w))}
		used += len(w)
	}
	return sl
}

func until(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRemoteAndServer(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sxai-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "ai.sock")
	l, err := ipc.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	model := &fakeModel{warm: map[int]uint32{}}
	go NewServer(model, t.Logf).Serve(l)

	var mu sync.Mutex
	var answers []answer
	got := func() []answer {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(answers)
	}
	r := rerank.NewRemote(path, rerank.StrengthStandard, t.Logf)
	// Before connecting: kept and replayed.
	r.Context(1, 7, []byte("我们去"))
	stop := make(chan struct{})
	defer close(stop)
	go r.Run(stop, func(s int, seq uint32, perm []uint8) {
		mu.Lock()
		answers = append(answers, answer{s, seq, slices.Clone(perm)})
		mu.Unlock()
	})
	until(t, r.Ready)
	until(t, func() bool { p, _ := model.snapshot(); return slices.Contains(p, "我们去") })

	r.Score(&rerank.Request{Session: 3, ExportSeq: 41, App: 1, AppRev: 7, Export: export(41, "台湾", "太晚", "抬碗")})
	until(t, func() bool { return len(got()) == 1 })
	if a := got()[0]; a.session != 3 || a.exportSeq != 41 || !slices.Equal(a.perm, []uint8{2, 1, 0}) {
		t.Fatalf("answer %+v", a)
	}

	// A request for a context revision the model never got is dropped.
	r.Score(&rerank.Request{Session: 3, ExportSeq: 42, App: 1, AppRev: 99, Export: export(42, "台湾", "太晚")})
	// A context update, then a request for it: prefilled, then answered.
	r.Context(1, 8, []byte("我们去台湾"))
	r.Score(&rerank.Request{Session: 3, ExportSeq: 43, App: 1, AppRev: 8, Export: export(43, "旅游", "绿油")})
	until(t, func() bool { return len(got()) == 2 })
	time.Sleep(50 * time.Millisecond)
	if a := got(); len(a) != 2 || a[1].exportSeq != 43 {
		t.Fatalf("answers %+v", a)
	}

	// A newer engine replaces the connection; the first reconnects.
	r2 := rerank.NewRemote(path, rerank.StrengthCautious, t.Logf)
	stop2 := make(chan struct{})
	go r2.Run(stop2, func(int, uint32, []uint8) {})
	until(t, r2.Ready)
	until(t, func() bool { return !r.Ready() || r2.Ready() })
	close(stop2)
	until(t, func() bool { return !r2.Ready() })
	until(t, r.Ready) // reconnected, contexts replayed
	until(t, func() bool {
		p, _ := model.snapshot()
		n := 0
		for _, s := range p {
			if s == "我们去台湾" {
				n++
			}
		}
		return n >= 2
	})
}

// TestWorkerNewestWins: of several requests for one input context, only
// the newest is answered; contexts collapse to the latest per app.
func TestWorkerNewestWins(t *testing.T) {
	var w worker
	for seq := uint32(1); seq <= 3; seq++ {
		w.take(message{typ: rerank.TypeScore, score: rerank.Score{Session: 5, ExportSeq: seq}})
	}
	w.take(message{typ: rerank.TypeScore, score: rerank.Score{Session: 6, ExportSeq: 9}})
	w.take(message{typ: rerank.TypeContext, ctx: rerank.Context{App: 1, Rev: 1}})
	w.take(message{typ: rerank.TypeContext, ctx: rerank.Context{App: 1, Rev: 2}})
	if len(w.scores) != 2 || w.scores[0].Session != 5 || w.scores[0].ExportSeq != 3 || w.scores[1].Session != 6 {
		t.Errorf("scores %+v", w.scores)
	}
	if len(w.contexts) != 1 || w.contexts[0].Rev != 2 {
		t.Errorf("contexts %+v", w.contexts)
	}
}
