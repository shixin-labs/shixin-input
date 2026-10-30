package client

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"shixin/internal/policy"
)

// fakeApp is a text field: runes and a caret. It answers TextBefore like a
// real app, or like a misbehaving one, and its clock advances only when
// told.
type fakeApp struct {
	text   []rune
	caret  int
	noread bool
	// stale answers from before the last insertion, as an app applying
	// insertText late would (Slack, Google Docs).
	stale bool
	// fakeCaret always reports the start of the text (Ghostty).
	fakeCaret bool
	slow      time.Duration // each read advances the clock by this much
	reads     int
	clock     time.Time

	prevText  []rune // the text before the last change, for stale
	prevCaret int
}

func (f *fakeApp) TextBefore() (rune, bool) {
	f.reads++
	f.clock = f.clock.Add(f.slow)
	text, caret := f.text, f.caret
	if f.stale {
		text, caret = f.prevText, f.prevCaret
	}
	switch {
	case f.noread:
		return 0, false
	case f.fakeCaret || caret == 0:
		return 0, true
	}
	return text[caret-1], true
}

// settle makes the current text what a stale app reports.
func (f *fakeApp) settle() {
	f.prevText, f.prevCaret = append(f.prevText[:0], f.text...), f.caret
}

func (f *fakeApp) insert(s string) {
	f.settle()
	r := []rune(s)
	f.text = append(f.text[:f.caret], append(r, f.text[f.caret:]...)...)
	f.caret += len(r)
}

// backspace deletes one character as users see it: a base and the code
// points joined to it.
func (f *fakeApp) backspace() {
	f.settle()
	end := f.caret
	for f.caret > 0 && joins(f.text[f.caret-1]) {
		f.caret--
	}
	if f.caret > 0 {
		f.caret--
		if f.caret > 0 && f.text[f.caret] >= 0x1f3fb && f.text[f.caret] <= 0x1f3ff {
			f.caret-- // a skin tone and its emoji
		}
	}
	f.text = append(f.text[:f.caret], f.text[end:]...)
}

func (f *fakeApp) String() string {
	return strings.NewReplacer("\n", "⏎", "\t", "⇥").Replace(string(f.text))
}

func newTestSpacer(app *fakeApp) *spacer {
	s := newSpacer(app)
	s.now = func() time.Time { return app.clock }
	s.on = true
	return &s
}

// press types one key into s and app, as the client does for a key that
// goes to the app: insert is what the app inserts if the key passes.
func press(s *spacer, app *fakeApp, keysym, mods uint32, insert string) {
	s.tick()
	if t := s.key(keysym, mods); t != nil {
		app.insert(string(t))
		return
	}
	app.insert(insert)
}

// runVector plays one line of testdata/spacing.txt.
func runVector(t *testing.T, events string) string {
	app := &fakeApp{clock: time.Unix(1e9, 0)}
	s := newTestSpacer(app)
	for _, ev := range strings.Split(events, "; ") {
		name, arg, _ := strings.Cut(ev, " ")
		n := 1
		if name == "bs" && arg != "" {
			n, _ = strconv.Atoi(arg)
		}
		app.clock = app.clock.Add(100 * time.Millisecond)
		switch name {
		case "commit":
			app.insert(string(s.commit([]byte(arg))))
		case "key":
			for _, r := range arg {
				press(s, app, uint32(r), 0, string(r))
			}
		case "bs":
			for range n {
				press(s, app, policy.KeyBackSpace, 0, "")
				app.backspace()
			}
		case "return":
			press(s, app, policy.KeyReturn, 0, "\n")
		case "tab":
			press(s, app, 0xff09, 0, "\t")
		case "esc":
			press(s, app, 0xff1b, 0, "")
		case "del":
			press(s, app, 0xffff, 0, "")
		case "left":
			press(s, app, 0xff51, 0, "")
			app.caret = max(app.caret-1, 0)
		case "right":
			press(s, app, 0xff53, 0, "")
			app.caret = min(app.caret+1, len(app.text))
		case "cmd":
			ins := ""
			if arg == "v" {
				ins = "P"
			}
			press(s, app, uint32(arg[0]), policy.SuperMask, ins)
		case "void":
			press(s, app, 0xffffff, 0, arg)
		case "click":
			s.reset.Store(true)
			if arg != "" {
				app.caret, _ = strconv.Atoi(arg)
			}
			app.settle()
		case "switch":
			s.reset.Store(true)
		case "focus":
			s.focus("app", "app")
		case "idle":
			app.clock = app.clock.Add(31 * time.Second)
		case "text": // long enough ago that even a stale app reports it
			app.insert(arg)
			app.settle()
		case "noread":
			app.noread = true
		case "stale":
			app.stale = true
		case "fakecaret":
			app.fakeCaret = true
		case "on", "off":
			s.on = name == "on"
		default:
			t.Fatalf("unknown event %q", ev)
		}
	}
	return app.String()
}

func TestSpacingVectors(t *testing.T) {
	f, err := os.Open("testdata/spacing.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for line := 1; sc.Scan(); line++ {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		events, want, ok := strings.Cut(l, " => ")
		if !ok {
			t.Fatalf("line %d: no =>", line)
		}
		if got := runVector(t, events); got != want {
			t.Errorf("line %d: %s\n got %q\nwant %q", line, events, got, want)
		}
		n++
	}
	if n < 50 {
		t.Fatalf("only %d vectors", n)
	}
}

func TestClassOf(t *testing.T) {
	for _, c := range []struct {
		s  string
		cl class
	}{
		{"中文〇々㐀𠀀豈", clCJK},
		{"あア한ㄅ", clCJK},
		{"aZéßαЖ", clLatin},
		{"09", clDigit},
		{" \t\n 　", clSpace},
		{"，。、！.,!?()（）「」😀×÷・０Ａ€%", clOther},
	} {
		for _, r := range c.s {
			if got := classOf(r); got != c.cl {
				t.Errorf("classOf(%q) = %d, want %d", r, got, c.cl)
			}
		}
	}
}

func TestBoundaryTable(t *testing.T) {
	classes := []class{clUnknown, clCJK, clLatin, clDigit, clSpace, clOther}
	for _, a := range classes {
		for _, b := range classes {
			want := a == clCJK && (b == clLatin || b == clDigit) || b == clCJK && (a == clLatin || a == clDigit)
			if boundary(a, b) != want {
				t.Errorf("boundary(%d, %d) = %v", a, b, !want)
			}
		}
	}
}

func TestSlowReaderFallsBackToTracking(t *testing.T) {
	app := &fakeApp{clock: time.Unix(1e9, 0), slow: 3 * time.Millisecond}
	s := newTestSpacer(app)
	app.insert("iPhone")
	for i := 0; i < 2; i++ {
		s.reset.Store(true)
		app.insert(string(s.commit([]byte("好"))))
	}
	if !s.unreadable || app.reads != 2 {
		t.Fatalf("after two slow reads: unreadable %v, %d reads", s.unreadable, app.reads)
	}
	s.reset.Store(true)
	s.commit([]byte("x"))
	if app.reads != 2 {
		t.Fatal("read an unreadable app")
	}
	// A new app is tried again; the same app after a focus change isn't.
	s.focus("app", "app")
	if !s.unreadable {
		t.Fatal("focus in the same app forgot")
	}
	s.focus("other", "app")
	if s.unreadable {
		t.Fatal("a new app is still unreadable")
	}
}

func TestOneSlowReadIsForgiven(t *testing.T) {
	app := &fakeApp{clock: time.Unix(1e9, 0), slow: 3 * time.Millisecond}
	s := newTestSpacer(app)
	s.commit([]byte("中"))
	if s.unreadable {
		t.Fatal("one slow read made the app unreadable")
	}
}

func TestReaderFailureMarksUnreadable(t *testing.T) {
	app := &fakeApp{clock: time.Unix(1e9, 0), noread: true}
	s := newTestSpacer(app)
	s.commit([]byte("中"))
	s.reset.Store(true)
	s.commit([]byte("中"))
	if app.reads != 1 {
		t.Fatalf("%d reads of an app that can't answer", app.reads)
	}
}

func TestNoReadsWhileOff(t *testing.T) {
	app := &fakeApp{clock: time.Unix(1e9, 0)}
	s := newTestSpacer(app)
	s.on = false
	s.commit([]byte("中文"))
	s.reset.Store(true)
	press(s, app, 'a', 0, "a")
	s.commit([]byte("好"))
	if app.reads != 0 {
		t.Fatalf("%d reads with spacing off", app.reads)
	}
}

func TestSpacerDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	app := &fakeApp{clock: time.Unix(1e9, 0), text: make([]rune, 0, 1<<16)}
	s := newTestSpacer(app)
	zh, en := []byte("中文"), []byte("hello")
	run := func() {
		s.tick()
		s.commit(zh)
		s.tick()
		s.key('a', 0)
		s.key('b', 0)
		s.key(policy.KeyBackSpace, 0)
		s.commit(en)
		s.commit(zh)
		s.reset.Store(true)
		s.commit(zh)
		s.key(0xff51, 0)
		s.key(policy.KeyReturn, 0)
	}
	run()
	runtime.GC()
	if n := testing.AllocsPerRun(1000, run); n != 0 {
		t.Fatalf("%v allocations", n)
	}
}

func TestStaleReaderFallsBackToTracking(t *testing.T) {
	app := &fakeApp{clock: time.Unix(1e9, 0), stale: true}
	s := newTestSpacer(app)
	app.insert("你好")
	app.settle()
	for i, text := range []string{"中文", "ni", "里", "ta", "很好"} {
		app.insert(string(s.commit([]byte(text))))
		if want := i >= 3; s.unreadable != want {
			t.Fatalf("after %q: unreadable %v", text, s.unreadable)
		}
	}
	if got := app.String(); got != "你好中文 ni 里 ta 很好" {
		t.Fatalf("text %q", got)
	}
}
