package engine

import (
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
	"shixin/internal/rerank"
)

func TestRerankBackspaceUpdatesContext(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, false)
	cl.focus("backspace-context", 0)
	cl.typ("chanpin")
	if got := string(cl.key(' ', 0).Commit); got != "产品" {
		t.Fatalf("commit %q", got)
	}
	cl.typ("hua")
	if got := string(cl.key(policy.KeyReturn, 0).Commit); got != "hua" {
		t.Fatalf("raw commit %q", got)
	}
	if _, got, _ := testScorer.lastContext(); got != "产品hua" {
		t.Fatalf("commit context %q", got)
	}
	for i, want := range []string{"产品hu", "产品h", "产品"} {
		if cl.policy().Lookup(policy.KeyBackSpace, 0).Disposition() != policy.PassNotify {
			t.Fatal("Backspace must pass and notify")
		}
		cl.seq++
		cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: policy.KeyBackSpace, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
		cl.result()
		if _, got, _ := testScorer.lastContext(); got != want {
			t.Fatalf("Backspace %d context %q, want %q", i+1, got, want)
		}
	}
	cl.typ("hua")
	if _, got, _ := testScorer.lastContext(); got != "产品" {
		t.Fatalf("next conversion context %q", got)
	}
	cl.key(policy.KeyEscape, 0)
}

func TestRerankUncertainEditsForgetContext(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, false)
	notify := func(key, mods uint32) {
		if key >= 0x20 && key < 0x7f && (key < '0' || key > '9') {
			mods |= policy.ReleaseMask
		}
		cl.seq++
		cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: key, Mods: mods, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
		cl.result()
	}
	for _, tc := range []struct {
		name      string
		key, mods uint32
	}{
		{"word-delete", policy.KeyBackSpace, policy.AltMask},
		{"line-delete", policy.KeyBackSpace, policy.SuperMask},
		{"forward-delete", policy.KeyDelete, 0},
		{"selection", 'a', policy.SuperMask},
		{"paste", 'v', policy.ControlMask},
		{"undo", 'z', policy.SuperMask},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl.focus("uncertain-"+tc.name, 0)
			cl.typ("chanpin")
			cl.key(' ', 0)
			if d := cl.policy().Lookup(tc.key, tc.mods).Disposition(); d != policy.PassNotify {
				t.Fatalf("edit policy %v", d)
			}
			notify(tc.key, tc.mods)
			if _, got, _ := testScorer.lastContext(); got != "" {
				t.Fatalf("uncertain edit kept %q", got)
			}
		})
	}
	cl.focus("release-backspace", 0)
	cl.typ("chanpin")
	cl.key(' ', 0)
	cl.typ("hua")
	cl.key(policy.KeyReturn, 0)
	notify(policy.KeyBackSpace, 0)
	notify(policy.KeyBackSpace, policy.ReleaseMask)
	if _, got, _ := testScorer.lastContext(); got != "产品hu" {
		t.Fatalf("key release deleted extra text: %q", got)
	}
	// Copy/save preserve the known suffix; a caret move destroys that certainty.
	notify('c', policy.SuperMask)
	notify('s', policy.ControlMask)
	if _, got, _ := testScorer.lastContext(); got != "产品hu" {
		t.Fatalf("copy/save changed context: %q", got)
	}
	notify(policy.KeyLeft, 0)
	notify(policy.KeyBackSpace, 0)
	if _, got, _ := testScorer.lastContext(); got != "" {
		t.Fatalf("delete after caret move kept %q", got)
	}
}

func TestRerankBackspaceDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, false)
	cl.focus("backspace-allocations", 0)
	cycle := func() {
		cl.typ("chanpin")
		cl.key(' ', 0)
		cl.typ("hua")
		cl.key(policy.KeyReturn, 0)
		for range 3 {
			cl.seq++
			cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: policy.KeyBackSpace, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
			cl.result()
		}
	}
	// Fill the bounded history and warm the fake scorer's copied context.
	for range 70 {
		cycle()
	}
	if n := testing.AllocsPerRun(30, cycle); n != 0 {
		t.Fatalf("Backspace input cycle: %g Go allocations", n)
	}
}

func TestRerankEditsInvalidateRepeatedCode(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	var perm [rerank.MaxOptions]uint8
	testScorer.use(t, func(sl *rerank.Slot) []uint8 {
		for i := range int(sl.Count) {
			perm[i] = uint8(i)
		}
		return perm[:sl.Count]
	}, false)
	cl.focus("repeated-code-after-edit", 0)
	initial := testScorer.requestCount()
	cl.typ("q")
	before := testScorer.requestCount()
	if before <= initial {
		t.Fatal("fixture did not produce an initial scoring request")
	}
	cl.key(policy.KeyReturn, 0)
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: policy.KeyBackSpace, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
	cl.result()
	cl.typ("q")
	if testScorer.requestCount() <= before {
		t.Fatal("reused an order from the context before deletion")
	}
	cl.key(policy.KeyEscape, 0)
}

func TestRerankRejectsReplyFromBeforeContextEdit(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, true)
	cl.focus("old-context-answer", 0)
	want := firstText(cl.typ("q"))
	old := testScorer.takeHeld()
	if len(old) != 1 {
		t.Fatalf("initial held requests %v", old)
	}
	n := len(testScorer.rimeOrder())
	if n < 2 {
		t.Fatal("fixture needs two candidates")
	}
	cl.key(policy.KeyReturn, 0)
	before := e.Stats().Rerank.Dropped
	var oldPerm, fresh [rerank.MaxOptions]uint8
	for i := 0; i < n; i++ {
		oldPerm[i] = uint8(i)
	}
	oldPerm[0], oldPerm[1] = oldPerm[1], oldPerm[0]
	testScorer.use(t, func(sl *rerank.Slot) []uint8 {
		if sl.ExportSeq == old[0].exportSeq {
			t.Error("context edit did not invalidate reply identity")
		}
		e.Rerank(old[0].session, old[0].exportSeq, oldPerm[:n])
		for i := range int(sl.Count) {
			fresh[i] = uint8(i)
		}
		return fresh[:sl.Count]
	}, false)
	if got := firstText(cl.typ("q")); got != want {
		t.Fatalf("stale answer changed new menu from %q to %q", want, got)
	}
	if e.Stats().Rerank.Dropped != before+1 {
		t.Fatal("old context answer was not dropped")
	}
	cl.key(policy.KeyEscape, 0)
}

func TestRerankPassedInputInvalidatesDeletionSuffix(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, false)
	for _, key := range []uint32{' ', policy.KeyTab, policy.KeyReturn} {
		cl.focus("passed-input", 0)
		cl.typ("chanpin")
		cl.key(' ', 0)
		cl.typ("hua")
		cl.key(policy.KeyReturn, 0)
		if d := cl.policy().Lookup(key, 0).Disposition(); d != policy.PassNotify {
			t.Fatalf("passed key %#x policy %v", key, d)
		}
		mods := uint32(0)
		if key == ' ' {
			mods = policy.ReleaseMask
		}
		cl.seq++
		cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: key, Mods: mods, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
		cl.result()
		cl.seq++
		cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: policy.KeyBackSpace, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
		cl.result()
		if _, got, _ := testScorer.lastContext(); got != "" {
			t.Fatalf("delete after passed key %#x retained %q", key, got)
		}
	}
	// Unknown keys whose ASK is declined are owned by the app too.
	cl.typ("chanpin")
	cl.key(' ', 0)
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: 0xffffff, Flags: ipc.KeySync}).Append(cl.buf[:0]))
	cl.result()
	if _, got, _ := testScorer.lastContext(); got != "" {
		t.Fatalf("declined ASK retained %q", got)
	}
}

func TestRerankCommitWithPassedCharacterInvalidatesSuffix(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, false)
	for _, flags := range []uint8{ipc.KeySync, 0} {
		cl.focus("commit-plus-passed-character", 0)
		cl.typ("chanpin")
		cl.seq++
		cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: 'A', Mods: policy.ShiftMask, Flags: flags}).Append(cl.buf[:0]))
		r := cl.result()
		if string(r.Commit) != "产品" || r.Flags&ipc.ResultHandled != 0 {
			t.Fatalf("expected commit plus declined A: commit %q flags %x", r.Commit, r.Flags)
		}
		cl.seq++
		cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: policy.KeyBackSpace, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
		cl.result()
		if _, got, _ := testScorer.lastContext(); got != "" {
			t.Fatalf("deleting passed A removed committed Chinese: %q", got)
		}
	}
	cl.typ("chanpin")
	cl.key(' ', 0)
	cl.key('A', policy.ShiftMask) // a repaired/declined printable with no composition commit
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: policy.KeyBackSpace, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
	cl.result()
	if _, got, _ := testScorer.lastContext(); got != "" {
		t.Fatalf("declined printable retained deletion suffix: %q", got)
	}
}

func TestRerankInvalidationKeepsVisibleMenuStable(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, reverse, false)
	want := firstText(cl.typ("ceshi"))
	e.SetRerank(false)
	e.SetRerank(true)
	before := testScorer.requestCount()
	if got := firstText(cl.key('a', policy.ReleaseMask)); got != want {
		t.Fatalf("invalidating context reordered visible menu: %q to %q", want, got)
	}
	if testScorer.requestCount() != before {
		t.Fatal("already-visible menu was scored after invalidation")
	}
	cl.key(policy.KeyEscape, 0)
	cl.typ("q")
	if testScorer.requestCount() <= before {
		t.Fatal("new native menu was not scored")
	}
	cl.key(policy.KeyEscape, 0)
}
