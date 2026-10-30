package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
)

func TestFullPinyinFallback(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		for _, dark := range []bool{false, true} {
			a, saves, applies := testApp(t)
			a.s.Scheme = "double_pinyin_flypy"
			a.saved, a.seen = clone(a.s), clone(a.s)
			tt := ui.NewTester(a.view, 880, 720)
			tt.SetDark(dark)
			for i, want := range []bool{true, false} {
				if err := tt.Click(T("s.g.full_pinyin")); err != nil {
					t.Fatal(err)
				}
				tt.Advance(time.Second)
				tt.Frame()
				if a.s.FullPinyinFallback != want || len(*saves) != i+1 || (*saves)[i].FullPinyinFallback != want || *applies != i+1 || a.status != statusSaved {
					t.Fatalf("fallback %v saves %d applies %d", a.s.FullPinyinFallback, len(*saves), *applies)
				}
				if want {
					name := "mixed-pinyin-" + language.name
					if dark {
						name += "-dark"
					}
					chartSnapshot(t, tt, name)
				}
			}
			a.s.Scheme, a.s.FullPinyinFallback = "rime_ice", true
			a.saved, a.seen = clone(a.s), clone(a.s)
			tt.Frame()
			if tt.HasText(T("s.g.full_pinyin")) || !a.s.FullPinyinFallback {
				t.Fatal("full-pinyin mode must hide the control and preserve its value")
			}
		}
	}
}
