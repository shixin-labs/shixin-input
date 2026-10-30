package rerank

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"
)

func text(c *Contexts, slot int) string { return string(c.Text(nil, slot)) }

func TestContextsPerApp(t *testing.T) {
	var c Contexts
	chat := c.Slot("chat")
	c.Append(chat, []byte("我们去"))
	editor := c.Slot("editor")
	if editor == chat {
		t.Fatal("two apps share a ring")
	}
	c.Append(editor, []byte("第一章"))
	if got := c.Slot("chat"); got != chat {
		t.Fatalf("chat came back in slot %d, want %d", got, chat)
	}
	c.Append(chat, []byte("台湾"))
	if got, want := text(&c, chat), "我们去台湾"; got != want {
		t.Errorf("chat = %q, want %q", got, want)
	}
	if got, want := text(&c, editor), "第一章"; got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
}

func TestContextsBreak(t *testing.T) {
	var c Contexts
	s := c.Slot("app")
	rev := c.Rev(s)
	c.Break(s)
	if c.Rev(s) != rev || text(&c, s) != "" {
		t.Fatal("a break in an empty ring changed it")
	}
	c.Append(s, []byte("你好"))
	c.Break(s)
	rev = c.Rev(s)
	c.Break(s)
	if c.Rev(s) != rev {
		t.Error("a repeated break changed the ring")
	}
	c.Append(s, []byte("再见"))
	if got, want := text(&c, s), "你好\n再见"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestContextsKeepsTail(t *testing.T) {
	var c Contexts
	s := c.Slot("app")
	long := strings.Repeat("一二三四五六七八九十", 20) // 200 runes
	c.Append(s, []byte(long))
	c.Append(s, []byte("ab"))
	want := string([]rune(long + "ab")[202-RingRunes:])
	if got := text(&c, s); got != want {
		t.Errorf("tail = %q, want %q", got, want)
	}
	c.Append(s, []byte{0xff})
	if got := []rune(text(&c, s)); got[len(got)-1] != '\uFFFD' {
		t.Errorf("invalid UTF-8 stored as %q", got[len(got)-1])
	}
}

func TestContextsLRU(t *testing.T) {
	var c Contexts
	slots := map[int]string{}
	for i := range Rings {
		app := fmt.Sprint("app", i)
		s := c.Slot(app)
		if _, dup := slots[s]; dup {
			t.Fatalf("%s reused slot %d before all were taken", app, s)
		}
		slots[s] = app
		c.Append(s, []byte(app))
	}
	c.Slot("app0") // app1 is now the least recently focused
	s := c.Slot("new")
	if slots[s] != "app1" {
		t.Fatalf("new app took %s's ring, want app1's", slots[s])
	}
	if got := text(&c, s); got != "" {
		t.Errorf("reused ring kept %q", got)
	}
	if got := text(&c, c.Slot("app0")); got != "app0" {
		t.Errorf("app0 = %q", got)
	}
}

func TestContextsRevisions(t *testing.T) {
	var c Contexts
	s := c.Slot("a")
	c.Append(s, []byte("字"))
	seen := map[uint32]bool{c.Rev(s): true}
	step := func(what string) {
		t.Helper()
		if seen[c.Rev(s)] {
			t.Errorf("%s reused revision %d", what, c.Rev(s))
		}
		seen[c.Rev(s)] = true
	}
	c.Append(s, []byte("词"))
	step("append")
	c.Break(s)
	step("break")
	c.Reset()
	step("reset")
	if text(&c, s) != "" {
		t.Error("reset kept text")
	}
	if got := c.Slot("b"); got != 0 {
		t.Errorf("after reset, the first app got slot %d", got)
	}
	step("reuse")
}

func TestContextsDoNotAllocate(t *testing.T) {
	var c Contexts
	apps := []string{"a", "b", "c"}
	commit := []byte("我们去台湾")
	buf := make([]byte, 0, 4*RingRunes)
	i := 0
	allocs := testing.AllocsPerRun(1000, func() {
		s := c.Slot(apps[i%len(apps)])
		c.Break(s)
		c.Append(s, commit)
		buf = c.Text(buf[:0], s)
		i++
	})
	if allocs != 0 {
		t.Errorf("%.1f allocations per focus and commit", allocs)
	}
}

// spikeStable is the spike's policy (spikes/laya-zeroshot/rank_eval.py, order
// with kind "stable"), without the guard and learned words.
func spikeStable(logp, w []float64, beta, margin float64, page int) []uint8 {
	s := make([]float64, len(logp))
	idx := make([]int, len(logp))
	for i := range logp {
		idx[i] = i
		s[i] = logp[i] + beta*math.Log((w[i]+1)/(w[0]+1))
	}
	ranked := slices.Clone(idx)
	sort.SliceStable(ranked, func(a, b int) bool { return s[ranked[a]] > s[ranked[b]] })
	if ranked[0] != 0 && s[ranked[0]]-s[0] < margin {
		ranked = slices.DeleteFunc(ranked, func(i int) bool { return i == 0 })
		ranked = slices.Insert(ranked, 0, 0)
	}
	page = min(page, len(ranked))
	out := append([]int{ranked[0]}, slices.Sorted(slices.Values(ranked[1:page]))...)
	out = append(out, slices.Sorted(slices.Values(ranked[page:]))...)
	r := make([]uint8, len(out))
	for i, v := range out {
		r[i] = uint8(v)
	}
	return r
}

func TestOrderMatchesSpike(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	perm := make([]uint8, MaxOptions)
	for range 5000 {
		n := 1 + rng.IntN(50)
		opts := make([]Option, n)
		logp, w := make([]float64, n), make([]float64, n)
		for i := range opts {
			// Coarse values make ties common.
			logp[i] = -float64(rng.IntN(12))
			w[i] = float64(rng.IntN(1000))
			opts[i] = Option{LogP: logp[i], Scored: true, Prior: math.Log(w[i] + 1)}
		}
		p := Params{Beta: []float64{0, 0.1, 0.3}[rng.IntN(3)], Margin: float64(rng.IntN(3)), Page: 5}
		got := Order(perm, opts, p)
		if want := spikeStable(logp, w, p.Beta, p.Margin, p.Page); !slices.Equal(got, want) {
			t.Fatalf("logp %v w %v %+v:\n got %v\nwant %v", logp, w, p, got, want)
		}
	}
}

func opts(logp ...float64) []Option {
	o := make([]Option, len(logp))
	for i, l := range logp {
		o[i] = Option{LogP: l, Scored: true, Prior: 5}
	}
	return o
}

func TestOrder(t *testing.T) {
	page := Params{Page: 3}
	tests := []struct {
		name string
		opts []Option
		p    Params
		want []uint8
	}{
		{"promotes the best", opts(-5, -4, -1, -3, -2), page, []uint8{2, 3, 4, 0, 1}},
		{"keeps #1 within the margin", opts(-5, -4, -4.5, -9), Params{Page: 3, Margin: 2}, []uint8{0, 1, 2, 3}},
		{"page keeps rime order", opts(-1, -9, -3, -9, -2, -9), page, []uint8{0, 2, 4, 1, 3, 5}},
		{"short menu", opts(-1), page, []uint8{0}},
		{"unscored first: rime order", func() []Option {
			o := opts(0, -1, 0)
			o[0].Scored = false
			return o
		}(), page, []uint8{0, 1, 2}},
		{"pruned never reach the page", func() []Option {
			o := opts(-5, -6, 0, -7, -8)
			o[2].Scored = false
			return o
		}(), page, []uint8{0, 1, 3, 2, 4}},
		{"beta weighs rime weights", func() []Option {
			o := opts(-2, -1)
			o[0].Prior, o[1].Prior = math.Log(1e6), 0
			return o
		}(), Params{Page: 3, Beta: 0.3}, []uint8{0, 1}},
		{"guard blocks a rare word", func() []Option {
			o := opts(-5, -1, -2)
			o[0].Prior, o[1].Prior, o[2].Prior = math.Log(1000), math.Log(10), math.Log(500)
			return o
		}(), Params{Page: 2, Guard: 0.1}, []uint8{2, 1, 0}},
		{"learned word drops one place", func() []Option {
			o := opts(-9, -1, -2, -3, -9, -9)
			o[0].User = true
			return o
		}(), page, []uint8{1, 0, 2, 3, 4, 5}},
		{"learned word kept near its place", func() []Option {
			o := opts(-1, -9, -9, -9, -2, -3)
			o[1].User = true
			return o
		}(), page, []uint8{0, 4, 1, 5, 2, 3}},
		{"fixed options stay", func() []Option {
			o := opts(-9, -1, -5, -2, -3)
			o[1].Fixed, o[3].Fixed = true, true
			return o
		}(), page, []uint8{4, 1, 2, 3, 0}},
		{"two learned words", func() []Option {
			o := opts(-9, -9, -1, -2, -3, -4)
			o[0].User, o[1].User = true, true
			return o
		}(), page, []uint8{2, 0, 1, 3, 4, 5}},
	}
	for _, tt := range tests {
		perm := make([]uint8, MaxOptions)
		if got := Order(perm, tt.opts, tt.p); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestOrderIsPermutation(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	perm := make([]uint8, MaxOptions)
	for range 5000 {
		n := 1 + rng.IntN(MaxOptions)
		o := make([]Option, n)
		for i := range o {
			o[i] = Option{LogP: -rng.Float64() * 10, Scored: rng.IntN(4) != 0, Prior: rng.Float64() * 10, User: rng.IntN(8) == 0, Fixed: rng.IntN(6) == 0}
		}
		got := Order(perm, o, Params{Beta: 0.3, Margin: 0.5, Guard: 0.05, Page: 5})
		seen := make([]bool, n)
		// rank counts places among the options that move.
		rank := func(i int) int {
			r := 0
			for j := range i {
				if !o[j].Fixed {
					r++
				}
			}
			return r
		}
		for i, v := range got {
			if int(v) >= n || seen[v] {
				t.Fatalf("not a permutation: %v", got)
			}
			seen[v] = true
			if o[v].Fixed && i != int(v) {
				t.Fatalf("fixed option %d moved to %d: %v", v, i, got)
			}
			if o[v].User && !o[v].Fixed && rank(i) > rank(int(v))+1 {
				t.Fatalf("learned word %d dropped to %d: %v", v, i, got)
			}
		}
		// Without learned words, everything after the page keeps rime order.
		for i := range o {
			o[i].User = false
		}
		got = Order(perm, o, Params{Beta: 0.3, Margin: 0.5, Guard: 0.05, Page: 5})
		// (Fixed options sit at their own index.)
		last := -1
		for _, v := range got[min(5, n):] {
			if o[v].Fixed {
				continue
			}
			if int(v) < last {
				t.Fatalf("after the page, rime order changed: %v", got)
			}
			last = int(v)
		}
	}
}

func TestOrderDoesNotAllocate(t *testing.T) {
	o := opts(-5, -4, -1, -3, -2, -7, -6, -8, -9, -10, -11, -12)
	perm := make([]uint8, MaxOptions)
	if a := testing.AllocsPerRun(1000, func() { Order(perm, o, Params{Beta: 0.3, Page: 5}) }); a != 0 {
		t.Errorf("%.1f allocations per order", a)
	}
}

func BenchmarkOrder50(b *testing.B) {
	rng := rand.New(rand.NewPCG(5, 6))
	o := make([]Option, 50)
	for i := range o {
		o[i] = Option{LogP: -rng.Float64() * 10, Scored: i < 13, Prior: rng.Float64() * 10}
	}
	perm := make([]uint8, MaxOptions)
	for b.Loop() {
		Order(perm, o, Params{Beta: 0.3, Margin: 0.5, Page: 5})
	}
}
