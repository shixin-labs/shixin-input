package main

// caretTracker keeps the caret the candidate window is placed at. The app
// is asked once per composition; an unusable answer (usableCaret) keeps
// the last usable caret, or the fallback before there is one, and is asked
// again shortly, a few times: Qt apps such as 企业微信 answer the zero
// rectangle until their event loop has laid out the marked text.
type caretTracker struct {
	caret   candidateRect
	valid   bool // the caret was read for this composition
	known   bool // a usable caret has been read
	retries int
}

// maxCaretRetries bounds how often one composition asks again, at
// caretRetryDelay apart: about a tenth of a second in all.
const (
	maxCaretRetries = 6
	caretRetryDelay = 16 // milliseconds, a frame
)

// reset starts a composition, or a new text field.
func (t *caretTracker) reset() { t.valid, t.retries = false, 0 }

// read records the app's answer and reports whether to ask again later.
func (t *caretTracker) read(answer candidateRect, screens []candidateScreen) (retry bool) {
	if usableCaret(answer, screens) {
		t.caret, t.valid, t.known = answer, true, true
		return false
	}
	if !t.known {
		t.caret = fallbackCaret(screens)
	}
	t.retries++
	return t.retries <= maxCaretRetries
}
