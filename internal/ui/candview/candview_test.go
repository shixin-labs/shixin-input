package candview

import (
	"flag"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"shixin/internal/ui/fonts"
	"shixin/internal/ui/skin"
)

func TestMeasure(t *testing.T) {
	if runtime.GOOS != "darwin" {
		// Only the macOS front places the window from Measure so far.
		t.Skip("Measure doesn't match the layout with Linux font fallback (KNOWN-ISSUES)")
	}
	families := append([]string{""}, fonts.Families()...)
	for _, family := range families {
		for _, vertical := range []bool{false, true} {
			for _, size := range []float32{10, 17, 32} {
				for _, preedit := range []string{"", "shi xin", "a much longer preedit than the candidates"} {
					st := sample()
					st.Preedit = preedit
					style := &Style{Skin: skin.Get("shixin"), FontFamily: family, FontSize: size, Vertical: vertical}
					var w, h float32
					tt := ui.NewTester(func(c *ui.Context) {
						w, h = Measure(c, st, style)
						if n := testing.AllocsPerRun(20, func() { Measure(c, st, style) }); n != 0 {
							t.Errorf("cached measurement allocates %v times", n)
						}
						Draw(c, st, style, nil).Label("panel")
					}, 1400, 1024)
					bounds, ok := tt.Find("panel")
					if !ok {
						t.Fatal("panel missing")
					}
					if math.Abs(float64(w-bounds.W)) > 1 || math.Abs(float64(h-bounds.H)) > 1 {
						t.Errorf("font %q vertical %v size %v preedit %q: measured %v×%v, layout %v", family, vertical, size, preedit, w, h, bounds)
					}
				}
			}
		}
	}
}

var snapshot = flag.String("snapshot", "", "write rendered panels to this directory")

func sample() *State {
	return &State{
		Preedit:   "shi xin",
		Highlight: 0,
		Items: []Item{
			{"1", "拾新", ""}, {"2", "实心", ""}, {"3", "是", ""}, {"4", "时", ""}, {"5", "🆕", "emoji"},
		},
	}
}

func TestDrawAndSelect(t *testing.T) {
	var picked = -1
	st := sample()
	style := &Style{Skin: skin.Get("shixin"), FontSize: 17}
	tt := ui.NewTester(func(c *ui.Context) { Draw(c, st, style, func(i int) { picked = i }) }, 600, 200)
	if !tt.HasText("拾新") || !tt.HasText("shi xin") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("实心"); err != nil {
		t.Fatal(err)
	}
	if picked != 1 {
		t.Errorf("picked %d", picked)
	}
}

func TestSnapshots(t *testing.T) {
	if *snapshot == "" {
		t.Skip("-snapshot DIR renders every skin")
	}
	for _, s := range skin.All {
		for _, dark := range []bool{false, true} {
			for _, vertical := range []bool{false, true} {
				style := &Style{Skin: &s, Dark: dark, FontSize: 17, Vertical: vertical}
				img := ui.Render(func(c *ui.Context) {
					c.Root().Background(ui.Hex("#808080")).Padding(16)
					Draw(c, sample(), style, nil)
				}, 460, 260, 2)
				name := s.ID
				if dark {
					name += "-dark"
				}
				if vertical {
					name += "-vertical"
				}
				f, err := os.Create(filepath.Join(*snapshot, name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				png.Encode(f, img)
				f.Close()
			}
		}
	}
}

func BenchmarkCandidateFrame(b *testing.B) {
	for _, family := range []string{"", "Songti SC", "Menlo"} {
		for _, vertical := range []bool{false, true} {
			name := "horizontal"
			if vertical {
				name = "vertical"
			}
			b.Run(family+"/"+name, func(b *testing.B) {
				st := sample()
				style := &Style{Skin: skin.Get("shixin"), FontFamily: family, FontSize: 17, Vertical: vertical}
				selectCandidate := func(int) {}
				tt := ui.NewTester(func(c *ui.Context) {
					Measure(c, st, style)
					Draw(c, st, style, selectCandidate)
				}, 600, 240)
				for range 10 {
					tt.Frame()
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					st.Highlight = i % len(st.Items)
					tt.Frame()
				}
			})
		}
	}
}

func TestMeasurementCache(t *testing.T) {
	st := sample()
	style := &Style{Skin: skin.Get("shixin"), FontSize: 17}
	var w, h float32
	tt := ui.NewTester(func(c *ui.Context) {
		w, h = Measure(c, st, style)
		Draw(c, st, style, nil).Label("panel")
	}, 1400, 1024)
	for _, family := range []string{"serif", "monospace", "", "serif"} {
		style.FontFamily = family
		tt.Frame()
		bounds, _ := tt.Find("panel")
		if runtime.GOOS == "darwin" && (math.Abs(float64(w-bounds.W)) > 1 || math.Abs(float64(h-bounds.H)) > 1) {
			t.Fatalf("switch to %q: measured %v×%v, layout %v", family, w, h, bounds)
		}
	}
	for i := range 600 {
		st.Preedit = strconv.Itoa(i)
		tt.Frame()
		if len(st.measured) > stringCacheEntries || st.measureBytes > stringCacheBytes {
			t.Fatal("measurement cache grew past its limits")
		}
	}
	for i := range 10 {
		st.Preedit = strings.Repeat(strconv.Itoa(i), 32<<10)
		tt.Frame()
		if st.measureBytes > stringCacheBytes {
			t.Fatal("measurement cache retains too many text bytes")
		}
	}
}
