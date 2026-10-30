package modeview

import (
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
	"shixin/internal/ui/skin"
)

var snapshot = flag.String("snapshot", "", "write mode bubbles in every skin, language and appearance")

func TestModeBubble(t *testing.T) {
	old := i18n.Current()
	defer i18n.SetLang(old)
	if *snapshot != "" {
		if err := os.MkdirAll(*snapshot, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, lang := range []i18n.Lang{i18n.Chinese, i18n.English} {
		i18n.SetLang(lang)
		for _, s := range skin.All {
			for _, dark := range []bool{false, true} {
				for _, ascii := range []bool{false, true} {
					tt := ui.NewTester(func(c *ui.Context) { Draw(c, ascii, s.Palette(dark)) }, Extent, Extent)
					label := i18n.T("mode.chinese")
					mode := "chinese"
					if ascii {
						label, mode = i18n.T("mode.english"), "english"
					}
					if !tt.HasText(label) {
						t.Fatalf("skin %s dark %v ascii %v: missing %q", s.ID, dark, ascii, label)
					}
					box, ok := tt.Find(i18n.T("mode.indicator"))
					if !ok || box.X != Margin || box.Y != Margin || box.W != Size || box.H != Size {
						t.Fatalf("bubble geometry %v, found %v", box, ok)
					}
					if *snapshot != "" {
						name := s.ID + "-" + mode
						if dark {
							name += "-dark"
						}
						if lang == i18n.English {
							name += "-en"
						}
						f, err := os.Create(filepath.Join(*snapshot, name+".png"))
						if err != nil {
							t.Fatal(err)
						}
						err = png.Encode(f, tt.Image())
						closeErr := f.Close()
						if err != nil || closeErr != nil {
							t.Fatalf("snapshot: encode %v, close %v", err, closeErr)
						}
					}
				}
			}
		}
	}
}
