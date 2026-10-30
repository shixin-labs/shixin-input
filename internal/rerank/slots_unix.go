//go:build darwin || linux

package rerank

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Slots are the engine's rerank slots, one per session, in anonymous
// memory outside the Go heap so librime may keep pointers into them.
type Slots struct {
	mem   []byte
	slots []Slot
}

// NewSlots maps n zeroed slots.
func NewSlots(n int) (*Slots, error) {
	mem, err := syscall.Mmap(-1, 0, n*int(SlotSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("rerank: map %d slots: %w", n, err)
	}
	return &Slots{mem: mem, slots: unsafe.Slice((*Slot)(unsafe.Pointer(&mem[0])), n)}, nil
}

// Len is the number of slots.
func (s *Slots) Len() int { return len(s.slots) }

// At returns slot i.
func (s *Slots) At(i int) *Slot { return &s.slots[i] }

// Base is the address of slot 0, for librime.
func (s *Slots) Base() unsafe.Pointer { return unsafe.Pointer(&s.mem[0]) }

// Close unmaps the slots. librime must have detached them first.
func (s *Slots) Close() error {
	s.slots = nil
	return syscall.Munmap(s.mem)
}
