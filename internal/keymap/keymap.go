// Package keymap converts native key events to Rime keysyms and modifier
// masks (X11 conventions), for the Go clients. It is platform-free so it can
// be tested anywhere; the platform code supplies raw key codes and flags.
package keymap

import "shixin/internal/policy"

// macOS virtual key codes (Carbon kVK_*) of non-printable keys.
var macSpecial = [128]uint32{
	36:  policy.KeyReturn,
	48:  policy.KeyTab,
	49:  ' ',                 // Space
	51:  policy.KeyBackSpace, // Delete
	53:  policy.KeyEscape,
	76:  policy.KeyKPEnter,
	115: policy.KeyHome,
	116: policy.KeyPrior,  // Page Up
	117: policy.KeyDelete, // Forward Delete
	119: policy.KeyEnd,
	121: policy.KeyNext, // Page Down
	123: policy.KeyLeft,
	124: policy.KeyRight,
	125: policy.KeyDown,
	126: policy.KeyUp,
	122: 0xffbe, 120: 0xffbf, 99: 0xffc0, 118: 0xffc1, // F1–F4
	96: 0xffc2, 97: 0xffc3, 98: 0xffc4, 100: 0xffc5, // F5–F8
	101: 0xffc6, 109: 0xffc7, 103: 0xffc8, 111: 0xffc9, // F9–F12
}

// macOS modifier key codes.
const (
	MacShiftL   = 56
	MacShiftR   = 60
	MacControlL = 59
	MacControlR = 62
	MacCapsLock = 57
)

// NSEvent modifier flags.
const (
	MacFlagCapsLock = 1 << 16
	MacFlagShift    = 1 << 17
	MacFlagControl  = 1 << 18
	MacFlagOption   = 1 << 19
	MacFlagCommand  = 1 << 20
)

// MacMods converts NSEvent modifier flags to a Rime mask.
func MacMods(flags uint64) uint32 {
	var m uint32
	if flags&MacFlagShift != 0 {
		m |= policy.ShiftMask
	}
	if flags&MacFlagCapsLock != 0 {
		m |= policy.LockMask
	}
	if flags&MacFlagControl != 0 {
		m |= policy.ControlMask
	}
	if flags&MacFlagOption != 0 {
		m |= policy.AltMask
	}
	if flags&MacFlagCommand != 0 {
		m |= policy.SuperMask
	}
	return m
}

// MacKeyDown returns the keysym of a key-down event. ch is the first byte
// of the event's charactersIgnoringModifiers for command combinations, or
// of characters otherwise (so Shift+1 is '!'); 0 if none. ok is false for
// keys Shixin never handles (e.g. non-ASCII characters from other layouts),
// which pass through.
func MacKeyDown(keyCode uint16, ch byte) (keysym uint32, ok bool) {
	if keyCode < uint16(len(macSpecial)) && macSpecial[keyCode] != 0 {
		return macSpecial[keyCode], true
	}
	if ch > 0x20 && ch < 0x7f {
		return uint32(ch), true
	}
	return 0, false
}

// MacModifierKey returns the keysym for a FlagsChanged event of a modifier
// key and whether it was pressed or released; ok is false for modifier keys
// Rime doesn't watch.
func MacModifierKey(keyCode uint16, flags uint64) (keysym uint32, mods uint32, ok bool) {
	var down bool
	switch keyCode {
	case MacShiftL, MacShiftR:
		keysym, down = policy.KeyShiftL, flags&MacFlagShift != 0
		if keyCode == MacShiftR {
			keysym = policy.KeyShiftR
		}
	case MacControlL, MacControlR:
		keysym, down = policy.KeyControlL, flags&MacFlagControl != 0
		if keyCode == MacControlR {
			keysym = policy.KeyControlR
		}
	default:
		return 0, 0, false
	}
	mods = MacMods(flags)
	if !down {
		mods |= policy.ReleaseMask
	}
	return keysym, mods, true
}
