package candview

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"

	"shixin/internal/ipc"
	"shixin/internal/ui/skin"
)

func stateResults() []ipc.Result {
	return []ipc.Result{
		{Preedit: []byte("shi"), Candidates: []ipc.Candidate{{Text: []byte("是")}, {Text: []byte("时"), Comment: []byte("time")}}},
		{Preedit: []byte("shi xin"), Highlighted: 1, Candidates: []ipc.Candidate{{Text: []byte("拾新")}, {Text: []byte("实心")}, {Text: []byte("🆕"), Comment: []byte("emoji")}}},
		{Preedit: []byte("shi xin"), Candidates: []ipc.Candidate{{Text: []byte("实心")}, {Text: []byte("拾新")}}},
	}
}

func TestStateOwnsText(t *testing.T) {
	var st State
	r := stateResults()[1]
	st.Apply(&r)
	clear(r.Preedit)
	for i := range r.Candidates {
		clear(r.Candidates[i].Text)
		clear(r.Candidates[i].Comment)
	}
	if st.Preedit != "shi xin" || st.Items[0].Text != "拾新" || st.Items[2].Comment != "emoji" || st.Highlight != 1 {
		t.Fatalf("display retained mutable IPC bytes: %+v", st.Items)
	}
}

func TestStateCachedUpdatesDoNotAllocate(t *testing.T) {
	var st State
	results := stateResults()
	for i := range results {
		st.Apply(&results[i])
	}
	if n := testing.AllocsPerRun(1000, func() {
		for i := range results {
			st.Apply(&results[i])
		}
		st.Clear()
	}); n != 0 {
		t.Fatalf("cached state updates allocate %v times", n)
	}
	st.Apply(&results[0])
	if len(st.Items) != 2 || st.Items[0].Label != "1" || st.Items[1].Comment != "time" {
		t.Fatalf("restoring the state lost labels/comments: %+v", st.Items)
	}
}

func TestStringCacheIsBounded(t *testing.T) {
	var c stringCache
	for i := range 2 * stringCacheEntries {
		c.intern([]byte(strconv.Itoa(i)))
		if c.count > stringCacheEntries || c.bytes > stringCacheBytes || len(c.values) != c.count {
			t.Fatalf("cache escaped its bounds: count %d bytes %d map %d", c.count, c.bytes, len(c.values))
		}
	}
	if _, ok := c.values["0"]; ok {
		t.Fatal("old entries were never evicted")
	}
	large := bytes.Repeat([]byte("x"), stringCacheBytes/2)
	c.intern(large)
	large[0] = 'y'
	c.intern(large)
	large[0] = 'z'
	c.intern(large)
	if c.bytes > stringCacheBytes || c.count != 2 {
		t.Fatalf("byte budget was not enforced: %d bytes in %d entries", c.bytes, c.count)
	}
	uncached := bytes.Repeat([]byte("w"), stringCacheBytes+1)
	if c.intern(uncached) != string(uncached) || c.count != 2 {
		t.Fatal("oversized text was truncated or retained in the cache")
	}
}

func TestSelectionHandlerUsesCurrentPageAndCallback(t *testing.T) {
	st := sample()
	style := &Style{Skin: skin.Get("shixin"), FontSize: 17}
	oldCalls, picked := 0, -1
	selectItem := func(int) { oldCalls++ }
	tt := ui.NewTester(func(c *ui.Context) { Draw(c, st, style, selectItem) }, 600, 200)
	// Hide, then reuse the slot for a new page and a new selection target.
	st.Clear()
	tt.Frame()
	r := stateResults()[0]
	st.Apply(&r)
	selectItem = func(i int) { picked = i }
	tt.Frame()
	if err := tt.Click("时"); err != nil {
		t.Fatal(err)
	}
	if picked != 1 || oldCalls != 0 {
		t.Fatalf("cached click used a stale target: index %d, old calls %d", picked, oldCalls)
	}
}

func TestCandidateFramesDoNotAllocate(t *testing.T) {
	for _, family := range []string{"", "Songti SC", "Menlo"} {
		for _, vertical := range []bool{false, true} {
			var st State
			results := stateResults()
			style := &Style{Skin: skin.Get("shixin"), FontFamily: family, FontSize: 17, Vertical: vertical}
			selectItem := func(int) {}
			tt := ui.NewTester(func(c *ui.Context) { Measure(c, &st, style); Draw(c, &st, style, selectItem) }, 600, 240)
			run := func() {
				for i := range results {
					st.Apply(&results[i])
					tt.Frame()
				}
				st.Clear()
				tt.Frame()
			}
			for range 10 {
				run()
			}
			if n := testing.AllocsPerRun(100, run); n != 0 {
				t.Fatalf("font %q vertical %v: warmed typing/page/hide frames allocate %v times", family, vertical, n)
			}
		}
	}
}
