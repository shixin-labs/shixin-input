package main

import "testing"

func TestLexicalMergePreservesOrdinaryPageAndZeroCost(t *testing.T) {
	var baseline []candidate
	for _, s := range []string{"一", "二", "三", "四", "五", "六"} {
		baseline = append(baseline, candidate{Text: s, Full: true})
	}
	out, cost := lexicalMerge(baseline, []lexicalHit{{Text: "一", Cost: 10}, {Text: "新", Cost: 0}, {Text: "新", Cost: 8}, {Text: "错", Cost: 10}})
	for i := 0; i < 5; i++ {
		if out[i] != baseline[i] {
			t.Fatal("changed ordinary page")
		}
	}
	if len(out) != 8 || cost["一"] != 0 || cost["新"] != 0 || cost["错"] != 1 {
		t.Fatalf("bad merge: %v %v", out, cost)
	}
	guard := preservePage(baseline, []candidate{{Text: "新", Full: true}, {Text: "一", Full: true}})
	for i := 0; i < 5; i++ {
		if guard[i] != baseline[i] {
			t.Fatal("guard changed base AI page")
		}
	}
}
func TestRecoveryRequiresNativeAcceptance(t *testing.T) {
	menus := map[string]menu{"nihao": {Complete: true, Candidates: []candidate{{Text: "你好", Full: true}, {Text: "你", Full: false}}}}
	hits, n, e := verifiedProposals([]lexicalHit{{Code: "nihao", Text: "你好"}, {Code: "nihao", Text: "你"}, {Code: "nihao", Text: "不存在"}}, menus)
	if e != nil || len(hits) != 1 || n != 2 {
		t.Fatalf("%v %d %v", hits, n, e)
	}
	if _, _, e = verifiedProposals([]lexicalHit{{Code: "missing"}}, menus); e == nil {
		t.Fatal("accepted missing verification")
	}
}

// The safe merge is applied to whichever ordinary order the engine already has:
// an AI order, an AI-off menu, or a timeout fallback. It needs no new scores.
func TestSinglePassMergePreservesEveryExistingFirstPage(t *testing.T) {
	for n := 0; n <= 50; n++ {
		var ordinary []candidate
		for i := 0; i < n; i++ {
			ordinary = append(ordinary, candidate{Text: string(rune(0x4e00 + i)), Full: i%3 != 0, Rank: i + 1})
		}
		hits := []lexicalHit{{Text: "新词", Cost: 10}, {Text: "备用", Cost: 0}, {Text: "一", Cost: 8}}
		merged, _ := lexicalMerge(ordinary, hits)
		for i := 0; i < min(5, n); i++ {
			if merged[i] != ordinary[i] {
				t.Fatalf("n=%d changed first page at %d", n, i)
			}
		}
		if len(merged) > 50 {
			t.Fatal("pool cap exceeded")
		}
	}
}
