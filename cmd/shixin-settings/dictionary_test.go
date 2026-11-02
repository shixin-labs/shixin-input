package main

import (
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
