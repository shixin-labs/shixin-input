package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"shixin/internal/keymap"
)

func TestASCIIKeys(t *testing.T) {
	for ch := rune(32); ch < 127; ch++ {
		if _, ok := asciiKey(ch); !ok {
			t.Errorf("printable ASCII %q is unsupported", ch)
		}
	}
	for _, tc := range []struct {
		ch    rune
		code  uint16
		shift bool
	}{
		{'a', 0, false}, {'A', 0, true}, {'z', 6, false},
		{'>', 47, true}, {':', 41, true}, {'<', 43, true}, {'?', 44, true},
		{'+', 24, true}, {'"', 39, true}, {'\'', 39, false}, {'|', 42, true},
		{'~', 50, true}, {'!', 18, true}, {'@', 19, true},
		{' ', 49, false}, {'\n', 36, false}, {'\t', 48, false},
	} {
		got, ok := asciiKey(tc.ch)
		if !ok || got.code != tc.code || (got.mods&keymap.MacFlagShift != 0) != tc.shift {
			t.Errorf("%q: %+v, supported %v", tc.ch, got, ok)
		}
	}
	for _, ch := range []rune{0, '\r', 27, 127, '中', '😀'} {
		if _, ok := asciiKey(ch); ok {
			t.Errorf("accepted unsupported character %q", ch)
		}
	}
}

func TestChords(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  stroke
	}{
		{"cmd+a", stroke{0, keymap.MacFlagCommand}},
		{"Command+C", stroke{8, keymap.MacFlagCommand}},
		{"ctrl+option+delete", stroke{117, keymap.MacFlagControl | keymap.MacFlagOption}},
		{"shift+.", stroke{47, keymap.MacFlagShift}},
		{"cmd+plus", stroke{24, keymap.MacFlagCommand | keymap.MacFlagShift}},
		{"+", stroke{24, keymap.MacFlagShift}},
		{"shift", stroke{56, 0}},
		{"escape", stroke{53, 0}},
		{"pageup", stroke{116, 0}},
	} {
		got, err := chord(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("chord(%q) = %+v, %v; want %+v", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{"", "cmd+", "cmd++", "cmd+cmd+a", "shift+shift", "banana+a", "a+b", "中"} {
		if _, err := chord(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestModifierEvents(t *testing.T) {
	k, _ := chord("cmd+shift+a")
	want := []keyEvent{
		{56, true, keymap.MacFlagShift},
		{55, true, keymap.MacFlagShift | keymap.MacFlagCommand},
		{0, true, keymap.MacFlagShift | keymap.MacFlagCommand},
		{0, false, keymap.MacFlagShift | keymap.MacFlagCommand},
		{55, false, keymap.MacFlagShift},
		{56, false, 0},
	}
	if got := events(k); !reflect.DeepEqual(got, want) {
		t.Fatalf("events: %+v, want %+v", got, want)
	}
	for _, input := range []string{"a", "shift", "ctrl", "cmd+a", "ctrl+alt+shift+cmd+right", ">", ":"} {
		k, err := chord(input)
		if err != nil {
			t.Fatal(err)
		}
		pressed := map[uint16]bool{}
		events := events(k)
		for _, e := range events {
			if pressed[e.code] == e.down {
				t.Fatalf("%s: unmatched event %+v", input, e)
			}
			pressed[e.code] = e.down
		}
		for code, down := range pressed {
			if down {
				t.Errorf("%s leaves key %d pressed", input, code)
			}
		}
		if last := events[len(events)-1]; last.flags != 0 {
			t.Errorf("%s leaves modifiers %#x", input, last.flags)
		}
	}
}

func TestParseAndExecute(t *testing.T) {
	actions, err := parse([]string{"type", ">:\n", "wait", "0", "key", "cmd+a", "key", "cmd+c", "expect", "中文\n"})
	if err != nil {
		t.Fatal(err)
	}
	var pressed []stroke
	reads := 0
	err = execute(actions, func(k stroke) error { pressed = append(pressed, k); return nil }, func() (string, error) {
		reads++
		if reads == 1 {
			return "stale", nil
		}
		return "中文\n", nil
	}, 0, time.Second)
	if err != nil || len(pressed) != 5 || reads != 2 {
		t.Fatalf("pressed %v, reads %d, err %v", pressed, reads, err)
	}
	want := []stroke{{47, keymap.MacFlagShift}, {41, keymap.MacFlagShift}, {36, 0}, {0, keymap.MacFlagCommand}, {8, keymap.MacFlagCommand}}
	if !reflect.DeepEqual(pressed, want) {
		t.Errorf("pressed %+v, want %+v", pressed, want)
	}
	// Failed parsing discards even an otherwise valid prefix.
	for _, args := range [][]string{nil, {"key"}, {"type", "okay", "type", "中"}, {"wait", "-1s"}, {"wait", "soon"}, {"tap", "a"}} {
		if got, err := parse(args); err == nil || got != nil {
			t.Errorf("invalid sequence %q produced %+v, %v", args, got, err)
		}
	}
}

func TestExecutionFailure(t *testing.T) {
	actions, _ := parse([]string{"key", "a", "key", "b"})
	want := errors.New("post failed")
	posts := 0
	if err := execute(actions, func(stroke) error { posts++; return want }, nil, 0, 0); !errors.Is(err, want) || posts != 1 {
		t.Fatalf("posting failure: %v, posts %d", err, posts)
	}
	actions, _ = parse([]string{"expect", "private expected text", "key", "a"})
	posts = 0
	err := execute(actions, func(stroke) error { posts++; return nil }, func() (string, error) { return "private actual text", nil }, 0, 0)
	if err == nil || strings.Contains(err.Error(), "private") || posts != 0 {
		t.Fatalf("clipboard mismatch: %v, posts %d", err, posts)
	}
	if err := execute(actions, nil, func() (string, error) { return "", want }, 0, 0); !errors.Is(err, want) {
		t.Fatalf("clipboard failure: %v", err)
	}
}
