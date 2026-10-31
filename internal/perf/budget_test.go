package perf

import (
	"testing"
	"time"
)

func TestOverBudget(t *testing.T) {
	for _, c := range []struct {
		env   string
		d     time.Duration
		limit time.Duration
		over  bool
	}{
		{"", 9 * time.Millisecond, 8 * time.Millisecond, true},
		{"", 8 * time.Millisecond, 8 * time.Millisecond, false},
		{"2", 9 * time.Millisecond, 16 * time.Millisecond, false},
		{"junk", 9 * time.Millisecond, 8 * time.Millisecond, true},
		{"off", time.Hour, 0, false},
	} {
		t.Setenv("SHIXIN_PERF_TOLERANCE", c.env)
		if limit, over := OverBudget(c.d, 8*time.Millisecond); limit != c.limit || over != c.over {
			t.Errorf("tolerance %q, %v: got %v %v, want %v %v", c.env, c.d, limit, over, c.limit, c.over)
		}
	}
}
