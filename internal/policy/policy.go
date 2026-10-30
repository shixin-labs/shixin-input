// Package policy is the key policy: a small table that tells a client what
// to do with a key without asking the engine (DESIGN §3.3).
//
// The engine builds one Policy per state (idle or composing, per schema and
// mode) and sends it to clients. A client looks each key up in the policy
// of its current, possibly speculative, state. Lookups are table reads and
// never allocate. The C++ clients implement the same lookup from the wire
// format in Encode.
package policy

import (
	"encoding/binary"
	"errors"
	"slices"
)

// Disposition is what the client does with a key.
type Disposition uint8

const (
	// Pass lets the app have the key. Nothing is sent.
	Pass Disposition = iota
	// PassNotify lets the app have the key and sends it to the engine
	// asynchronously (Shift/Control taps for 中/英 switching).
	PassNotify
	// Eat reports the key as handled at once and sends it asynchronously.
	Eat
	// Ask sends the key and waits for the engine's answer within the budget.
	Ask
)

// String returns the disposition's name in DESIGN §3.3, e.g. "PASS_NOTIFY".
func (d Disposition) String() string {
	switch d {
	case Pass:
		return "PASS"
	case PassNotify:
		return "PASS_NOTIFY"
	case Eat:
		return "EAT"
	case Ask:
		return "ASK"
	}
	return "?"
}

// Next predicts the composing state after the engine processes a key, so a
// client can classify the following key before the result arrives.
type Next uint8

const (
	// Same: the composing state doesn't change.
	Same Next = iota
	// Composing: the key starts (or continues) a composition.
	Composing
	// Unknown: the key may end the composition (select, commit, delete).
	// The client must not guess the state of the next key from this one.
	Unknown
)

// Action is a Disposition and a Next packed into one byte.
type Action uint8

// Act packs d and n.
func Act(d Disposition, n Next) Action { return Action(d) | Action(n)<<2 }

// Disposition returns the action's disposition.
func (a Action) Disposition() Disposition { return Disposition(a & 3) }

// Next returns the predicted state after the action.
func (a Action) Next() Next { return Next(a >> 2 & 3) }

// Rime modifier masks (rime/key_table.h), as sent by every client.
const (
	ShiftMask   = 1 << 0
	LockMask    = 1 << 1
	ControlMask = 1 << 2
	AltMask     = 1 << 3
	SuperMask   = 1 << 26
	ReleaseMask = 1 << 30

	// CommandMask is the set of modifiers that turn a key into a shortcut.
	CommandMask = ControlMask | AltMask | SuperMask
)

// X11 keysyms of the modifier keys that Rime's ascii_composer watches.
const (
	KeyShiftL   = 0xffe1
	KeyShiftR   = 0xffe2
	KeyControlL = 0xffe3
	KeyControlR = 0xffe4
)

// X11 keysyms of the non-printable keys that the engine probes and the
// clients map or treat specially.
const (
	KeyBackSpace = 0xff08
	KeyTab       = 0xff09
	KeyReturn    = 0xff0d
	KeyEscape    = 0xff1b
	KeyHome      = 0xff50
	KeyLeft      = 0xff51
	KeyUp        = 0xff52
	KeyRight     = 0xff53
	KeyDown      = 0xff54
	KeyPrior     = 0xff55 // Page Up
	KeyNext      = 0xff56 // Page Down
	KeyEnd       = 0xff57
	KeyKPEnter   = 0xff8d
	KeyDelete    = 0xffff // Forward Delete
)

// Notify flags: which modifier keys the engine wants to see.
const (
	NotifyShift   uint8 = 1 << 0
	NotifyControl uint8 = 1 << 1
)

// Special is the action for a non-printable key, with or without Shift.
type Special struct {
	Keysym uint32
	Shift  bool
	Action Action
}

// Hotkey is a modifier combination that the engine binds, e.g. the
// switcher's Control+grave. Its keysym is the unshifted key.
type Hotkey struct {
	Keysym uint32
	Mods   uint32 // Shift, Control, Alt and Super bits only
}

// Policy is the client's key table for one engine state.
type Policy struct {
	// ID identifies the policy on a connection; results refer to it.
	ID uint16
	// Composing is the ID of the policy to assume after an action whose
	// Next is Composing, until the engine's result arrives.
	Composing uint16
	// Printable holds the actions for keysyms 0x20–0x7e, i.e. the
	// character the key produces, without command modifiers.
	Printable [95]Action
	// Special holds non-printable keys, sorted by (Keysym, Shift).
	Special []Special
	// SpecialDefault applies to non-printable keys not in Special.
	SpecialDefault Action
	// Hotkeys are command-modifier combinations that need the engine (Ask),
	// sorted by (Keysym, Mods).
	Hotkeys []Hotkey
	// CommandDefault applies to other command-modifier combinations.
	CommandDefault Action
	// Notify says which modifier keys to send as PassNotify.
	Notify uint8
}

// Lookup returns the action for a key press or release. keysym is an X11
// keysym and mods a Rime modifier mask. It never allocates.
func (p *Policy) Lookup(keysym, mods uint32) Action {
	switch keysym {
	case KeyShiftL, KeyShiftR:
		if p.Notify&NotifyShift != 0 {
			return Act(PassNotify, Same)
		}
		return Act(Pass, Same)
	case KeyControlL, KeyControlR:
		if p.Notify&NotifyControl != 0 {
			return Act(PassNotify, Same)
		}
		return Act(Pass, Same)
	}
	if mods&ReleaseMask != 0 {
		return Act(Pass, Same)
	}
	if mods&CommandMask != 0 {
		m := mods & (ShiftMask | CommandMask)
		if _, ok := slices.BinarySearchFunc(p.Hotkeys, Hotkey{keysym, m}, cmpHotkey); ok {
			return Act(Ask, Unknown)
		}
		return p.CommandDefault
	}
	if keysym >= 0x20 && keysym <= 0x7e {
		return p.Printable[keysym-0x20]
	}
	k := Special{Keysym: keysym, Shift: mods&ShiftMask != 0}
	if i, ok := slices.BinarySearchFunc(p.Special, k, cmpSpecial); ok {
		return p.Special[i].Action
	}
	return p.SpecialDefault
}

// Sort orders Special and Hotkeys for Lookup. Builders call it once.
func (p *Policy) Sort() {
	slices.SortFunc(p.Special, cmpSpecial)
	slices.SortFunc(p.Hotkeys, cmpHotkey)
}

func cmpSpecial(a, b Special) int {
	if a.Keysym != b.Keysym {
		return cmpU32(a.Keysym, b.Keysym)
	}
	switch {
	case a.Shift == b.Shift:
		return 0
	case !a.Shift:
		return -1
	}
	return 1
}

func cmpHotkey(a, b Hotkey) int {
	if a.Keysym != b.Keysym {
		return cmpU32(a.Keysym, b.Keysym)
	}
	return cmpU32(a.Mods, b.Mods)
}

func cmpU32(a, b uint32) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Wire format (little-endian), embedded in the IPC Policy message:
//
//	u16 id | u16 composing | u8 notify | u8 specialDefault | u8 commandDefault
//	| 95 × u8 printable
//	| u16 n | n × (u32 keysym, u8 shift, u8 action)
//	| u16 m | m × (u32 keysym, u32 mods)
//
// Special and Hotkeys are sent sorted.

// Append appends p's wire encoding to dst.
func (p *Policy) Append(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint16(dst, p.ID)
	dst = binary.LittleEndian.AppendUint16(dst, p.Composing)
	dst = append(dst, p.Notify, byte(p.SpecialDefault), byte(p.CommandDefault))
	for _, a := range p.Printable {
		dst = append(dst, byte(a))
	}
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(p.Special)))
	for _, s := range p.Special {
		dst = binary.LittleEndian.AppendUint32(dst, s.Keysym)
		var sh byte
		if s.Shift {
			sh = 1
		}
		dst = append(dst, sh, byte(s.Action))
	}
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(p.Hotkeys)))
	for _, h := range p.Hotkeys {
		dst = binary.LittleEndian.AppendUint32(dst, h.Keysym)
		dst = binary.LittleEndian.AppendUint32(dst, h.Mods)
	}
	return dst
}

var errShort = errors.New("policy: truncated")

// Decode reads a policy from b into p, reusing p's slices, and returns the
// bytes after it.
func (p *Policy) Decode(b []byte) ([]byte, error) {
	const fixed = 4 + 3 + 95 + 2
	if len(b) < fixed {
		return nil, errShort
	}
	p.ID = binary.LittleEndian.Uint16(b)
	p.Composing = binary.LittleEndian.Uint16(b[2:])
	p.Notify, p.SpecialDefault, p.CommandDefault = b[4], Action(b[5]), Action(b[6])
	for i := range p.Printable {
		p.Printable[i] = Action(b[7+i])
	}
	b = b[7+95:]
	n := int(binary.LittleEndian.Uint16(b))
	b = b[2:]
	if len(b) < n*6+2 {
		return nil, errShort
	}
	p.Special = p.Special[:0]
	for i := 0; i < n; i++ {
		p.Special = append(p.Special, Special{
			Keysym: binary.LittleEndian.Uint32(b),
			Shift:  b[4] != 0,
			Action: Action(b[5]),
		})
		b = b[6:]
	}
	m := int(binary.LittleEndian.Uint16(b))
	b = b[2:]
	if len(b) < m*8 {
		return nil, errShort
	}
	p.Hotkeys = p.Hotkeys[:0]
	for i := 0; i < m; i++ {
		p.Hotkeys = append(p.Hotkeys, Hotkey{
			Keysym: binary.LittleEndian.Uint32(b),
			Mods:   binary.LittleEndian.Uint32(b[4:]),
		})
		b = b[8:]
	}
	if !slices.IsSortedFunc(p.Special, cmpSpecial) || !slices.IsSortedFunc(p.Hotkeys, cmpHotkey) {
		return nil, errors.New("policy: tables not sorted")
	}
	return b, nil
}

// Passthrough is the policy of a client with no engine: every key goes to
// the app (DESIGN §3.3, disconnected).
var Passthrough = Policy{}
