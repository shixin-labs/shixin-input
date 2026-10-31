package main

import "strings"

// bundleID is Shixin.app's bundle identifier, also its input source ID.
const bundleID = "im.shixin.inputmethod.Shixin"

// newerBuild reports whether build got is newer than have. Builds are
// timestamps (20261027235857, or 20261027-223527 from older scripts), so
// their digits compare as numbers; an unreadable installed build is older
// than anything.
func newerBuild(got, have string) bool {
	digits := func(s string) string {
		return strings.TrimLeft(strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s), "0")
	}
	g, h := digits(got), digits(have)
	if len(g) != len(h) {
		return len(g) > len(h)
	}
	return g > h
}
