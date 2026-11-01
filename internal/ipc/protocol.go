// Package ipc is the protocol between shixin-engine and its clients
// (DESIGN §5.4): length-prefixed binary frames over a Unix socket or named
// pipe.
//
// Frame: u32 len | u8 type | payload, where len counts the type byte and
// the payload. Integers are little-endian; byte strings are u32 len | bytes.
//
// Compatibility: messages only ever gain fields at the end, and decoders
// ignore trailing bytes they don't know. Old TSF DLLs stay loaded in apps
// after an update, so a change here must keep decoding old frames and be
// mirrored in the C++ codec and the golden vectors in testdata.
//
// Encoding appends to a caller-owned buffer and decoding aliases the frame,
// so neither allocates once buffers have grown.
package ipc

import (
	"shixin/internal/perf"
	"shixin/internal/policy"
)

// Version is the protocol version sent in Hello and Welcome.
const Version = 1

// MaxFrame bounds a frame's length; larger frames are a protocol error.
const MaxFrame = 1 << 20

// Type is a message type.
type Type uint8

// Client → engine.
const (
	TypeHello      Type = 1
	TypeKey        Type = 2
	TypeFocusIn    Type = 3
	TypeFocusOut   Type = 4
	TypeSelect     Type = 5
	TypeChangePage Type = 6
	TypeClear      Type = 7
	TypeSetOption  Type = 8
	// TypeStatsRequest asks for the engine's counters (the settings app's
	// performance page); the engine answers with Stats.
	TypeStatsRequest Type = 9
	// TypeCaret is a TSF-to-candidate-host screen rectangle in physical pixels.
	// It is never sent on the engine key stream.
	TypeCaret Type = 10
)

// Engine → client.
const (
	TypeWelcome     Type = 64
	TypeResult      Type = 65
	TypePolicy      Type = 66
	TypeUnavailable Type = 67
	TypeMigrate     Type = 68
	TypeStats       Type = 69
)

// ClientKind identifies the client in Hello.
type ClientKind uint8

// Client kinds.
const (
	KindTest      ClientKind = iota // tests and tools
	KindMacOS                       // Shixin.app's IMK front
	KindTSF                         // the Windows TSF DLL
	KindFcitx5                      // the fcitx5 addon
	KindWindowsUI                   // Shixin.exe, the Windows candidate window
	KindSettings                    // the settings app: stats only, no input contexts
)

// Hello opens a connection.
type Hello struct {
	Version uint16
	Kind    ClientKind
	PID     uint32
	App     []byte // process or bundle identifier
}

// Welcome answers Hello.
type Welcome struct {
	Version  uint16
	Features uint32
}

// Key flags.
const (
	// KeySync marks an ASK key: the client is waiting for the result.
	KeySync uint8 = 1 << 0
	// KeyNotify marks a key the client passed to the app (PASS_NOTIFY, or a
	// shortcut typed while Shift is down). The engine processes it for
	// ascii_composer's benefit; its result needn't be applied.
	KeyNotify uint8 = 1 << 1
)

// Key is a key press or release in Rime terms.
type Key struct {
	Ctx    uint32 // client-chosen input context
	Seq    uint32 // per-connection request sequence number
	Keysym uint32 // X11 keysym
	Mods   uint32 // Rime modifier mask
	Flags  uint8
}

// FocusIn flags.
const FocusSecure uint8 = 1 << 0

// FocusIn tells the engine which app an input context is in.
type FocusIn struct {
	Ctx   uint32
	Flags uint8
	App   []byte
}

// FocusOut ends focus; the engine clears the context's composition.
type FocusOut struct {
	Ctx uint32
}

// Select picks a candidate on the current page (mouse click).
type Select struct {
	Ctx, Seq, Index uint32
}

// ChangePage flips the candidate page.
type ChangePage struct {
	Ctx, Seq uint32
	Backward bool
}

// Clear discards the composition.
type Clear struct {
	Ctx, Seq uint32
}

// SetOption sets a Rime switch such as ascii_mode.
type SetOption struct {
	Ctx, Seq uint32
	On       bool
	Name     []byte
}

// Result flags.
const (
	ResultHandled     uint16 = 1 << 0
	ResultComposing   uint16 = 1 << 1
	ResultASCII       uint16 = 1 << 2
	ResultLastPage    uint16 = 1 << 3
	ResultFullShape   uint16 = 1 << 4
	ResultTraditional uint16 = 1 << 5
	ResultASCIIPunct  uint16 = 1 << 6
	// ResultAutoSpace: auto spacing is on for this context (DESIGN §7.1).
	ResultAutoSpace uint16 = 1 << 7
)

// Candidate is one menu entry.
type Candidate struct {
	Text, Comment []byte
}

// Result is the engine's state after a request.
type Result struct {
	Ctx, Seq uint32
	Flags    uint16
	// Policy is the ID of the policy for the next key. The engine sends a
	// Policy message before the first Result that refers to it.
	Policy      uint16
	Commit      []byte
	Preedit     []byte
	Cursor      uint32 // byte offset in Preedit
	SelStart    uint32
	SelEnd      uint32
	PageNo      uint16
	Highlighted uint16
	Candidates  []Candidate
	Labels      []byte // select keys, one byte per candidate; empty means 1–9, 0
}

// Unavailable reasons.
const (
	UnavailableStarting uint8 = iota + 1
	UnavailableMaintenance
	UnavailableShuttingDown
)

// Unavailable tells clients to pass keys through until further notice.
// A later Result or Welcome ends it.
type Unavailable struct {
	Reason uint8
}

// Migrate asks the client to reconnect to another endpoint (hot swap).
type Migrate struct {
	Endpoint []byte
}

// StatsRequest asks for Stats.
type StatsRequest struct {
	// Reset clears the latency histogram after reading it.
	Reset bool
}

// Stats are the engine's counters (DESIGN §3.7).
type Stats struct {
	Keys, Dropped uint64
	StartupMS     uint32 // load, deploy and warm-up
	UptimeS       uint32
	Librime       []byte // librime version
	Native        bool   // librime-shixin loaded
	Schemas       []byte // comma-separated schema ids
	KeyTime       perf.Hist
	// Rerank is 智能排序's state (DESIGN §7.2), appended after KeyTime;
	// nil from engines without it.
	Rerank *RerankStats
}

// RerankStats are 智能排序's state and counters in Stats.
type RerankStats struct {
	On    bool // the setting is on
	Ready bool // shixin-ai is connected
	Slow  bool // its orders came too late here: paused, probing
	// Results held for an order: sent with it, timed out, superseded by
	// a newer request; orders that came too late, or were invalid.
	Requests, Applied, Timeouts, Superseded, Dropped, Rejected uint64
	Wait                                                       perf.Hist // how long held results waited
}

// ---------------------------------------------------------------- encoding

// Append appends m as a frame to dst.
func (m *Hello) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeHello)
	dst = AppendU16(dst, m.Version)
	dst = append(dst, byte(m.Kind))
	dst = AppendU32(dst, m.PID)
	dst = AppendBytes(dst, m.App)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Welcome) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeWelcome)
	dst = AppendU16(dst, m.Version)
	dst = AppendU32(dst, m.Features)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Key) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeKey)
	dst = AppendU32(dst, m.Ctx)
	dst = AppendU32(dst, m.Seq)
	dst = AppendU32(dst, m.Keysym)
	dst = AppendU32(dst, m.Mods)
	dst = append(dst, m.Flags)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *FocusIn) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeFocusIn)
	dst = AppendU32(dst, m.Ctx)
	dst = append(dst, m.Flags)
	dst = AppendBytes(dst, m.App)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *FocusOut) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeFocusOut)
	dst = AppendU32(dst, m.Ctx)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Select) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeSelect)
	dst = AppendU32(dst, m.Ctx)
	dst = AppendU32(dst, m.Seq)
	dst = AppendU32(dst, m.Index)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *ChangePage) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeChangePage)
	dst = AppendU32(dst, m.Ctx)
	dst = AppendU32(dst, m.Seq)
	dst = AppendBool(dst, m.Backward)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Clear) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeClear)
	dst = AppendU32(dst, m.Ctx)
	dst = AppendU32(dst, m.Seq)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *SetOption) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeSetOption)
	dst = AppendU32(dst, m.Ctx)
	dst = AppendU32(dst, m.Seq)
	dst = AppendBool(dst, m.On)
	dst = AppendBytes(dst, m.Name)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Result) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeResult)
	dst = AppendU32(dst, m.Ctx)
	dst = AppendU32(dst, m.Seq)
	dst = AppendU16(dst, m.Flags)
	dst = AppendU16(dst, m.Policy)
	dst = AppendBytes(dst, m.Commit)
	dst = AppendBytes(dst, m.Preedit)
	dst = AppendU32(dst, m.Cursor)
	dst = AppendU32(dst, m.SelStart)
	dst = AppendU32(dst, m.SelEnd)
	dst = AppendU16(dst, m.PageNo)
	dst = AppendU16(dst, m.Highlighted)
	dst = AppendU16(dst, uint16(len(m.Candidates)))
	for i := range m.Candidates {
		dst = AppendBytes(dst, m.Candidates[i].Text)
		dst = AppendBytes(dst, m.Candidates[i].Comment)
	}
	dst = AppendBytes(dst, m.Labels)
	return EndFrame(dst, off)
}

// AppendPolicy appends a Policy message for p to dst.
func AppendPolicy(dst []byte, p *policy.Policy) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypePolicy)
	dst = p.Append(dst)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Unavailable) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeUnavailable)
	dst = append(dst, m.Reason)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Migrate) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeMigrate)
	dst = AppendBytes(dst, m.Endpoint)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *StatsRequest) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeStatsRequest)
	dst = AppendBool(dst, m.Reset)
	return EndFrame(dst, off)
}

// Append appends m as a frame to dst.
func (m *Stats) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeStats)
	dst = AppendU64(dst, m.Keys)
	dst = AppendU64(dst, m.Dropped)
	dst = AppendU32(dst, m.StartupMS)
	dst = AppendU32(dst, m.UptimeS)
	dst = AppendBytes(dst, m.Librime)
	dst = AppendBool(dst, m.Native)
	dst = AppendBytes(dst, m.Schemas)
	dst = m.KeyTime.Append(dst)
	if r := m.Rerank; r != nil {
		var flags uint8
		if r.On {
			flags |= 1
		}
		if r.Ready {
			flags |= 2
		}
		if r.Slow {
			flags |= 4
		}
		dst = append(dst, flags)
		for _, v := range [...]uint64{r.Requests, r.Applied, r.Timeouts, r.Superseded, r.Dropped, r.Rejected} {
			dst = AppendU64(dst, v)
		}
		dst = r.Wait.Append(dst)
	}
	return EndFrame(dst, off)
}

// ---------------------------------------------------------------- decoding

// Decode reads m from a frame payload. Byte fields alias payload.
func (m *Hello) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Version = r.U16()
	m.Kind = ClientKind(r.U8())
	m.PID = r.U32()
	m.App = r.Bytes()
	return r.Err()
}

// Decode reads m from a frame payload.
func (m *Welcome) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Version = r.U16()
	m.Features = r.U32()
	return r.Err()
}

// Decode reads m from a frame payload.
func (m *Key) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	m.Seq = r.U32()
	m.Keysym = r.U32()
	m.Mods = r.U32()
	m.Flags = r.U8()
	return r.Err()
}

// Decode reads m from a frame payload. Byte fields alias payload.
func (m *FocusIn) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	m.Flags = r.U8()
	m.App = r.Bytes()
	return r.Err()
}

// Decode reads m from a frame payload.
func (m *FocusOut) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	return r.Err()
}

// Decode reads m from a frame payload.
func (m *Select) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	m.Seq = r.U32()
	m.Index = r.U32()
	return r.Err()
}

// Decode reads m from a frame payload.
func (m *ChangePage) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	m.Seq = r.U32()
	m.Backward = r.Bool()
	return r.Err()
}

// Decode reads m from a frame payload.
func (m *Clear) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	m.Seq = r.U32()
	return r.Err()
}

// Decode reads m from a frame payload. Byte fields alias payload.
func (m *SetOption) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	m.Seq = r.U32()
	m.On = r.Bool()
	m.Name = r.Bytes()
	return r.Err()
}

// Decode reads m from a frame payload, reusing m.Candidates. Byte fields
// alias payload.
func (m *Result) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	m.Seq = r.U32()
	m.Flags = r.U16()
	m.Policy = r.U16()
	m.Commit = r.Bytes()
	m.Preedit = r.Bytes()
	m.Cursor = r.U32()
	m.SelStart = r.U32()
	m.SelEnd = r.U32()
	m.PageNo = r.U16()
	m.Highlighted = r.U16()
	n := int(r.U16())
	m.Candidates = m.Candidates[:0]
	for i := 0; i < n && !r.Bad(); i++ {
		m.Candidates = append(m.Candidates, Candidate{Text: r.Bytes(), Comment: r.Bytes()})
	}
	m.Labels = r.Bytes()
	if r.Bad() {
		m.Candidates = m.Candidates[:0]
	}
	return r.Err()
}

// DecodePolicy reads a Policy message payload into p, reusing its slices.
func DecodePolicy(payload []byte, p *policy.Policy) error {
	if _, err := p.Decode(payload); err != nil {
		return ErrMalformed
	}
	return nil
}

// Decode reads m from a frame payload.
func (m *Unavailable) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Reason = r.U8()
	return r.Err()
}

// Decode reads m from a frame payload. Byte fields alias payload.
func (m *Migrate) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Endpoint = r.Bytes()
	return r.Err()
}

// Decode reads m from a frame payload.
func (m *StatsRequest) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Reset = r.Bool()
	return r.Err()
}

// Decode reads m from a frame payload. Byte fields alias payload.
func (m *Stats) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Keys = r.U64()
	m.Dropped = r.U64()
	m.StartupMS = r.U32()
	m.UptimeS = r.U32()
	m.Librime = r.Bytes()
	m.Native = r.Bool()
	m.Schemas = r.Bytes()
	if r.Bad() {
		return ErrMalformed
	}
	rest, err := m.KeyTime.Decode(r.Rest())
	if err != nil {
		return ErrMalformed
	}
	m.Rerank = nil
	if len(rest) == 0 {
		return nil // an engine without 智能排序
	}
	r = NewDecoder(rest)
	flags := r.U8()
	rs := &RerankStats{On: flags&1 != 0, Ready: flags&2 != 0, Slow: flags&4 != 0}
	for _, v := range [...]*uint64{&rs.Requests, &rs.Applied, &rs.Timeouts, &rs.Superseded, &rs.Dropped, &rs.Rejected} {
		*v = r.U64()
	}
	if r.Bad() {
		return ErrMalformed
	}
	if _, err := rs.Wait.Decode(r.Rest()); err != nil {
		return ErrMalformed
	}
	m.Rerank = rs
	return nil
}

// Caret positions the Windows candidate host for one focused context.
// Coordinates are physical screen pixels; negative monitor origins are valid.
type Caret struct {
	Ctx                      uint32
	Left, Top, Right, Bottom int32
}

// Append appends m as a frame.
func (m *Caret) Append(dst []byte) []byte {
	off := len(dst)
	dst = BeginFrame(dst, TypeCaret)
	dst = AppendU32(dst, m.Ctx)
	for _, v := range [...]int32{m.Left, m.Top, m.Right, m.Bottom} {
		dst = AppendU32(dst, uint32(v))
	}
	return EndFrame(dst, off)
}

// Decode reads a caret from a frame payload, ignoring future trailing fields.
func (m *Caret) Decode(payload []byte) error {
	r := NewDecoder(payload)
	m.Ctx = r.U32()
	m.Left = int32(r.U32())
	m.Top = int32(r.U32())
	m.Right = int32(r.U32())
	m.Bottom = int32(r.U32())
	return r.Err()
}
