package engine

import (
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

// Match the client's asynchronous printable notification, including the
// synthetic release used to prevent Rime from inserting the key again.
func notifyPassedASCII(cl *client, ch byte) {
	mods := uint32(0)
	if shifted(ch) {
		mods |= policy.ShiftMask
	}
	if ch < '0' || ch > '9' {
		mods |= policy.ReleaseMask
	}
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: uint32(ch), Mods: mods, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
	cl.result()
}

func TestRerankMixedASCIIContext(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, false)
	for _, prefix := range []string{"", "产品"} {
		cl.focus("mixed-ascii-"+prefix, 0)
		if prefix != "" {
			cl.typ("chanpin")
			cl.key(' ', 0)
		}
		want := prefix
		for _, ch := range []byte(" Q3 !") {
			notifyPassedASCII(cl, ch)
			want += string(ch)
			if _, got, _ := testScorer.lastContext(); got != want {
				t.Fatalf("passed %q: context %q, want %q", ch, got, want)
			}
		}
		// A genuine release (without KeyNotify) must not append twice.
		cl.key('Q', policy.ShiftMask|policy.ReleaseMask)
		if _, got, _ := testScorer.lastContext(); got != want {
			t.Fatalf("release changed context: %q", got)
		}
	}
}

func TestRerankMixedASCIIDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, false)
	cl.focus("mixed-ascii-allocations", 0)
	cycle := func() {
		for _, ch := range []byte(" Q3 ") {
			notifyPassedASCII(cl, ch)
		}
	}
	for range 150 {
		cycle()
	}
	if n := testing.AllocsPerRun(100, cycle); n != 0 {
		t.Fatalf("passed ASCII input allocated %g times", n)
	}
}

func TestRerankMixedASCIIExcludesEnglishMode(t *testing.T) {
	e := rerankEngine(t)
	cl := dial(t, e)
	testScorer.use(t, nil, false)
	cl.focus("mixed-ascii-english-mode", 0)
	cl.typ("chanpin")
	cl.key(' ', 0)
	cl.setASCII(true)
	t.Cleanup(func() { cl.setASCII(false) })
	// Even if a queued notification reaches the engine after switching
	// modes, ordinary English text must not enter the context.
	for _, ch := range []byte(" Q3 ") {
		notifyPassedASCII(cl, ch)
	}
	if _, got, _ := testScorer.lastContext(); got != "产品\n" {
		t.Fatalf("English-mode notification entered context: %q", got)
	}
}
