package rerank

import "unsafe"

// RefreshKeysym is the private key that makes librime-shixin's
// shixin_rerank_processor refresh the composition, so the filter applies an
// imported order. Only the engine sends it; client keys with it are
// dropped. It lies outside X11's keysyms and Unicode's range.
const RefreshKeysym = 0x01fffffe

// SlotProperty is the context property that holds a session's slot index.
const SlotProperty = "shixin_rerank_slot"

// ArenaSize is the size of a slot's text arena.
const ArenaSize = 4096

// Entry flags.
const (
	EntryFixed uint8 = 1 << 0 // never moves: English, translators, custom phrases
	EntryUser  uint8 = 1 << 1 // a learned user_phrase
)

// ExportAbbrev marks an export whose input is initials only (简拼).
const ExportAbbrev uint16 = 1 << 0

// Entry is one exported candidate.
type Entry struct {
	Offset, Length uint16 // text in the slot's arena
	Flags          uint8
	_              [3]uint8
	// Weight is the candidate's prior: its dictionary log weight
	// (librime's Phrase::weight), or the log of its quality.
	Weight float32
}

// Slot is one session's exchange with the shixin_rerank filter. Its layout
// is librime-shixin's rerank::Slot (plugins/librime-shixin/src/
// rerank_slot.h); change both together. Slots live outside the Go heap
// (NewSlots), and only the engine thread touches them.
type Slot struct {
	// Written by the filter.
	ExportSeq   uint32 // changes with every new export; 0: none yet
	InputHash   uint32
	Count       uint16
	ExportFlags uint16
	ArenaUsed   uint32
	Entries     [MaxOptions]Entry
	Arena       [ArenaSize]byte
	// Written by the engine.
	Enabled     uint8 // 0: the filter passes candidates through
	_           [3]uint8
	ImportSeq   uint32 // the ExportSeq Perm is for; 0: none
	ImportCount uint16
	Perm        [MaxOptions]uint8 // Perm[k]: the entry shown at position k
	_           uint16
	// Counters written by the filter.
	Applied  uint32 // menus built with an imported order
	Rejected uint32 // imported orders that weren't valid permutations
}

// SlotSize is the size of a Slot, which librime checks at attach time.
const SlotSize = unsafe.Sizeof(Slot{})

// Text returns entry i's text. It aliases the slot.
func (s *Slot) Text(i int) []byte {
	e := &s.Entries[i]
	if int(e.Offset)+int(e.Length) > ArenaSize {
		return nil
	}
	return s.Arena[e.Offset : e.Offset+e.Length : e.Offset+e.Length]
}

// Import sets the order for the current export: perm[k] is the entry to
// show at position k. The filter applies it at the next refresh if the
// export hasn't changed meanwhile and perm is valid.
func (s *Slot) Import(perm []uint8) {
	n := copy(s.Perm[:], perm)
	s.ImportCount = uint16(n)
	s.ImportSeq = s.ExportSeq
}

// ClearImport drops the imported order, so the next refresh shows rime's.
func (s *Slot) ClearImport() { s.ImportSeq = 0 }

// Imported reports whether the slot holds an order for its current export.
func (s *Slot) Imported() bool { return s.ImportSeq != 0 && s.ImportSeq == s.ExportSeq }
