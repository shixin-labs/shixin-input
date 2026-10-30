package rerank

import (
	"testing"
	"unsafe"
)

// TestSlotLayout pins the offsets librime-shixin's rerank_slot.h asserts.
func TestSlotLayout(t *testing.T) {
	var s Slot
	for _, c := range []struct {
		name      string
		got, want uintptr
	}{
		{"sizeof(Entry)", unsafe.Sizeof(Entry{}), 12},
		{"entries", unsafe.Offsetof(s.Entries), 16},
		{"arena", unsafe.Offsetof(s.Arena), 16 + 12*MaxOptions},
		{"enabled", unsafe.Offsetof(s.Enabled), 16 + 12*MaxOptions + ArenaSize},
		{"import_seq", unsafe.Offsetof(s.ImportSeq), 16 + 12*MaxOptions + ArenaSize + 4},
		{"perm", unsafe.Offsetof(s.Perm), 16 + 12*MaxOptions + ArenaSize + 10},
		{"applied", unsafe.Offsetof(s.Applied), 16 + 12*MaxOptions + ArenaSize + 76},
		{"sizeof(Slot)", SlotSize, 16 + 12*MaxOptions + ArenaSize + 84},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestSlots(t *testing.T) {
	s, err := NewSlots(3)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sl := s.At(2)
	copy(sl.Arena[:], "台湾天物")
	sl.Entries[1] = Entry{Offset: 6, Length: 6}
	sl.Count, sl.ExportSeq = 2, 5
	if got := string(sl.Text(1)); got != "天物" {
		t.Errorf("Text(1) = %q", got)
	}
	sl.Import([]uint8{1, 0})
	if !sl.Imported() || sl.ImportCount != 2 || sl.Perm[0] != 1 {
		t.Errorf("import: %v %d %v", sl.Imported(), sl.ImportCount, sl.Perm[:2])
	}
	sl.ExportSeq++
	if sl.Imported() {
		t.Error("an import outlived its export")
	}
	sl.Entries[0] = Entry{Offset: ArenaSize - 1, Length: 2}
	if sl.Text(0) != nil {
		t.Error("out-of-arena entry read")
	}
}
