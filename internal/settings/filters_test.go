package settings

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNativeFilterSettings(t *testing.T) {
	s := Default()
	s.LongWordPriority, s.DemoteEnglish, s.VSymbolsFirst, s.EnglishAutocap = true, true, true, true
	s.Pins = []Pin{{Scheme: "rime_ice", Code: "ni", Text: "泥"}, {Scheme: "double_pinyin", Code: "ui", Text: "是"}}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatal("filter settings did not round trip", err)
	}
	files, err := Compile(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range Schemes {
		var config struct{ Patch map[string]any }
		if err := yaml.Unmarshal(files[schema.ID+".custom.yaml"], &config); err != nil {
			t.Fatal(err)
		}
		p := config.Patch
		if p["shixin_filters/long_word"] != true || p["shixin_filters/demote_english"] != true || p["shixin_filters/autocap"] != true || p["shixin_filters/v_symbols"] != (schema.ID == "rime_ice") {
			t.Fatal("filter flags missing in", schema.ID)
		}
		text := string(files[schema.ID+".custom.yaml"])
		if schema.ID != "rime_ice" && strings.Contains(text, "泥") {
			t.Fatal("pin leaked to another layout")
		}
		if schema.ID != "double_pinyin" && strings.Contains(text, "ui\\t是") {
			t.Fatal("double-pinyin pin leaked")
		}
		list := p["engine/filters"].([]any)
		if list[0] != "shixin_rerank" || list[len(list)-1] != "shixin_pin_filter" {
			t.Fatal("pins must win after reranking and other word rules")
		}
	}
	if !EngineChanged(Default(), s) {
		t.Fatal("filter edits did not request a generation")
	}
}

func TestPinLimitsAndRepair(t *testing.T) {
	s := Default()
	for i := 0; i < 11; i++ {
		s.Pins = append(s.Pins, Pin{Scheme: "rime_ice", Code: "ni", Text: string(rune('你' + i))})
	}
	if s.Validate() == nil {
		t.Fatal("accepted more than ten pins for a code")
	}
	s.Pins = append(s.Pins, Pin{Scheme: "rime_ice", Code: "bad\ncode", Text: "private pin"})
	if err := s.repair(); err == nil || strings.Contains(err.Error(), "private pin") {
		t.Fatal("repair failed or disclosed text")
	}
	if len(s.Pins) != 10 || s.Validate() != nil {
		t.Fatal("repair lost valid pins")
	}
}
