package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
)

func TestForgetShortcut(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		for _, dark := range []bool{false, true} {
			a, saves, applies := testApp(t)
			a.page = "dictionary"
			tt := ui.NewTester(a.view, 880, 720)
			tt.SetDark(dark)
			if !tt.HasText(keyLabel("Control+Shift+BackSpace")) {
				t.Fatal("dictionary page does not explain the shortcut")
			}
			if err := tt.Click(T("s.d.shortcuts")); err != nil {
				t.Fatal(err)
			}
			if a.page != "shortcuts" {
				t.Fatal("shortcut link failed")
			}
			previous := "Control+Shift+BackSpace"
			for i, key := range []string{"Control+Delete", "", "Control+Shift+BackSpace"} {
				if err := tt.Click(T("s.k.forget") + " " + keyLabel(previous)); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyHome)
				steps := map[string]int{"Control+Shift+BackSpace": 0, "Control+Delete": 1, "": 2}[key]
				for range steps {
					tt.Key(0, ui.KeyDown)
				}
				tt.Key(0, ui.KeyEnter)
				tt.Advance(time.Second)
				tt.Frame()
				if a.s.Hotkeys["forget"] != key || len(*saves) != i+1 || (*saves)[i].Hotkeys["forget"] != key || *applies != i+1 || a.status != statusSaved {
					t.Fatalf("forget %q saves %d applies %d status %v", a.s.Hotkeys["forget"], len(*saves), *applies, a.status)
				}
				previous = key
			}
		}
	}
}
