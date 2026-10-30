package main

import (
	"bytes"
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/ui/candview"
)

func candidateResults() []ipc.Result {
	return []ipc.Result{
		{Preedit: []byte("ni"), Candidates: []ipc.Candidate{{Text: []byte("你")}, {Text: []byte("呢")}}},
		{Preedit: []byte("ni hao"), Highlighted: 1, Candidates: []ipc.Candidate{{Text: []byte("你好")}, {Text: []byte("你号"), Comment: []byte("test")}}},
	}
}

func TestCandidateUpdateOwnsAndCoalesces(t *testing.T) {
	var u candidateUpdate
	u.init()
	var st candview.State
	results := candidateResults()
	u.capture(&results[0])
	u.capture(&results[1])
	clear(results[1].Preedit)
	clear(results[1].Candidates[0].Text)
	u.apply(&st)
	if st.Preedit != "ni hao" || len(st.Items) != 2 || st.Items[0].Text != "你好" || st.Items[1].Comment != "test" || st.Highlight != 1 {
		t.Fatalf("coalesced display lost or borrowed text: %q %+v", st.Preedit, st.Items)
	}
	u.capture(&results[0])
	if st.Items[0].Text != "你好" {
		t.Fatal("overwriting the pending buffer changed already displayed text")
	}
	u.capture(&ipc.Result{})
	u.apply(&st)
	if u.visible || len(st.Items) != 0 || st.Preedit != "" {
		t.Fatal("a late frame resurrected candidates after a hide")
	}
}

func TestCandidateCaptureDoesNotAllocate(t *testing.T) {
	var u candidateUpdate
	u.init()
	results := candidateResults()
	// Fill a valid maximum-size frame as well as changing ordinary text.
	large := ipc.Result{Candidates: []ipc.Candidate{{}}}
	room := ipc.MaxFrame + 4 - len(large.Append(nil))
	large.Candidates[0].Text = bytes.Repeat([]byte("a"), room)
	results = append(results, large, ipc.Result{})
	if n := testing.AllocsPerRun(100, func() {
		for i := range results {
			u.capture(&results[i])
		}
	}); n != 0 {
		t.Fatalf("key-path capture allocates %v times", n)
	}
}

func BenchmarkCandidateUpdate(b *testing.B) {
	for _, apply := range []bool{false, true} {
		name := "capture"
		if apply {
			name = "capture-and-cached-state"
		}
		b.Run(name, func(b *testing.B) {
			var u candidateUpdate
			u.init()
			var st candview.State
			results := candidateResults()
			for i := range results {
				u.capture(&results[i])
				u.apply(&st)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				u.capture(&results[i%len(results)])
				if apply {
					u.apply(&st)
				}
			}
		})
	}
}
