package main

import "testing"

func TestNewerBuild(t *testing.T) {
	for _, c := range []struct {
		got, have string
		want      bool
	}{
		{"20261027235857", "20261027235433", true},
		{"20261027235433", "20261027235857", false}, // a downgrade
		{"20261027235857", "20261027235857", false}, // the same build
		{"20261027235857", "20261027-223527", true}, // older scripts' format
		{"20261027-223527", "20261027235857", false},
		{"20261027235857", "", true}, // unreadable installed build
		{"", "20261027235857", false},
		{"", "", false},
	} {
		if got := newerBuild(c.got, c.have); got != c.want {
			t.Errorf("newerBuild(%q, %q) = %v, want %v", c.got, c.have, got, c.want)
		}
	}
}
