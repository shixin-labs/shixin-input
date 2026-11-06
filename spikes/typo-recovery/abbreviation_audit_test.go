package main

import "testing"

func TestLabeledPathBelowQuota(t *testing.T) {
	hits := []lexicalHit{{Prior: 10, Cost: 8}, {Prior: 10, Cost: 8}, {Prior: 10, Cost: 8}, {Prior: 10, Cost: 8}}
	if !labeledPathBelowQuota(9, "swap", hits) {
		t.Fatal("four strictly better typo paths should explain quota")
	}
	if labeledPathBelowQuota(10, "swap", hits) {
		t.Fatal("ties must remain unresolved")
	}
	if labeledPathBelowQuota(9, "clean", hits) {
		t.Fatal("other category does not consume clean quota")
	}
	if labeledPathBelowQuota(9, "swap", hits[:3]) {
		t.Fatal("unfilled quota cannot explain loss")
	}
}
