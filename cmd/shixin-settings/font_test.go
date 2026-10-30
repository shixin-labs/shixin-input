package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
)

func TestFontSelection(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		for _, dark := range []bool{false, true} {
			a, saves, applies := testApp(t)
			a.page = "appearance"
			// Keep the interaction deterministic on CI machines with different
			// installed fonts; real family enumeration and sizing have their
			// own tests.
			a.fonts = []string{"Arial", "Songti SC"}
			tt := ui.NewTester(a.view, 880, 640)
			tt.SetDark(dark)
			if err := tt.Click(T("s.a.font.choose")); err != nil {
				t.Fatal(err)
			}
			tt.Type("song")
			if !tt.HasText("Songti SC") || tt.HasText("Arial") {
				t.Fatalf("font search did not filter the list: %q", tt.Texts())
			}
			tt.Key(0, ui.KeyEnter)
			tt.Advance(time.Second)
			tt.Frame()
			if a.s.FontFamily != "Songti SC" || len(*saves) != 1 || (*saves)[0].FontFamily != "Songti SC" || *applies != 0 || a.status != statusSaved {
				t.Fatalf("font %q saves %d applies %d status %v", a.s.FontFamily, len(*saves), *applies, a.status)
			}
			name := "font-selected-" + language.name
			if dark {
				name += "-dark"
			}
			chartSnapshot(t, tt, name)
			if err := tt.Click(T("s.a.font.choose")); err != nil {
				t.Fatal(err)
			}
			tt.Type("Shixin nonexistent font")
			tt.Key(0, ui.KeyEscape)
			tt.Key(0, ui.KeyTab)
			tt.Advance(time.Second)
			if a.s.FontFamily != "Songti SC" || len(*saves) != 1 {
				t.Fatal("unconfirmed font search changed the preference")
			}
			if err := tt.Click(T("s.a.font.choose")); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, ui.KeyDown)
			if err := tt.Click(T("s.a.font.system")); err != nil {
				t.Fatal(err)
			}
			tt.Advance(time.Second)
			if a.s.FontFamily != "" || len(*saves) != 2 || *applies != 0 {
				t.Fatalf("reset font %q saves %d applies %d", a.s.FontFamily, len(*saves), *applies)
			}
		}
	}
}

func TestUnavailableFont(t *testing.T) {
	a, saves, applies := testApp(t)
	a.page = "appearance"
	a.s.FontFamily = "Font on another machine"
	a.saved, a.seen = clone(a.s), clone(a.s)
	tt := ui.NewTester(a.view, 720, 480)
	if !tt.HasText(T("s.a.font.missing")) {
		t.Fatal("missing font fallback is not explained")
	}
	tt.Advance(time.Second)
	if a.s.FontFamily != "Font on another machine" || len(*saves) != 0 || *applies != 0 {
		t.Fatal("unavailable font preference was lost")
	}
	tt.Scroll(600, 350, 0, 160)
	chartSnapshot(t, tt, "font-unavailable-small")
}
