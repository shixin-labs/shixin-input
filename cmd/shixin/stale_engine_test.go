package main

import (
	"testing"
	"time"
)

func TestPredates(t *testing.T) {
	install := time.Date(2026, 11, 1, 6, 52, 0, 0, time.UTC)
	for _, c := range []struct {
		started time.Time
		want    bool
	}{
		{install.Add(-6 * time.Hour), true},           // the old engine kept running
		{install.Add(-500 * time.Millisecond), false}, // within the slack
		{install.Add(10 * time.Second), false},        // started after the install
	} {
		if got := predates(c.started, install); got != c.want {
			t.Errorf("predates(%v) = %v, want %v", c.started.Sub(install), got, c.want)
		}
	}
}
