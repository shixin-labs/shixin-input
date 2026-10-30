package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
	"shixin/internal/settings"
)

func TestCandidateShortcutSelection(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		t.Run(language.name, func(t *testing.T) {
			a, saves, applies := testApp(t)
			a.page = "shortcuts"
			tt := ui.NewTester(a.view, 880, 640)
			label := T("s.off")
			for i, id := range []string{"control", "alt", "off"} {
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				next := T("s.off")
				if keys, ok := settings.CandidateKeyPair(id); ok {
					next = keyLabel(keys[0]) + " / " + keyLabel(keys[1])
				}
				if err := tt.Click(next); err != nil {
					t.Fatal(err)
				}
				tt.Advance(time.Second)
				if a.s.CandidateKeys != id || len(*saves) != i+1 || (*saves)[i].CandidateKeys != id || *applies != i+1 || a.status != statusSaved {
					t.Fatalf("candidate_keys %q saves %d applies %d status %v", a.s.CandidateKeys, len(*saves), *applies, a.status)
				}
				label = next
			}
		})
	}
}
