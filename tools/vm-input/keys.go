package main

import (
	"errors"
	"fmt"
	"strings"

	"shixin/internal/keymap"
)

type stroke struct {
	code uint16
	mods uint64
}

// Physical ANSI/US keys. Shifted text must use the same key code with Shift,
// rather than a Unicode insertion that could bypass the input method.
var asciiCodes = map[rune]uint16{
	'a': 0, 's': 1, 'd': 2, 'f': 3, 'h': 4, 'g': 5, 'z': 6, 'x': 7,
	'c': 8, 'v': 9, 'b': 11, 'q': 12, 'w': 13, 'e': 14, 'r': 15,
	'y': 16, 't': 17, '1': 18, '2': 19, '3': 20, '4': 21, '6': 22,
	'5': 23, '=': 24, '9': 25, '7': 26, '-': 27, '8': 28, '0': 29,
	']': 30, 'o': 31, 'u': 32, '[': 33, 'i': 34, 'p': 35, '\n': 36,
	'l': 37, 'j': 38, '\'': 39, 'k': 40, ';': 41, '\\': 42, ',': 43,
	'/': 44, 'n': 45, 'm': 46, '.': 47, '\t': 48, ' ': 49, '`': 50,
}

func asciiKey(ch rune) (stroke, bool) {
	var mods uint64
	if ch >= 'A' && ch <= 'Z' {
		ch += 'a' - 'A'
		mods = keymap.MacFlagShift
	} else if i := strings.IndexRune("~!@#$%^&*()_+{}|:\"<>?", ch); i >= 0 {
		ch = rune("`1234567890-=[]\\;',./"[i])
		mods = keymap.MacFlagShift
	}
	code, ok := asciiCodes[ch]
	return stroke{code: code, mods: mods}, ok
}

var namedKeys = map[string]uint16{
	"return": 36, "enter": 36, "tab": 48, "space": 49, "backspace": 51,
	"esc": 53, "escape": 53, "cmd": 55, "command": 55, "shift": 56,
	"option": 58, "alt": 58, "ctrl": 59, "control": 59,
	"home": 115, "pageup": 116, "delete": 117, "end": 119, "pagedown": 121,
	"left": 123, "right": 124, "down": 125, "up": 126,
}

var errUnknownKey = errors.New("unknown key; use US ASCII or a named key (use 'plus' in chords)")

var modifiers = []struct {
	code uint16
	flag uint64
}{
	{59, keymap.MacFlagControl},
	{58, keymap.MacFlagOption},
	{56, keymap.MacFlagShift},
	{55, keymap.MacFlagCommand},
}

func modifierFlag(code uint16) uint64 {
	for _, m := range modifiers {
		if m.code == code {
			return m.flag
		}
	}
	return 0
}

func chord(s string) (stroke, error) {
	if len(s) == 1 {
		if k, ok := asciiKey(rune(s[0])); ok {
			return k, nil
		}
	}
	parts := strings.Split(s, "+")
	var mods uint64
	for _, part := range parts[:len(parts)-1] {
		code, ok := namedKeys[strings.ToLower(part)]
		flag := modifierFlag(code)
		if !ok || flag == 0 || mods&flag != 0 {
			return stroke{}, fmt.Errorf("invalid or duplicate modifier %q", part)
		}
		mods |= flag
	}
	last := parts[len(parts)-1]
	var k stroke
	if last == "plus" {
		k, _ = asciiKey('+')
	} else if code, ok := namedKeys[strings.ToLower(last)]; ok {
		k.code = code
		if mods&modifierFlag(code) != 0 {
			return stroke{}, fmt.Errorf("duplicate modifier key %q", last)
		}
	} else if len(last) == 1 {
		var ok bool
		if len(parts) > 1 {
			last = strings.ToLower(last) // Cmd+A means cmd+a; add Shift explicitly.
		}
		k, ok = asciiKey(rune(last[0]))
		if !ok {
			return stroke{}, errUnknownKey
		}
	} else {
		return stroke{}, errUnknownKey
	}
	k.mods |= mods
	return k, nil
}

type keyEvent struct {
	code  uint16
	down  bool
	flags uint64
}

// events presses modifiers, taps the key, then releases modifiers in reverse
// order. Every stroke ends with the synthetic modifier state cleared.
func events(k stroke) []keyEvent {
	var out []keyEvent
	var flags uint64
	for _, m := range modifiers {
		if k.mods&m.flag != 0 {
			flags |= m.flag
			out = append(out, keyEvent{m.code, true, flags})
		}
	}
	out = append(out, keyEvent{k.code, true, flags | modifierFlag(k.code)}, keyEvent{k.code, false, flags})
	for i := len(modifiers) - 1; i >= 0; i-- {
		m := modifiers[i]
		if k.mods&m.flag != 0 {
			flags &^= m.flag
			out = append(out, keyEvent{m.code, false, flags})
		}
	}
	return out
}
