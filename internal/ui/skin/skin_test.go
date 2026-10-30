package skin

import (
	"testing"

	"shixin/internal/settings"
)

func TestSkinsMatchSettings(t *testing.T) {
	if len(All) != len(settings.Skins) {
		t.Fatalf("%d skins, settings lists %d", len(All), len(settings.Skins))
	}
	for i, s := range settings.Skins {
		if All[i].ID != s.ID {
			t.Errorf("skin %d is %q, settings has %q", i, All[i].ID, s.ID)
		}
	}
	if Get("nope").ID != All[0].ID {
		t.Error("unknown skin isn't the default")
	}
}
