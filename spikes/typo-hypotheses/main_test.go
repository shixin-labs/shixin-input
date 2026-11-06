package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSpellingEditsAndExhaustion(t *testing.T) {
	p := &planner{Forms: map[string]float64{"ni": -1, "hao": -1}}
	p.Neighbors['s'-'a'] = "a"
	for _, raw := range []string{"nihoa", "nihso"} {
		r := p.query(raw, 32768, 0)
		if r.Exhausted || len(r.Hits) != 1 || r.Hits[0].Code != "nihao" {
			t.Fatalf("%s: %+v", raw, r)
		}
		if !reflect.DeepEqual(r.Hits, p.query(raw, 32768, 0).Hits) {
			t.Fatal("unstable ranking")
		}
	}
	r := p.query("nihoa", 1, 0)
	if !r.Exhausted || len(r.Hits) != 0 || r.Work > 1 {
		t.Fatalf("exhaustion: %+v", r)
	}
}

func TestTenPerSpellingAndDedup(t *testing.T) {
	p := plan{Hits: []hypothesis{{Code: "a"}, {Code: "b"}}}
	menus := map[string]menu{}
	for _, code := range []string{"a", "b"} {
		m := menu{Code: code, Complete: true}
		for i := 0; i < 20; i++ {
			m.Candidates = append(m.Candidates, candidate{fmt.Sprintf("%s%d", code, i), true})
		}
		menus[code] = m
	}
	pool, err := candidatePool(p, menus, 2, 10)
	if err != nil || len(pool) != 20 || pool["a10"] || pool["b10"] {
		t.Fatalf("per-spelling cap: %v %v", pool, err)
	}
	menus["b"].Candidates[0] = candidate{"a0", true}
	menus["b"].Candidates[1].Full = false
	pool, err = candidatePool(p, menus, 2, 10)
	if err != nil || len(pool) != 18 || pool["b10"] {
		t.Fatalf("dedup/no refill: %v %v", pool, err)
	}
	delete(menus, "a")
	if _, err = candidatePool(p, menus, 2, 10); err == nil {
		t.Fatal("missing capture accepted")
	}
}

func TestHypothesisBoundAndOrdering(t *testing.T) {
	p := &planner{Forms: map[string]float64{}}
	for c := byte('a'); c <= 'z'; c++ {
		p.Forms[string(c)] = -1
		for other := byte('a'); other <= 'z'; other++ {
			if c != other {
				p.Neighbors[c-'a'] += string(other)
			}
		}
	}
	r := p.query("abcdef", 32768, 0)
	if r.Exhausted || len(r.Hits) != 16 {
		t.Fatalf("bound: %+v", r)
	}
	seen := map[string]bool{}
	for i, h := range r.Hits {
		if seen[h.Code] || h.Code == r.Raw {
			t.Fatal("duplicate or unchanged hypothesis")
		}
		seen[h.Code] = true
		if i > 0 && better(h, r.Hits[i-1]) {
			t.Fatal("out of order")
		}
	}
	for _, raw := range []string{"", "ni'hao", "NIHAO"} {
		if len(p.query(raw, 32768, 0).Hits) != 0 {
			t.Fatalf("unsupported input %q", raw)
		}
	}
}
