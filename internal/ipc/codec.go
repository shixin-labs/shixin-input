package ipc

import (
	"encoding/binary"
	"errors"
)

// The field codec of the protocol, shared with other protocols that use
// the same framing (internal/rerank's engine↔shixin-ai protocol). Encoders
// append to a caller-owned buffer; Decoder aliases the payload. Neither
// allocates.

// ErrMalformed reports a payload that is too short or inconsistent.
var ErrMalformed = errors.New("ipc: malformed message")

// BeginFrame appends the header of a frame of type t to dst. Pass len(dst)
// from before the call to EndFrame once the payload is appended.
func BeginFrame(dst []byte, t Type) []byte {
	return append(dst, 0, 0, 0, 0, byte(t))
}

// EndFrame patches the length of the frame starting at off.
func EndFrame(dst []byte, off int) []byte {
	binary.LittleEndian.PutUint32(dst[off:], uint32(len(dst)-off-4))
	return dst
}

// AppendU16 appends v, little-endian.
func AppendU16(b []byte, v uint16) []byte { return binary.LittleEndian.AppendUint16(b, v) }

// AppendU32 appends v, little-endian.
func AppendU32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }

// AppendU64 appends v, little-endian.
func AppendU64(b []byte, v uint64) []byte { return binary.LittleEndian.AppendUint64(b, v) }

// AppendBytes appends s as a byte string: u32 len | bytes.
func AppendBytes(b []byte, s []byte) []byte { return append(AppendU32(b, uint32(len(s))), s...) }

// AppendBool appends v as one byte, 0 or 1.
func AppendBool(b []byte, v bool) []byte {
	if v {
		return append(b, 1)
	}
	return append(b, 0)
}

// Decoder reads fields from a payload. After the first short read every
// read returns zero and Bad reports true.
type Decoder struct {
	b   []byte
	bad bool
}

// NewDecoder returns a Decoder reading payload.
func NewDecoder(payload []byte) Decoder { return Decoder{b: payload} }

// Take returns the next n bytes, aliasing the payload.
func (d *Decoder) Take(n int) []byte {
	if d.bad || len(d.b) < n {
		d.bad = true
		return nil
	}
	p := d.b[:n:n]
	d.b = d.b[n:]
	return p
}

// U8 reads a byte.
func (d *Decoder) U8() uint8 {
	if p := d.Take(1); p != nil {
		return p[0]
	}
	return 0
}

// U16 reads a little-endian uint16.
func (d *Decoder) U16() uint16 {
	if p := d.Take(2); p != nil {
		return binary.LittleEndian.Uint16(p)
	}
	return 0
}

// U32 reads a little-endian uint32.
func (d *Decoder) U32() uint32 {
	if p := d.Take(4); p != nil {
		return binary.LittleEndian.Uint32(p)
	}
	return 0
}

// U64 reads a little-endian uint64.
func (d *Decoder) U64() uint64 {
	if p := d.Take(8); p != nil {
		return binary.LittleEndian.Uint64(p)
	}
	return 0
}

// Bool reads a byte as a bool: anything but 0 is true.
func (d *Decoder) Bool() bool { return d.U8() != 0 }

// Bytes reads a byte string (u32 len | bytes), aliasing the payload.
func (d *Decoder) Bytes() []byte {
	n := d.U32()
	if n > MaxFrame {
		d.bad = true
		return nil
	}
	return d.Take(int(n))
}

// Rest returns the bytes not read yet.
func (d *Decoder) Rest() []byte { return d.b }

// Bad reports whether a read ran past the payload.
func (d *Decoder) Bad() bool { return d.bad }

// Err returns ErrMalformed after a bad read, else nil.
func (d *Decoder) Err() error {
	if d.bad {
		return ErrMalformed
	}
	return nil
}
