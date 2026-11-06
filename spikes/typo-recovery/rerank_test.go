package main

import (
	"testing"
)

func TestContextPrefixUsesOnlyPrecedingText(t *testing.T) {
	prefix, ok := contextPrefix("这个孩子平时表现很好，十分讨人喜欢。", "讨人")
	if !ok || prefix != "这个孩子平时表现很好，十分" {
		t.Fatalf("%q %v", prefix, ok)
	}
	if _, ok = contextPrefix("讨人喜欢的孩子今天来到了学校。", "讨人"); ok {
		t.Fatal("accepted absent prefix")
	}
	if _, ok = contextPrefix("这个孩子很讨人喜欢", "讨人"); ok {
		t.Fatal("accepted unqualified source")
	}
}
func TestCorrectionCostsPreserveExactAndChooseMinimum(t *testing.T) {
	raw := menu{Candidates: []candidate{c("原词", true)}}
	menus := map[string]menu{"a": {Candidates: []candidate{c("原词", true), c("新词", true)}}, "b": {Candidates: []candidate{c("新词", true)}}}
	costs := correctionCosts(raw, []hit{{Code: "a", Cost: 10}, {Code: "b", Cost: 8}}, menus)
	if costs["原词"] != 0 || costs["新词"] != .8 {
		t.Fatal(costs)
	}
}
