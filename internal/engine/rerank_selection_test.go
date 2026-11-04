package engine

import (
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
	"shixin/internal/rerank"
)

func TestRerankSelections(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	var perm [rerank.MaxOptions]uint8
	identity := func(sl *rerank.Slot) []uint8 {
		for i := range int(sl.Count) {
			perm[i] = uint8(i)
		}
		return perm[:sl.Count]
	}
	click := func(index uint32) {
		cl.seq++
		cl.send((&ipc.Select{Ctx: 1, Seq: cl.seq, Index: index}).Append(cl.buf[:0]))
		cl.result()
	}
	for _, tc := range []struct {
		name, code        string
		choose            func()
		total, top1, top5 uint64
	}{
		{"space", "ceshi", func() { cl.key(' ', 0) }, 1, 1, 1},
		{"digit", "ceshi", func() { cl.key('3', 0) }, 1, 0, 1},
		{"click", "ceshi", func() { click(1) }, 1, 0, 1},
		{"highlight", "ceshi", func() { cl.key(policy.KeyDown, 0); cl.key(' ', 0) }, 1, 0, 1},
		{"page-two", "qiancheng", func() { cl.key(policy.KeyNext, 0); cl.key(' ', 0) }, 1, 0, 0},
		{"page-two-click", "qiancheng", func() { cl.key(policy.KeyNext, 0); click(1) }, 1, 0, 0},
		{"cancel", "ceshi", func() { cl.key(policy.KeyEscape, 0) }, 0, 0, 0},
		{"raw-input", "ceshi", func() { cl.key(policy.KeyReturn, 0) }, 0, 0, 0},
		{"punctuation", "ceshi", func() { cl.key('.', 0) }, 1, 1, 1},
		// Only shorter prefixes share a menu with the pinned 上海市.
		{"pinned", "shanghaishi", func() { cl.key(' ', 0) }, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "pinned" {
				rerankShortWords(t, e)
			}
			testScorer.use(t, identity, false)
			before := e.Stats().Rerank
			cl.typ(tc.code)
			tc.choose()
			after := e.Stats().Rerank
			if got := [3]uint64{after.Selections - before.Selections, after.Top1 - before.Top1, after.Top5 - before.Top5}; got != [3]uint64{tc.total, tc.top1, tc.top5} {
				t.Fatalf("selection counts %v, want %v", got, [3]uint64{tc.total, tc.top1, tc.top5})
			}
			cl.key(policy.KeyEscape, 0)
		})
	}
	cl.send((&ipc.StatsRequest{}).Append(cl.buf[:0]))
	typ, payload, err := cl.r.Next()
	var st ipc.Stats
	if err != nil || typ != ipc.TypeStats || st.Decode(payload) != nil || st.Rerank == nil || st.Rerank.Selections == nil {
		t.Fatalf("selection Stats: %v %v", typ, err)
	}
	want := e.Stats().Rerank
	if s := st.Rerank.Selections; s.Total != want.Selections || s.Top1 != want.Top1 || s.Top5 != want.Top5 {
		t.Fatalf("IPC selections %+v, engine %+v", s, want)
	}
}

func TestRerankSelectionsPartialAndInvalidation(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	var perm [rerank.MaxOptions]uint8
	order := func(sl *rerank.Slot) []uint8 {
		for i := range int(sl.Count) {
			perm[i] = uint8(i)
		}
		for i := range int(sl.Count) {
			if string(sl.Text(i)) == "北京" {
				perm[0], perm[i] = perm[i], perm[0]
				break
			}
		}
		return perm[:sl.Count]
	}
	rerankShortWords(t, e)
	testScorer.use(t, order, false)
	before := e.Stats().Rerank
	if firstText(cl.typ("beijingshi")) != "北京" {
		t.Fatal("shorter prefix was not promoted")
	}
	cl.key(' ', 0)
	if after := e.Stats().Rerank; after.Selections != before.Selections+1 || after.Top1 != before.Top1+1 {
		t.Fatalf("partial selection not counted once: before %+v after %+v", before, after)
	}
	cl.key(policy.KeyEscape, 0)
	for _, mode := range []string{"edit", "clear", "focus", "secure", "off"} {
		t.Run(mode, func(t *testing.T) {
			testScorer.use(t, order, false)
			cl.typ("ceshi")
			testScorer.use(t, nil, false)
			before := e.Stats().Rerank.Selections
			switch mode {
			case "edit":
				cl.key(policy.KeyBackSpace, 0)
				cl.key('i', 0)
			case "clear":
				cl.key(policy.KeyEscape, 0)
				cl.typ("ceshi")
			case "focus":
				cl.focus("selections-focus", 0)
			case "secure":
				cl.focus("selections-focus", ipc.FocusSecure)
			case "off":
				e.SetRerank(false, false)
				defer e.SetRerank(true, false)
			}
			cl.key(' ', 0)
			if after := e.Stats().Rerank.Selections; after != before {
				t.Fatalf("%s retained stale menu rating", mode)
			}
			cl.key(policy.KeyEscape, 0)
			if mode == "secure" {
				cl.focus("test", 0)
			}
		})
	}
}

func TestRerankSelectionsExcludeUnratedMenus(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	for _, mode := range []string{"off", "unavailable", "timeout", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "off" {
				e.SetRerank(false, false)
				defer e.SetRerank(true, false)
			}
			var order func(*rerank.Slot) []uint8
			if mode == "invalid" {
				order = func(*rerank.Slot) []uint8 { return []uint8{0} }
			}
			testScorer.use(t, order, mode == "timeout")
			before := e.Stats().Rerank
			cl.typ("ceshi")
			cl.key(' ', 0)
			if after := e.Stats().Rerank; after.Selections != before.Selections {
				t.Fatalf("%s menu counted as AI-rated", mode)
			}
		})
	}
}
