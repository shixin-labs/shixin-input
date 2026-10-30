package client

import (
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"shixin/internal/policy"
)

// Auto spacing between Chinese and English or digits (DESIGN §7.1): the
// client tracks the classes of the characters left of the caret and inserts
// a space where CJK meets Latin or a digit. Anything it didn't see resets
// the history to unknown, and unknown never gets a space.

// TextReader is implemented by an Applier that can read the app's text.
// The spacer asks at most once per boundary, never per key.
type TextReader interface {
	// TextBefore returns the character before the caret, or before the
	// marked text if there is any; 0 at the start of the text. ok is
	// false if the app can't tell.
	TextBefore() (r rune, ok bool)
}

// class is a character's kind for spacing.
type class uint8

const (
	clUnknown class = iota
	clCJK
	clLatin
	clDigit
	clSpace
	clOther
)

const (
	spaceHistory = 16
	spaceIdle    = 30 * time.Second
	// A read the app can't answer, or two that are slow or stale, leave
	// the app's spacing to tracking alone until it loses focus.
	readBudget  = 2 * time.Millisecond
	readStrikes = 2
)

// Modifier keys alone move nothing; every other non-printable key but
// Backspace and Return resets the spacer. Command (Super) shortcuts are
// told apart by their mask.
const (
	keyModFirst = policy.KeyShiftL // Shift_L … Hyper_R
	keyModLast  = 0xffee
	keySuper    = policy.SuperMask
)

type spaceEntry struct {
	cl   class
	ours bool // a space the spacer inserted
}

// spacer is the auto-spacing state of one client. Main thread only, except
// for reset.
type spacer struct {
	on bool // the engine's ResultAutoSpace for the focused context

	// hist is a ring of the last classes left of the caret; hist[top] is
	// the character just before it. n == 0 means unknown.
	hist   [spaceHistory]spaceEntry
	top, n int
	// muted: the user deleted a space the spacer inserted, so the next
	// character gets none.
	muted bool
	// before is the class left of the caret before the last insertion,
	// to recognize an app answering from before it.
	before class
	last   time.Time // the last key

	reader     TextReader // nil: tracking only
	unreadable bool       // the reader failed or was slow, for this app
	strikes    int
	now        func() time.Time

	buf  []byte
	buf1 [1]byte // a repaired key

	// reset is set by ResetSpacing from any thread (a click, an app
	// switch) and applied before the next decision.
	reset atomic.Bool
}

func newSpacer(r TextReader) spacer {
	return spacer{reader: r, now: time.Now, buf: make([]byte, 0, 256)}
}

func (s *spacer) clear() { s.n, s.muted, s.before = 0, false, clUnknown }

func (s *spacer) push(cl class, ours bool) {
	s.top = (s.top + 1) % spaceHistory
	s.hist[s.top] = spaceEntry{cl, ours}
	if s.n < spaceHistory {
		s.n++
	}
}

func (s *spacer) pop() spaceEntry {
	if s.n == 0 {
		return spaceEntry{}
	}
	e := s.hist[s.top]
	s.top = (s.top + spaceHistory - 1) % spaceHistory
	s.n--
	return e
}

func (s *spacer) left() class {
	if s.n == 0 {
		return clUnknown
	}
	return s.hist[s.top].cl
}

// focus starts over in a new text field; app is its app.
func (s *spacer) focus(app, prev string) {
	s.clear()
	s.reset.Store(false)
	if app != prev {
		s.unreadable, s.strikes = false, 0
	}
}

// tick is called for every key press: it applies a pending reset and the
// idle timeout.
func (s *spacer) tick() {
	now := s.now()
	if s.reset.Swap(false) || now.Sub(s.last) > spaceIdle {
		s.clear()
	}
	s.last = now
}

// key handles a key that goes to the app. It returns the text to insert
// instead of passing the key (" a" after Chinese), or nil.
func (s *spacer) key(keysym, mods uint32) []byte {
	if mods&policy.ReleaseMask != 0 {
		return nil
	}
	if mods&policy.CommandMask != 0 {
		// Copy and save leave the text alone; anything else (paste, undo,
		// word moves, Emacs bindings, Option characters) may not.
		if mods&(policy.CommandMask|policy.ShiftMask) != keySuper || keysym != 'c' && keysym != 's' {
			s.clear()
		}
		return nil
	}
	var cl class
	switch {
	case keysym >= 0x20 && keysym < 0x7f || keysym >= 0xa0 && keysym < 0x100:
		cl = classOf(rune(keysym))
	case keysym == policy.KeyBackSpace:
		s.muted = s.pop().ours
		s.before = clUnknown
		return nil
	case keysym == policy.KeyReturn || keysym == policy.KeyKPEnter:
		// A new line, or a sent message: either way nothing on the left.
		s.clear()
		s.push(clSpace, false)
		return nil
	case keysym >= keyModFirst && keysym <= keyModLast:
		return nil
	default:
		// Arrows, Home/End, Page Up/Down, Tab, Escape, Forward Delete,
		// function keys, characters outside the keymap.
		s.clear()
		return nil
	}
	if s.on && (cl == clLatin || cl == clDigit) && s.space(cl) {
		s.before = s.left()
		s.push(clSpace, true)
		s.push(cl, false)
		s.buf = append(s.buf[:0], ' ')
		s.buf = utf8.AppendRune(s.buf, rune(keysym))
		return s.buf
	}
	s.muted = false
	s.before = s.left()
	s.push(cl, false)
	return nil
}

// commit returns the text to insert for a commit: text itself, or a space
// and text. The commit itself is taken as given.
func (s *spacer) commit(text []byte) []byte {
	if s.reset.Swap(false) {
		s.clear()
	}
	s.last = s.now() // a candidate clicked after a pause is activity too
	if len(text) == 0 {
		return text
	}
	out := text
	r, _ := utf8.DecodeRune(text)
	space := false
	if first := classOf(r); s.on && (first == clCJK || first == clLatin || first == clDigit) {
		space = s.space(first)
	}
	s.before = s.left()
	if space {
		s.buf = append(append(s.buf[:0], ' '), text...)
		s.push(clSpace, true)
		out = s.buf
	}
	s.muted = false
	s.pushText(text)
	return out
}

// space reports whether a space goes between the text left of the caret
// and a character of class next. It asks the app when the history is
// unknown, and before every space where the app can answer.
func (s *spacer) space(next class) bool {
	if s.muted {
		return false
	}
	left := s.left()
	if left == clUnknown {
		got, _, ok := s.read()
		if !ok {
			return false
		}
		s.push(got, false)
		return boundary(got, next)
	}
	if !boundary(left, next) {
		return false
	}
	got, start, ok := s.read()
	switch {
	case !ok || got == left:
		return true
	case start:
		// We saw text typed left of the caret since the last reset, so
		// the app reports a caret it doesn't have (Ghostty's selectedRange
		// is always {0, 0}). Stop asking it.
		s.unreadable = true
		return true
	case got == s.before:
		// The app answered from before our last insertion: Slack and
		// Google Docs apply insertText late. Believe the tracking, and stop
		// asking an app that does this repeatedly.
		s.strike()
		return true
	}
	// Something changed the text without us seeing it: skip this space
	// and believe the app from now on.
	s.clear()
	s.push(got, false)
	return false
}

// read asks the app for the class of the character left of the caret;
// start reports the start of the text.
func (s *spacer) read() (cl class, start, ok bool) {
	if s.reader == nil || s.unreadable {
		return clUnknown, false, false
	}
	t0 := s.now()
	r, ok := s.reader.TextBefore()
	if !ok {
		s.unreadable = true
		return clUnknown, false, false
	}
	if s.now().Sub(t0) > readBudget {
		s.strike()
	}
	if r == 0 {
		return clSpace, true, true
	}
	return classOf(r), false, true
}

// strike counts an unreliable answer; enough of them and the app's
// spacing is left to tracking alone until it loses focus.
func (s *spacer) strike() {
	if s.strikes++; s.strikes >= readStrikes {
		s.unreadable = true
	}
}

// pushText records the characters of inserted text.
func (s *spacer) pushText(text []byte) {
	joined := false
	var cl class
	for len(text) > 0 {
		r, size := utf8.DecodeRune(text)
		text = text[size:]
		if joins(r) {
			joined = true
		}
		cl = classOf(r)
		s.push(cl, false)
	}
	if joined {
		// Backspace deletes a whole emoji sequence or accented letter, not
		// one code point: keep only what's certain.
		s.clear()
		s.push(cl, false)
	}
}

// boundary reports whether a space goes between classes a and b.
func boundary(a, b class) bool {
	return a == clCJK && (b == clLatin || b == clDigit) || b == clCJK && (a == clLatin || a == clDigit)
}

var asciiClass = func() (t [0x80]class) {
	for r := range t {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			t[r] = clLatin
		case r >= '0' && r <= '9':
			t[r] = clDigit
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			t[r] = clSpace
		default:
			t[r] = clOther
		}
	}
	return t
}()

// classOf classifies a character (DESIGN §7.1, "What counts as a
// boundary").
func classOf(r rune) class {
	switch {
	case r < 0x80:
		if r < 0 {
			return clOther
		}
		return asciiClass[r]
	case r >= 0x4e00 && r <= 0x9fff, r >= 0x3400 && r <= 0x4dbf, r >= 0xac00 && r <= 0xd7af,
		r >= 0x3040 && r <= 0x30ff && r != 0x30fb: // kana, without the middle dot
		return clCJK
	case r == 0xa0 || r == 0x3000 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029:
		return clSpace
	case r >= 0xff00 && r <= 0xff65: // full-width forms
		return clOther
	case r < 0x530 && unicode.In(r, unicode.Latin, unicode.Greek, unicode.Cyrillic),
		r >= 0x1e00 && r <= 0x1fff && unicode.In(r, unicode.Latin, unicode.Greek):
		return clLatin
	case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo):
		return clCJK
	}
	return clOther
}

// joins reports whether r combines with the character before it, so
// Backspace removes both.
func joins(r rune) bool {
	switch {
	case r < 0x300:
		return false
	case r == 0x200d, r == 0x20e3, r >= 0xfe00 && r <= 0xfe0f, r >= 0x1f3fb && r <= 0x1f3ff,
		r >= 0x1f1e6 && r <= 0x1f1ff, r >= 0xe0020 && r <= 0xe007f:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me)
}
