package rerank

import (
	"strings"
	"testing"
)

func TestContextsBackspace(t *testing.T) {
	var c Contexts
	s := c.Slot("editor")
	c.Append(s, []byte("产品"))
	c.Append(s, []byte("hua"))
	for _, want := range []string{"产品hu", "产品h", "产品"} {
		rev := c.Rev(s)
		c.Backspace(s)
		if got := text(&c, s); got != want || c.Rev(s) == rev {
			t.Fatalf("Backspace = %q, want %q with new revision", got, want)
		}
	}
	// The last commit is exhausted. Any auto-spacing or earlier edits are
	// outside that known suffix, so don't guess where the next delete lands.
	c.Backspace(s)
	if text(&c, s) != "" {
		t.Fatal("unknown delete kept stale context")
	}
	c.Append(s, []byte("产品"))
	c.Backspace(s)
	if text(&c, s) != "产" {
		t.Fatal("Han Backspace did not remove one character")
	}
	c.Break(s)
	c.Backspace(s)
	if text(&c, s) != "" {
		t.Fatal("Backspace after caret boundary retained old text")
	}
}

func TestContextsBackspaceComplexAndWrapped(t *testing.T) {
	for _, suffix := range []string{"e\u0301", "👩‍💻", "🇨🇳", "\r\n"} {
		var c Contexts
		s := c.Slot("a")
		c.Append(s, []byte("产品"+suffix))
		c.Backspace(s)
		if text(&c, s) != "" {
			t.Fatalf("guessed a grapheme boundary for %q", suffix)
		}
	}
	var c Contexts
	s := c.Slot("a")
	c.Append(s, []byte(strings.Repeat("字", RingRunes+3)))
	for range RingRunes {
		c.Backspace(s)
	}
	if text(&c, s) != "" {
		t.Fatal("wrapped ring did not empty")
	}
	c.Backspace(s)
	if text(&c, s) != "" {
		t.Fatal("Backspace underflowed empty ring")
	}
}

func TestContextsEditDoesNotAllocate(t *testing.T) {
	var c Contexts
	s := c.Slot("a")
	commit := []byte("产品hua")
	if n := testing.AllocsPerRun(100, func() {
		c.Append(s, commit)
		c.Backspace(s)
		c.Backspace(s)
		c.AppendPassed(s, 'Q')
		c.Break(s)
		c.Clear(s)
	}); n != 0 {
		t.Fatalf("context editing allocated %g times", n)
	}
}
