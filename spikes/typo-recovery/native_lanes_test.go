package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSeparateLaneCounterfactualKeepsOrdinaryAndCountsFullSpans(t *testing.T) {
	ordinary := nativeMenu{Code: "abc", Complete: true}
	for i := 0; i < 6; i++ {
		ordinary.Candidates = append(ordinary.Candidates, nativeCandidate{Text: "ordinary", Rank: i + 1, Full: true, End: 3})
	}
	ordinary.Candidates = append(ordinary.Candidates, nativeCandidate{Text: "干净词", Rank: 7, Full: true, End: 3})
	mixed := nativeMenu{Code: "abc", Complete: true, Candidates: append([]nativeCandidate(nil), ordinary.Candidates[:5]...)}
	mixed.Candidates = append(mixed.Candidates, nativeCandidate{Text: "前缀", Comment: "纠错 → test", Rank: 6, Full: false, End: 2}, nativeCandidate{Text: "纠错词", Comment: "纠错 → test", Rank: 7, Full: true, End: 3})
	before := append([]nativeCandidate(nil), mixed.Candidates...)
	lane := capturedCorrectionLane(mixed)
	if !reflect.DeepEqual(before, mixed.Candidates) {
		t.Fatal("mutated captured menu")
	}
	if correctionLaneRank(lane, "前缀") != 0 || correctionLaneRank(lane, "纠错词") != 2 {
		t.Fatal("partial span or slot accounting incorrect")
	}
	root, source := t.TempDir(), t.TempDir()
	for name, menu := range map[string]nativeMenu{"ordinary": ordinary, "protected": mixed} {
		b, _ := json.Marshal(menu)
		if e := os.WriteFile(filepath.Join(root, name+".jsonl"), append(b, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
	labels := `{"raw":"abc","target":"干净词","split":"dev","form":"full","edit":{"kind":"clean"}}
{"raw":"abc","target":"纠错词","split":"dev","form":"full","edit":{"kind":"neighbor"}}
{"raw":"abc","target":"前缀","split":"dev","form":"full","edit":{"kind":"neighbor"}}
`
	if e := os.WriteFile(filepath.Join(source, "selected.jsonl"), []byte(labels), 0600); e != nil {
		t.Fatal(e)
	}
	if e := nativeLanes(root, source); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(root, "separate-lanes.json"))
	if e != nil {
		t.Fatal(e)
	}
	var got struct {
		Metrics map[string]laneMetric `json:"metrics"`
	}
	if e = json.Unmarshal(data, &got); e != nil {
		t.Fatal(e)
	}
	clean, typo := got.Metrics["clean"], got.Metrics["typo"]
	if clean.Ordinary13 != 1 || clean.Mixed13 != 0 || clean.MixedLosses != 1 || clean.Union[2] != 1 {
		t.Fatalf("clean: %+v", clean)
	}
	if typo.Examples != 2 || typo.Additional[1] != 0 || typo.Additional[2] != 1 || typo.Union[2] != 1 || typo.FirstScreen[2] != 1 || clean.FirstScreen[2] != 0 {
		t.Fatalf("typo: %+v", typo)
	}
	mixed.Complete = false
	b, _ := json.Marshal(mixed)
	if e = os.WriteFile(filepath.Join(root, "protected.jsonl"), append(b, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	if e = nativeLanes(root, source); e == nil {
		t.Fatal("accepted incomplete capture")
	}
}
