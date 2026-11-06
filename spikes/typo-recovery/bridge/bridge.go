// Package bridge is an unshipped purego binding for the scratch correction ABI.
// All native methods run on the engine's locked OS thread. Attach is startup work.
package bridge

import (
	"encoding/binary"
	"errors"
	"github.com/ebitengine/purego"
	"math"
	"runtime"
	"unicode/utf8"
	"unsafe"
)

const (
	Version      = 1
	TokenSize    = 40
	HeaderSize   = 16
	RecordSize   = 328
	SnapshotSize = 2640
)

var ErrUnsupported = errors.New("scratch correction ABI unavailable or incompatible")

type Token [TokenSize]byte
type Snapshot [SnapshotSize]byte
type Record struct {
	Token                                Token
	Begin, End, Kind, EditBegin, EditEnd uint32
	Cost                                 float32
	Text                                 []byte // caller-owned snapshot storage; invalidated by the next read
}
type Native struct {
	configure, read, selectChoice, invalidate uintptr
	args                                      [3]uintptr
	keepSnapshot                              *Snapshot
	keepToken                                 *Token
}

func Attach(resolve func(string) (uintptr, error)) (*Native, error) {
	if resolve == nil || unsafe.Sizeof(uintptr(0)) != 8 {
		return nil, ErrUnsupported
	}
	version, err := resolve("shixin_spike_corrections_version")
	if err != nil || version == 0 {
		return nil, ErrUnsupported
	}
	value, _, _ := purego.SyscallN(version)
	if value != Version {
		return nil, ErrUnsupported
	}
	n := new(Native)
	fields := []*uintptr{&n.configure, &n.read, &n.selectChoice, &n.invalidate}
	names := []string{"configure", "read", "select", "invalidate"}
	for i, name := range names {
		value, err := resolve("shixin_spike_corrections_v1_" + name)
		if err != nil || value == 0 {
			return nil, ErrUnsupported
		}
		*fields[i] = value
	}
	return n, nil
}
func (n *Native) call(fn uintptr, count int, a, b, c uintptr) uintptr {
	n.args = [3]uintptr{a, b, c}
	result, _, _ := purego.SyscallN(fn, n.args[:count]...)
	return result
}
func (n *Native) Configure(session uintptr, generation uint64) bool {
	return n != nil && n.configure != 0 && n.call(n.configure, 2, session, uintptr(generation), 0) == 1
}
func (n *Native) Read(session uintptr, snapshot *Snapshot) bool {
	if snapshot == nil {
		return false
	}
	if n == nil || n.read == 0 {
		*snapshot = Snapshot{}
		return false
	}
	// Retaining the pointer forces reusable caller buffers onto stable heap storage.
	n.keepSnapshot = snapshot
	result := n.call(n.read, 3, session, uintptr(unsafe.Pointer(n.keepSnapshot)), SnapshotSize)
	runtime.KeepAlive(snapshot)
	n.keepSnapshot = nil
	ok := int32(result) == 0 && ValidSnapshot(snapshot[:])
	if !ok {
		*snapshot = Snapshot{}
	}
	return ok
}
func (n *Native) Select(session uintptr, token *Token) bool {
	if n == nil || n.selectChoice == 0 || token == nil {
		return false
	}
	n.keepToken = token
	result := n.call(n.selectChoice, 3, session, uintptr(unsafe.Pointer(n.keepToken)), TokenSize)
	runtime.KeepAlive(token)
	n.keepToken = nil
	return result == 1
}
func (n *Native) Invalidate(session uintptr) bool {
	return n != nil && n.invalidate != 0 && n.call(n.invalidate, 1, session, 0, 0) == 1
}
func zeros(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
func header(b []byte) (int, bool) {
	if len(b) != SnapshotSize || binary.LittleEndian.Uint32(b) != Version || binary.LittleEndian.Uint32(b[4:]) != SnapshotSize || !zeros(b[12:16]) {
		return 0, false
	}
	count := binary.LittleEndian.Uint32(b[8:])
	return int(count), count <= 8
}
func validRecord(b []byte, slot int) bool {
	for _, offset := range [...]int{0, 8, 16, 24} {
		if binary.LittleEndian.Uint64(b[offset:]) == 0 {
			return false
		}
	}
	if b[32] != byte(slot) || !zeros(b[33:40]) || !zeros(b[68:72]) {
		return false
	}
	begin, end := binary.LittleEndian.Uint32(b[40:]), binary.LittleEndian.Uint32(b[44:])
	kind, editBegin, editEnd := binary.LittleEndian.Uint32(b[48:]), binary.LittleEndian.Uint32(b[52:]), binary.LittleEndian.Uint32(b[56:])
	if begin >= end || end > 32 || editBegin < begin || editEnd > end || editEnd < editBegin {
		return false
	}
	width := editEnd - editBegin
	if kind > 3 || (kind == 0 && width != 2) || ((kind == 1 || kind == 3) && width != 1) || (kind == 2 && width != 0) {
		return false
	}
	cost := math.Float32frombits(binary.LittleEndian.Uint32(b[60:]))
	if cost < 0 || math.IsNaN(float64(cost)) || math.IsInf(float64(cost), 0) {
		return false
	}
	length := binary.LittleEndian.Uint32(b[64:])
	if length == 0 || length > 256 {
		return false
	}
	return utf8.Valid(b[72:72+length]) && zeros(b[72+length:])
}
func ValidSnapshot(b []byte) bool {
	count, ok := header(b)
	if !ok {
		return false
	}
	for i := 0; i < count; i++ {
		r := b[HeaderSize+i*RecordSize : HeaderSize+(i+1)*RecordSize]
		if !validRecord(r, i) {
			return false
		}
		if i > 0 {
			for j := 0; j < 32; j++ {
				if r[j] != b[HeaderSize+j] {
					return false
				}
			}
		}
	}
	return zeros(b[HeaderSize+count*RecordSize:])
}
func (s *Snapshot) Count() int {
	if s == nil {
		return 0
	}
	count, ok := header(s[:])
	if !ok {
		return 0
	}
	return count
}
func (s *Snapshot) Record(index int) (Record, bool) {
	var r Record
	if s == nil || !ValidSnapshot(s[:]) || index < 0 || index >= s.Count() {
		return r, false
	}
	b := s[HeaderSize+index*RecordSize : HeaderSize+(index+1)*RecordSize]
	copy(r.Token[:], b[:TokenSize])
	r.Begin = binary.LittleEndian.Uint32(b[40:])
	r.End = binary.LittleEndian.Uint32(b[44:])
	r.Kind = binary.LittleEndian.Uint32(b[48:])
	r.EditBegin = binary.LittleEndian.Uint32(b[52:])
	r.EditEnd = binary.LittleEndian.Uint32(b[56:])
	r.Cost = math.Float32frombits(binary.LittleEndian.Uint32(b[60:]))
	length := binary.LittleEndian.Uint32(b[64:])
	r.Text = b[72 : 72+length]
	return r, true
}
