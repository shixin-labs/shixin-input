package rerank

import (
	"errors"
	"math"

	"shixin/internal/ipc"
)

// The protocol between shixin-engine and shixin-ai (DESIGN §7.2) uses the
// client protocol's framing and field codec (ipc.Reader, ipc.BeginFrame,
// ipc.Decoder: u32 len | u8 type | payload, little-endian, strings as
// u32 len | bytes). Messages only gain trailing fields; golden vectors are
// in testdata/golden.txt. No C++ peer sees it.

// WireVersion is the protocol version in Hello and Status.
const WireVersion = 1

// Message types.
const (
	// Engine → shixin-ai.
	TypeHello   ipc.Type = 1
	TypeContext ipc.Type = 2
	TypeScore   ipc.Type = 3
	// shixin-ai → engine.
	TypeStatus ipc.Type = 64
	TypeRerank ipc.Type = 65
)

// Strength codes in Score (the 智能排序 setting).
const (
	StrengthCautious uint8 = 1
	StrengthStandard uint8 = 2
)

// ErrMalformed reports a truncated or invalid payload.
var ErrMalformed = errors.New("rerank: malformed message")

// Hello opens a connection from the engine.
type Hello struct {
	Version uint16
}

// Status answers Hello: whether shixin-ai can score.
type Status struct {
	Version uint16
	Ready   bool
}

// Context is an app's committed text after a change (ContextUpdate).
type Context struct {
	App  uint8
	Rev  uint32
	Text []byte
}

// ScoreEntry is one exported candidate.
type ScoreEntry struct {
	Weight float32
	Flags  uint8
	Text   []byte
}

// Score asks for an order of one export (ScoreRequest).
type Score struct {
	Session   uint32
	ExportSeq uint32
	App       uint8
	AppRev    uint32
	Strength  uint8
	Entries   []ScoreEntry
}

// Rerank is the order for an export: Perm[k] is the entry to show at
// position k.
type Rerank struct {
	Session   uint32
	ExportSeq uint32
	Perm      []uint8
}

// Append appends m as a frame to dst.
func (m *Hello) Append(dst []byte) []byte {
	off := len(dst)
	dst = ipc.BeginFrame(dst, TypeHello)
	dst = ipc.AppendU16(dst, m.Version)
	return ipc.EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Status) Append(dst []byte) []byte {
	off := len(dst)
	dst = ipc.BeginFrame(dst, TypeStatus)
	dst = ipc.AppendU16(dst, m.Version)
	dst = ipc.AppendBool(dst, m.Ready)
	return ipc.EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Context) Append(dst []byte) []byte {
	off := len(dst)
	dst = ipc.BeginFrame(dst, TypeContext)
	dst = append(dst, m.App)
	dst = ipc.AppendU32(dst, m.Rev)
	dst = ipc.AppendBytes(dst, m.Text)
	return ipc.EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Score) Append(dst []byte) []byte {
	off := len(dst)
	dst = ipc.BeginFrame(dst, TypeScore)
	dst = ipc.AppendU32(dst, m.Session)
	dst = ipc.AppendU32(dst, m.ExportSeq)
	dst = append(dst, m.App)
	dst = ipc.AppendU32(dst, m.AppRev)
	dst = append(dst, m.Strength, uint8(len(m.Entries)))
	for i := range m.Entries {
		e := &m.Entries[i]
		dst = ipc.AppendU32(dst, math.Float32bits(e.Weight))
		dst = append(dst, e.Flags)
		dst = ipc.AppendBytes(dst, e.Text)
	}
	return ipc.EndFrame(dst, off)
}

// AppendScore appends a Score frame for req, reading its entries from the
// export, without building a Score.
func AppendScore(dst []byte, req *Request, strength uint8) []byte {
	off := len(dst)
	sl := req.Export
	n := min(int(sl.Count), MaxOptions)
	dst = ipc.BeginFrame(dst, TypeScore)
	dst = ipc.AppendU32(dst, uint32(req.Session))
	dst = ipc.AppendU32(dst, req.ExportSeq)
	dst = append(dst, uint8(req.App))
	dst = ipc.AppendU32(dst, req.AppRev)
	dst = append(dst, strength, uint8(n))
	for i := range n {
		e := &sl.Entries[i]
		dst = ipc.AppendU32(dst, math.Float32bits(e.Weight))
		dst = append(dst, e.Flags)
		dst = ipc.AppendBytes(dst, sl.Text(i))
	}
	return ipc.EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Rerank) Append(dst []byte) []byte {
	off := len(dst)
	dst = ipc.BeginFrame(dst, TypeRerank)
	dst = ipc.AppendU32(dst, m.Session)
	dst = ipc.AppendU32(dst, m.ExportSeq)
	dst = append(dst, uint8(len(m.Perm)))
	dst = append(dst, m.Perm...)
	return ipc.EndFrame(dst, off)
}

// decodeErr returns ErrMalformed after a bad read, else nil.
func decodeErr(r *ipc.Decoder) error {
	if r.Bad() {
		return ErrMalformed
	}
	return nil
}

// Decode reads m from a frame payload.
func (m *Hello) Decode(payload []byte) error {
	r := ipc.NewDecoder(payload)
	m.Version = r.U16()
	return decodeErr(&r)
}

// Decode reads m from a frame payload.
func (m *Status) Decode(payload []byte) error {
	r := ipc.NewDecoder(payload)
	m.Version = r.U16()
	m.Ready = r.Bool()
	return decodeErr(&r)
}

// Decode reads m from a frame payload. Text aliases payload.
func (m *Context) Decode(payload []byte) error {
	r := ipc.NewDecoder(payload)
	m.App = r.U8()
	m.Rev = r.U32()
	m.Text = r.Bytes()
	return decodeErr(&r)
}

// Decode reads m from a frame payload, reusing m.Entries. Texts alias
// payload.
func (m *Score) Decode(payload []byte) error {
	r := ipc.NewDecoder(payload)
	m.Session = r.U32()
	m.ExportSeq = r.U32()
	m.App = r.U8()
	m.AppRev = r.U32()
	m.Strength = r.U8()
	n := int(r.U8())
	m.Entries = m.Entries[:0]
	for i := 0; i < n && !r.Bad(); i++ {
		w := math.Float32frombits(r.U32())
		f := r.U8()
		m.Entries = append(m.Entries, ScoreEntry{Weight: w, Flags: f, Text: r.Bytes()})
	}
	if r.Bad() {
		m.Entries = m.Entries[:0]
	}
	return decodeErr(&r)
}

// Decode reads m from a frame payload. Perm aliases payload.
func (m *Rerank) Decode(payload []byte) error {
	r := ipc.NewDecoder(payload)
	m.Session = r.U32()
	m.ExportSeq = r.U32()
	m.Perm = r.Take(int(r.U8()))
	return decodeErr(&r)
}
