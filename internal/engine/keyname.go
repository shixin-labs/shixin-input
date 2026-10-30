package engine

import (
	"strings"

	"shixin/internal/policy"
)

// keyNames maps Rime/X11 key names to keysyms. Letters and digits are
// handled separately.
var keyNames = map[string]uint32{
	"space": ' ', "exclam": '!', "quotedbl": '"', "numbersign": '#',
	"dollar": '$', "percent": '%', "ampersand": '&', "apostrophe": '\'',
	"parenleft": '(', "parenright": ')', "asterisk": '*', "plus": '+',
	"comma": ',', "minus": '-', "period": '.', "slash": '/',
	"colon": ':', "semicolon": ';', "less": '<', "equal": '=',
	"greater": '>', "question": '?', "at": '@', "bracketleft": '[',
	"backslash": '\\', "bracketright": ']', "asciicircum": '^',
	"underscore": '_', "grave": '`', "braceleft": '{', "bar": '|',
	"braceright": '}', "asciitilde": '~',

	"BackSpace": policy.KeyBackSpace, "Tab": policy.KeyTab, "ISO_Left_Tab": 0xfe20,
	"Return": policy.KeyReturn, "Escape": policy.KeyEscape, "Delete": policy.KeyDelete,
	"Home": policy.KeyHome, "Left": policy.KeyLeft, "Up": policy.KeyUp, "Right": policy.KeyRight,
	"Down": policy.KeyDown, "Prior": policy.KeyPrior, "Page_Up": policy.KeyPrior, "Next": policy.KeyNext,
	"Page_Down": policy.KeyNext, "End": policy.KeyEnd, "Insert": 0xff63, "KP_Enter": policy.KeyKPEnter,
	"Caps_Lock": 0xffe5, "Shift_L": policy.KeyShiftL, "Shift_R": policy.KeyShiftR,
	"Control_L": policy.KeyControlL, "Control_R": policy.KeyControlR,
	"F1": 0xffbe, "F2": 0xffbf, "F3": 0xffc0, "F4": 0xffc1, "F5": 0xffc2,
	"F6": 0xffc3, "F7": 0xffc4, "F8": 0xffc5, "F9": 0xffc6, "F10": 0xffc7,
	"F11": 0xffc8, "F12": 0xffc9,
}

var modNames = map[string]uint32{
	"Shift": policy.ShiftMask, "Lock": policy.LockMask,
	"Control": policy.ControlMask, "Alt": policy.AltMask,
	"Super": policy.SuperMask, "Release": policy.ReleaseMask,
}

// parseKey parses a Rime key representation such as "Control+Shift+grave",
// "F4" or "a". ok is false for names it doesn't know.
func parseKey(repr string) (keysym, mods uint32, ok bool) {
	if strings.HasSuffix(repr, "++") { // "Control++"
		repr = strings.TrimSuffix(repr, "+") + "plus"
	}
	parts := strings.Split(repr, "+")
	name := parts[len(parts)-1]
	for _, m := range parts[:len(parts)-1] {
		bit, known := modNames[m]
		if !known {
			return 0, 0, false
		}
		mods |= bit
	}
	if len(name) == 1 && name[0] > 0x20 && name[0] < 0x7f {
		return uint32(name[0]), mods, true
	}
	if k, known := keyNames[name]; known {
		return k, mods, true
	}
	if strings.HasPrefix(name, "0x") || strings.HasPrefix(name, "0X") {
		var v uint32
		for _, c := range name[2:] {
			switch {
			case c >= '0' && c <= '9':
				v = v<<4 | uint32(c-'0')
			case c >= 'a' && c <= 'f':
				v = v<<4 | uint32(c-'a'+10)
			case c >= 'A' && c <= 'F':
				v = v<<4 | uint32(c-'A'+10)
			default:
				return 0, 0, false
			}
		}
		return v, mods, true
	}
	return 0, 0, false
}

// shifted reports whether ch is typed with Shift on a US layout. Probes send
// these with the Shift mask, as clients do.
func shifted(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || strings.IndexByte(`~!@#$%^&*()_+{}|:"<>?`, ch) >= 0
}
