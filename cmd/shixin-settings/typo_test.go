package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
)

func TestTypoCorrection(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		for _, dark := range []bool{false, true} {
			a, saves, applies := testApp(t)
			a.s.Scheme = "rime_ice"
			a.saved, a.seen = clone(a.s), clone(a.s)
			tt := ui.NewTester(a.view, 880, 720)
			tt.SetDark(dark)
			for i, want := range []bool{true, false} {
				if err := tt.Click(T("s.g.typo")); err != nil {
					t.Fatal(err)
				}
				tt.Advance(time.Second)
				tt.Frame()
				if a.s.TypoCorrection != want || len(*saves) != i+1 || (*saves)[i].TypoCorrection != want || *applies != i+1 || a.status != statusSaved {
					t.Fatalf("fallback %v saves %d applies %d", a.s.TypoCorrection, len(*saves), *applies)
				}
				if want {
					name := "typo-correction-" + language.name
					if dark {
						name += "-dark"
					}
					chartSnapshot(t, tt, name)
				}
			}
			a.s.Scheme, a.s.TypoCorrection = "double_pinyin_flypy", true
			a.saved, a.seen = clone(a.s), clone(a.s)
			tt.Frame()
			if tt.HasText(T("s.g.typo")) || !a.s.TypoCorrection {
				t.Fatal("shuangpin mode must hide the control and preserve its value")
			}
		}
	}
}
