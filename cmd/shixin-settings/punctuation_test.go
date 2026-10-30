package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
)

func TestPunctuationSelection(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		t.Run(language.name, func(t *testing.T) {
			a, saves, applies := testApp(t)
			tt := ui.NewTester(a.view, 880, 640)
			for i, style := range []string{"english", "chinese"} {
				if err := tt.Click(T("s.g.punctuation." + style)); err != nil {
					t.Fatal(err)
				}
				tt.Advance(time.Second)
				if a.s.Punctuation != style || len(*saves) != i+1 || (*saves)[i].Punctuation != style || *applies != i+1 || a.status != statusSaved {
					t.Fatalf("punctuation %q saves %d applies %d status %v", a.s.Punctuation, len(*saves), *applies, a.status)
				}
			}
		})
	}
}
