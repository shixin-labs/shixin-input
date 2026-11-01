package rerank

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Slots holds stable, zeroed native memory that librime may reference.
type Slots struct {
	base  uintptr
	slots []Slot
}

// NewSlots allocates n slots outside the Go heap.
func NewSlots(n int) (*Slots, error) {
	if n <= 0 || uintptr(n) > ^uintptr(0)/SlotSize {
		return nil, fmt.Errorf("rerank: invalid slot count %d", n)
	}
	p, err := windows.VirtualAlloc(0, uintptr(n)*SlotSize, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		return nil, fmt.Errorf("rerank: allocate slots: %w", err)
	}
	return &Slots{base: p, slots: unsafe.Slice((*Slot)(nativePointer(p)), n)}, nil
}

// Len is the number of slots.
func (s *Slots) Len() int { return len(s.slots) }

// At returns slot i.
func (s *Slots) At(i int) *Slot { return &s.slots[i] }

// Base is the address passed to librime.
func (s *Slots) Base() unsafe.Pointer { return nativePointer(s.base) }

// VirtualAlloc returns native memory rather than an address in the Go heap.
// As in internal/rime.cptr, reinterpret it without Go pointer arithmetic.
func nativePointer(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// Close releases the native memory after librime has detached it.
func (s *Slots) Close() error {
	if s.base == 0 {
		return nil
	}
	if err := windows.VirtualFree(s.base, 0, windows.MEM_RELEASE); err != nil {
		return err
	}
	s.base, s.slots = 0, nil
	return nil
}
