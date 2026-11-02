package main

import (
	"strings"
	"testing"

	"shixin/internal/dictionary"
	"shixin/internal/settings"
)

func TestDictionaryRestorePreservesPreferences(t *testing.T) {
	s := settings.Default()
	s.Skin = "ink"
	p := settings.Phrase{Code: "yx", Text: "邮箱"}
	pin := settings.Pin{Scheme: "rime_ice", Code: "ni", Text: "你"}
	s.Phrases = []settings.Phrase{p}
	s.Pins = []settings.Pin{pin}
	b := dictionary.Backup{Phrases: []settings.Phrase{p, {Code: "dz", Text: "地址"}}, Pins: []settings.Pin{pin}}
	merged, err := mergeDictionary(s, b)
	if err != nil || merged.Skin != "ink" || len(merged.Phrases) != 2 || len(merged.Pins) != 1 || len(s.Phrases) != 1 {
		t.Fatal("restore lost preferences, duplicated data or aliased controls", err)
	}
	a := &app{s: merged}
	a.resetDefaults()
	if len(a.s.Pins) != 1 || len(a.s.Phrases) != 2 {
		t.Fatal("restore defaults erased dictionary data")
	}
}

func TestDictionaryMergeRejectsOverflowWithoutChangingInput(t *testing.T) {
	s := settings.Default()
	var b dictionary.Backup
	for i := range 257 {
		b.Pins = append(b.Pins, settings.Pin{Scheme: "rime_ice", Code: "a" + strings.Repeat("b", i%60) + string(rune('a'+i/60)), Text: "字"})
	}
	if _, err := mergeDictionary(s, b); err == nil {
		t.Fatal("merge accepted more than 256 pins")
	}
	if len(s.Pins) != 0 {
		t.Fatal("failed merge changed the current settings")
	}
	// Merging the same backup twice adds nothing the second time.
	b = dictionary.Backup{Phrases: []settings.Phrase{{Code: "yx", Text: "邮箱"}}, Pins: []settings.Pin{{Scheme: "rime_ice", Code: "ni", Text: "你"}}}
	once, err := mergeDictionary(s, b)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := mergeDictionary(once, b)
	if err != nil || len(twice.Phrases) != 1 || len(twice.Pins) != 1 {
		t.Fatalf("repeated merge duplicated data: %v %+v", err, twice)
	}
}

func TestMovePinUpIgnoresStaleClicks(t *testing.T) {
	a, b, c := settings.Pin{Scheme: "rime_ice", Code: "ni", Text: "你"}, settings.Pin{Scheme: "rime_ice", Code: "ni", Text: "泥"}, settings.Pin{Scheme: "rime_ice", Code: "ni", Text: "拟"}
	pins := []settings.Pin{a, b, c}
	movePinUp(pins, 2, 1, c)
	if pins[1] != c || pins[2] != b {
		t.Fatalf("move: %+v", pins)
	}
	// A click rendered before the list changed must not move another pin
	// or index past the end.
	movePinUp(pins, 2, 1, c)
	movePinUp(pins[:2], 2, 1, b)
	movePinUp(pins, 1, -1, c)
	if pins[0] != a || pins[1] != c || pins[2] != b {
		t.Fatalf("stale click changed pins: %+v", pins)
	}
}
