// Package rerank holds the parts of context-aware reranking (智能排序,
// DESIGN §7.2) shared by shixin-engine and shixin-ai: the per-app context
// rings, and the policy that turns model scores into a candidate order.
//
// Nothing here allocates after construction, so the engine can use it on
// the key path.
package rerank

import "unicode/utf8"

const (
	// Rings is how many apps keep their context at once.
	Rings = 16
	// RingRunes is how much committed text each app keeps.
	RingRunes = 128
)

// Contexts are the committed text of the last Rings apps, one ring each
// (DESIGN §7.2, "Context"). Context belongs to the app, not to the input
// context: every field of an app shares its ring. The least recently
// focused app's ring is reused for a new app.
//
// Contexts are used by one goroutine (the engine thread).
type Contexts struct {
	rings [Rings]ring
	clock uint64
}

type ring struct {
	app    string
	used   uint64 // clock at the last Focus; 0: free
	rev    uint32
	n, end int // rune count; index after the newest rune
	buf    [RingRunes]rune
}

// Slot returns the ring of app, taking the least recently focused ring
// when app has none, and marks it focused. Slots are the only app
// identity that leaves the engine.
func (c *Contexts) Slot(app string) int {
	c.clock++
	old := 0
	for i := range c.rings {
		r := &c.rings[i]
		if r.used != 0 && r.app == app {
			r.used = c.clock
			return i
		}
		if r.used < c.rings[old].used {
			old = i
		}
	}
	r := &c.rings[old]
	rev := r.rev
	*r = ring{app: app, used: c.clock, rev: rev + 1}
	return old
}

// Append adds committed text to the slot's ring, dropping the oldest runes
// beyond RingRunes. Invalid UTF-8 is stored as U+FFFD.
func (c *Contexts) Append(slot int, text []byte) {
	if len(text) == 0 {
		return
	}
	r := &c.rings[slot]
	for len(text) > 0 {
		ch, size := utf8.DecodeRune(text)
		text = text[size:]
		r.push(ch)
	}
	r.rev++
}

// Break ends the slot's current line with a newline, which the model reads
// as a paragraph boundary: earlier text is kept, but what follows may not
// continue it. It is used when focus moves to another field of the app and
// when the caret moves. Breaks don't repeat, and an empty ring stays empty.
func (c *Contexts) Break(slot int) {
	r := &c.rings[slot]
	if r.n == 0 || r.buf[(r.end+RingRunes-1)%RingRunes] == '\n' {
		return
	}
	r.push('\n')
	r.rev++
}

func (r *ring) push(ch rune) {
	r.buf[r.end] = ch
	r.end = (r.end + 1) % RingRunes
	if r.n < RingRunes {
		r.n++
	}
}

// Rev is the slot's revision. It changes with every change to its text,
// and when the slot is given to another app.
func (c *Contexts) Rev(slot int) uint32 { return c.rings[slot].rev }

// Text appends the slot's text, oldest first, to dst as UTF-8.
func (c *Contexts) Text(dst []byte, slot int) []byte {
	r := &c.rings[slot]
	for i := r.end + RingRunes - r.n; i < r.end+RingRunes; i++ {
		dst = utf8.AppendRune(dst, r.buf[i%RingRunes])
	}
	return dst
}

// Reset forgets every app's text, as when 智能排序 is turned off.
// Revisions keep increasing so no old score can match.
func (c *Contexts) Reset() {
	for i := range c.rings {
		r := &c.rings[i]
		*r = ring{rev: r.rev + 1}
	}
}
