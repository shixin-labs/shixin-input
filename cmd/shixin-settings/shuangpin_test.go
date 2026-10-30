package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
	"shixin/internal/settings"
)

func TestShuangpinCharts(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		for _, dark := range []bool{false, true} {
			for _, scheme := range settings.Schemes {
				layout := settings.Shuangpin(scheme.ID)
				if layout == nil {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/dark=%v", scheme.ID, language.name, dark), func(t *testing.T) {
					a, saves, applies := testApp(t)
					a.s.Scheme = scheme.ID
					a.saved, a.seen = clone(a.s), clone(a.s)
					tt := ui.NewTester(a.view, 880, 640)
					tt.SetDark(dark)
					if err := tt.Click(T("s.g.chart.open")); err != nil {
						t.Fatal(err)
					}
					if !a.chartShown || !tt.HasText(T("s.g.chart.zero")) {
						t.Fatal("chart did not open")
					}
					for _, key := range layout.Keys {
						chartTextWithin(t, tt, strings.ToUpper(key.Key), 880, 640)
					}
					chartTextWithin(t, tt, T("s.g.chart.note"), 880, 640)
					name := "shuangpin-" + scheme.ID
					if dark {
						name += "-dark"
					}
					if language.lang == i18n.English {
						name += "-en"
					}
					chartSnapshot(t, tt, name)
					if err := tt.Click(T("s.g.chart.close")); err != nil || a.chartShown {
						t.Fatalf("close chart: %v, open %v", err, a.chartShown)
					}
					if err := tt.Click(T("s.g.chart.open")); err != nil {
						t.Fatal(err)
					}
					tt.Key(0, ui.KeyEscape)
					tt.Advance(time.Second)
					if a.chartShown || len(*saves) != 0 || *applies != 0 {
						t.Fatalf("opening/closing a reference changed settings: open %v saves %d applies %d", a.chartShown, len(*saves), *applies)
					}
				})
			}
		}
	}
}

func TestShuangpinChartSmallWindow(t *testing.T) {
	defer i18n.SetLang(i18n.Current())
	for _, language := range testLanguages {
		i18n.SetLang(language.lang)
		for _, scheme := range settings.Schemes {
			if settings.Shuangpin(scheme.ID) == nil {
				continue
			}
			t.Run(scheme.ID+"/"+language.name, func(t *testing.T) {
				a, _, _ := testApp(t)
				a.s.Scheme = scheme.ID
				a.saved, a.seen = clone(a.s), clone(a.s)
				tt := ui.NewTester(a.view, 720, 480)
				if err := tt.Click(T("s.g.chart.open")); err != nil {
					t.Fatal(err)
				}
				before := chartTextWithin(t, tt, T("s.g.chart.close"), 720, 480)
				for _, key := range []string{"Q", "P", "A", ";", "Z", "M"} {
					chartTextWithin(t, tt, key, 720, 480)
				}
				tt.Scroll(360, 240, 0, 1000)
				chartTextWithin(t, tt, T("s.g.chart.note"), 720, 480)
				after := chartTextWithin(t, tt, T("s.g.chart.close"), 720, 480)
				if before != after {
					t.Error("close button scrolled with the content")
				}
				chartSnapshot(t, tt, "shuangpin-"+scheme.ID+"-small-"+language.name)
				if err := tt.Click(T("s.g.chart.close")); err != nil || a.chartShown {
					t.Fatalf("close small chart: %v", err)
				}
			})
		}
	}

	a, _, _ := testApp(t)
	tt := ui.NewTester(a.view, 880, 640)
	if tt.HasText(T("s.g.chart.open")) {
		t.Error("full pinyin offers a shuangpin chart")
	}
	a.chartShown = true
	tt.Frame()
	if a.chartShown {
		t.Error("switching to full pinyin keeps the chart open")
	}
}

func chartTextWithin(t *testing.T, tt *ui.Tester, text string, width, height float32) ui.Rect {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > width || r.Y+r.H > height {
		t.Fatalf("chart text %q outside %v×%v: %v, found %v", text, width, height, r, ok)
	}
	return r
}

func chartSnapshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if *snapshot == "" {
		return
	}
	if err := os.MkdirAll(*snapshot, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(*snapshot, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(f, tt.Image())
	closeErr := f.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
