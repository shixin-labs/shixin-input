package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Counterfactual layout only. Comment labels identify entries in saved spike
// captures; product code must use typed provenance and fresh selection IDs.
func capturedCorrectionLane(menu nativeMenu) []nativeCandidate {
	var out []nativeCandidate
	for _, c := range menu.Candidates {
		if strings.HasPrefix(c.Comment, "纠错 → ") {
			out = append(out, c)
		}
	}
	return out
}
func correctionLaneRank(lane []nativeCandidate, target string) int {
	for i, c := range lane {
		if c.Text == target && c.Full {
			return i + 1
		}
	}
	return 0
}

type laneMetric struct {
	Ordinary5   int         `json:"ordinary_top5"`
	FirstScreen map[int]int `json:"ordinary5_plus_correction_slots"`
	Examples    int         `json:"examples"`
	Ordinary13  int         `json:"ordinary_top13"`
	Mixed13     int         `json:"mixed_top13"`
	MixedLosses int         `json:"mixed_losses_from_ordinary13"`
	Additional  map[int]int `json:"additional_targets_in_correction_slots"`
	Union       map[int]int `json:"ordinary13_plus_correction_slots"`
}

func nativeLanes(root, source string) error {
	rows, err := lines[abbrExample](filepath.Join(source, "selected.jsonl"))
	if err != nil {
		return err
	}
	load := func(name string) (map[string]nativeMenu, error) {
		rows, e := lines[nativeMenu](filepath.Join(root, name+".jsonl"))
		if e != nil {
			return nil, e
		}
		result := map[string]nativeMenu{}
		for _, r := range rows {
			if _, ok := result[r.Code]; ok || !r.Complete {
				return nil, fmt.Errorf("%s duplicate/incomplete %q", name, r.Code)
			}
			for i, c := range r.Candidates {
				if c.Rank != i+1 {
					return nil, fmt.Errorf("%s invalid ranks %q", name, r.Code)
				}
			}
			result[r.Code] = r
		}
		return result, nil
	}
	ordinary, err := load("ordinary")
	if err != nil {
		return err
	}
	mixed, err := load("protected")
	if err != nil {
		return err
	}
	metrics := map[string]*laneMetric{}
	displaced := []map[string]any{}
	for _, r := range rows {
		a, ok := ordinary[r.Raw]
		if !ok {
			return fmt.Errorf("missing ordinary %q", r.Raw)
		}
		b, ok := mixed[r.Raw]
		if !ok {
			return fmt.Errorf("missing mixed %q", r.Raw)
		}
		ar, br := nativeTargetRank(a, r.Target), nativeTargetRank(b, r.Target)
		before, after := ar > 0 && ar <= 13, br > 0 && br <= 13
		cr := correctionLaneRank(capturedCorrectionLane(b), r.Target)
		group := "typo"
		if r.Edit.Kind == "clean" {
			group = "clean"
		}
		for _, key := range []string{group, r.Split + "/" + r.Form + "/" + r.Edit.Kind} {
			m := metrics[key]
			if m == nil {
				m = &laneMetric{Additional: map[int]int{}, Union: map[int]int{}, FirstScreen: map[int]int{}}
				metrics[key] = m
			}
			m.Examples++
			if ar > 0 && ar <= 5 {
				m.Ordinary5++
			}
			if before {
				m.Ordinary13++
			}
			if after {
				m.Mixed13++
			}
			if before && !after {
				m.MixedLosses++
			}
			for _, n := range []int{1, 2, 4, 8} {
				found := cr > 0 && cr <= n
				if (ar > 0 && ar <= 5) || found {
					m.FirstScreen[n]++
				}
				if found && !before {
					m.Additional[n]++
				}
				if before || found {
					m.Union[n]++
				}
			}
		}
		if before && !after {
			displaced = append(displaced, map[string]any{"raw": r.Raw, "target": r.Target, "kind": r.Edit.Kind, "form": r.Form, "ordinary_rank": ar, "mixed_rank": br, "correction_lane_rank": cr})
		}
	}
	hashes := map[string]string{}
	for _, path := range []string{filepath.Join(source, "selected.jsonl"), filepath.Join(root, "ordinary.jsonl"), filepath.Join(root, "protected.jsonl")} {
		h, e := fileHash(path)
		if e != nil {
			return e
		}
		hashes[path] = h
	}
	return writeJSON(filepath.Join(root, "separate-lanes.json"), map[string]any{"metrics": metrics, "displaced_in_mixed_window": displaced, "sha256": hashes, "notes": []string{
		"Offline layout counterfactual, not native implementation or UI usability evidence.",
		"Ordinary candidates retain their baseline positions by construction. A separate correction lane exposes extra positions, so union availability is NOT top-13 recall or a fair fixed-slot ranking comparison.",
		"One/two/four/eight correction slots are counted independently of thirteen ordinary positions; no new model scoring or retrieval is performed.",
		"Labels come from reused synthetic data. Only verified full-input commits count as targets; partial candidates still consume correction slots.",
		"Capture comments identify correction provenance for this analysis only. Production requires typed metadata, current revision/selection IDs and independent paging.",
	}})
}
