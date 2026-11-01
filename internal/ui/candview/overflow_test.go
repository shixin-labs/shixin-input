package candview

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"shixin/internal/ui/skin"
)

func TestOverflowLongTextAndComment(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		st := &State{Preedit: strings.Repeat("long preedit ", 30), Items: []Item{
			{Label: "1", Text: strings.Repeat("候选", 60), Comment: strings.Repeat("comment ", 30)},
		}}
		style := &Style{Skin: skin.Get("shixin"), FontSize: 28, Vertical: vertical, MaxWidth: 240, MaxHeight: 150}
		picked := -1
		tt := ui.NewTester(func(c *ui.Context) { Draw(c, st, style, func(i int) { picked = i }).Label("panel") }, 400, 250)
		card, _ := tt.Find("panel")
		x, y := card.X+card.W/2, card.Y+card.H/2
		tt.Scroll(x, y, 100000, 0)
		tt.Frame()
		if st.scroll.MaxX <= 0 || st.scroll.X != st.scroll.MaxX {
			t.Fatalf("cannot scroll to the end of the long comment: %+v", st.scroll)
		}
		tt.Scroll(x, y, -100000, 0)
		tt.Frame()
		text, ok := tt.Find(st.Items[0].Text)
		if !ok || text.X < card.X || text.X >= card.X+card.W {
			t.Fatalf("candidate start unreachable: %+v in %+v", text, card)
		}
		tt.ClickAt(text.X+5, text.Y+text.H/2)
		if picked != 0 {
			t.Fatal("visible part of a long candidate is not selectable")
		}
	}
}

func TestOverflowAppearanceRevealsSelection(t *testing.T) {
	st := &State{Highlight: 9}
	for i := range 10 {
		st.Items = append(st.Items, Item{Label: Labels[i], Text: fmt.Sprintf("%s%d", strings.Repeat("候选", 20), i)})
	}
	style := &Style{Skin: skin.Get("shixin"), FontSize: 17, Vertical: true, MaxWidth: 120, MaxHeight: 60}
	tt := ui.NewTester(func(c *ui.Context) { Draw(c, st, style, nil).Label("panel") }, 400, 250)
	for _, change := range []func(){
		func() { style.FontSize = 28 },
		func() { style.FontFamily = "monospace" },
		func() { style.Vertical = false },
	} {
		tt.Scroll(60, 30, -100000, -100000)
		change()
		tt.Frame()
		selected, ok := tt.Find(st.Items[9].Text)
		if !ok || selected.W <= 0 || selected.H <= 0 {
			t.Fatal("appearance change left the keyboard selection outside the capped viewport")
		}
	}
}

func TestOverflowKeepsSelectionVisible(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		t.Run(fmt.Sprint(vertical), func(t *testing.T) {
			st := &State{Highlight: 9}
			for i := range 10 {
				st.Items = append(st.Items, Item{Label: Labels[i], Text: fmt.Sprintf("候选词%d", i)})
			}
			style := &Style{Skin: skin.Get("shixin"), FontSize: 32, Vertical: vertical, MaxWidth: 240, MaxHeight: 120}
			picked := -1
			selectCandidate := func(i int) { picked = i }
			view := func(c *ui.Context) { Draw(c, st, style, selectCandidate).Label("panel") }
			tt := ui.NewTester(view, 400, 250)
			card, ok := tt.Find("panel")
			if !ok || card.W > 240.1 || card.H > 120.1 {
				t.Fatalf("unbounded card: %+v", card)
			}
			last, ok := tt.Find("候选词9")
			if !ok || last.X < card.X || last.Y < card.Y || last.X+last.W > card.X+card.W || last.Y+last.H > card.Y+card.H {
				t.Fatalf("selected candidate outside card: %+v %+v", last, card)
			}
			if err := tt.Click("候选词9"); err != nil || picked != 9 {
				t.Fatalf("last candidate not clickable: %d %v", picked, err)
			}
			for range 10 {
				tt.Frame()
			}
			if n := testing.AllocsPerRun(20, tt.Frame); n != 0 {
				t.Fatalf("warmed overflow frame allocates %v times", n)
			}
			tt.Scroll(card.X+card.W/2, card.Y+card.H/2, -10000, -10000)
			tt.Frame() // Idle frames must not pull manual scrolling back to Highlight.
			first, ok := tt.Find("候选词0")
			if !ok || first.X < card.X || first.Y < card.Y || first.X+first.W > card.X+card.W || first.Y+first.H > card.Y+card.H {
				t.Fatalf("cannot scroll back to first candidate: %+v", first)
			}
			if err := tt.Click("候选词0"); err != nil || picked != 0 {
				t.Fatalf("scrolled candidate not clickable: %d %v", picked, err)
			}
			// Moving to a smaller screen or changing appearance must reveal
			// the keyboard selection even when no engine result has arrived.
			style.MaxWidth, style.MaxHeight = 200, 100
			tt.Frame()
			card, _ = tt.Find("panel")
			last, _ = tt.Find("候选词9")
			if last.X < card.X || last.Y < card.Y || last.X+last.W > card.X+card.W || last.Y+last.H > card.Y+card.H {
				t.Fatalf("viewport resize hid selection: %+v %+v", last, card)
			}
			if *snapshot != "" {
				st.viewportSeen = false // The renderer creates a fresh scroll viewport.
				f, err := os.Create(filepath.Join(*snapshot, fmt.Sprintf("overflow-%v.png", vertical)))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, ui.Render(view, 400, 250, 2)); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
		})
	}
}
