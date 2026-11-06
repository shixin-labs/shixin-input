package main

import "testing"

func TestNativeEvaluationUsesVerifiedSpan(t *testing.T) {
	m := nativeMenu{Candidates: []nativeCandidate{{Text: "尝试错误", Rank: 1}, {Text: "尝试错误", Rank: 6, Full: true, End: 13}}}
	if got := nativeTargetRank(m, "尝试错误"); got != 6 {
		t.Fatalf("rank=%d: partial match must not count", got)
	}
	a := nativeMenu{Candidates: []nativeCandidate{{Text: "你好", Rank: 1, Full: true, End: 5}}}
	b := nativeMenu{Candidates: append(append([]nativeCandidate{}, a.Candidates...), nativeCandidate{Text: "你要", Rank: 2})}
	if !nativePrefixRetained(a, b) {
		t.Fatal("appended candidates changed existing prefix")
	}
	if !nativePrefixRetained(nativeMenu{}, b) {
		t.Fatal("empty ordinary prefix must be retained")
	}
	b.Candidates[0].Full = false
	if nativePrefixRetained(a, b) {
		t.Fatal("same text with different consumption counted as retained")
	}
}
