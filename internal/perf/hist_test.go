package perf

import (
	"testing"
	"time"
)

func TestQuantiles(t *testing.T) {
	var h Hist
	for i := 1; i <= 1000; i++ {
		h.Add(time.Duration(i) * time.Microsecond)
	}
	for _, c := range []struct {
		q    float64
		want time.Duration
	}{{.5, 500 * time.Microsecond}, {.99, 990 * time.Microsecond}, {1, 1000 * time.Microsecond}} {
		got := h.Quantile(c.q)
		if got < c.want || float64(got) > float64(c.want)*1.13 {
			t.Errorf("q%v = %v, want ≈%v", c.q, got, c.want)
		}
	}
	if h.Max() != time.Millisecond || h.Count() != 1000 {
		t.Error(h.String())
	}
}

func TestBuckets(t *testing.T) {
	prev := -1
	for us := uint64(0); us < 1<<20; us = us*9/8 + 1 {
		b := bucket(us)
		if b < prev || lower(b) > us {
			t.Fatalf("bucket(%d) = %d (lower %d), prev %d", us, b, lower(b), prev)
		}
		prev = b
	}
}

func TestAddDoesNotAllocate(t *testing.T) {
	var h Hist
	if n := testing.AllocsPerRun(1000, func() { h.Add(123 * time.Microsecond) }); n != 0 {
		t.Fatal(n)
	}
}

func TestHistBinary(t *testing.T) {
	var h Hist
	for _, us := range []int{3, 40, 41, 900, 12000} {
		h.Add(time.Duration(us) * time.Microsecond)
	}
	b := append(h.Append(nil), 0xAA)
	var g Hist
	rest, err := g.Decode(b)
	if err != nil || len(rest) != 1 || rest[0] != 0xAA || g != h {
		t.Fatalf("round trip: %v %x %+v", err, rest, g)
	}
	if len(g.Buckets()) != 4 {
		t.Errorf("buckets %v", g.Buckets())
	}
	if _, err := g.Decode(b[:20]); err == nil {
		t.Error("short histogram decoded")
	}
}
