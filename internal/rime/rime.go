// Package rime binds librime through its C API (rime_get_api) using purego,
// without cgo.
//
// librime is not thread-safe: all calls must come from a single goroutine,
// the engine thread (DESIGN §3.4). Hot-path methods on Session write into
// caller-owned, reusable buffers and do not allocate.
package rime

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Traits configures librime. Modules should always be set explicitly; the
// engine passes {"default"} so the dynamic "plugins" loader (and with it any
// librime-lua found on disk) never runs (DESIGN R9).
type Traits struct {
	SharedDataDir        string
	UserDataDir          string
	StagingDir           string
	PrebuiltDataDir      string
	LogDir               string // "" logs to stderr only
	MinLogLevel          int    // 0 info, 1 warning, 2 error, 3 fatal
	DistributionName     string
	DistributionCodeName string
	DistributionVersion  string
	AppName              string // "rime.<app>"
	Modules              []string
}

// Lib is a loaded librime.
type Lib struct {
	handle uintptr
	api    *cAPI
	traits cTraits
	keep   [][]byte // C strings referenced by traits; kept alive for librime
	mods   []uintptr

	// args backs every call's argument slice. purego.SyscallN allocates the
	// variadic array when called with literal arguments; passing a slice of
	// this array does not. Safe because librime is single-threaded anyway.
	args [4]uintptr
}

// call invokes fn with up to len(args) arguments without allocating.
func (l *Lib) call(fn uintptr, a ...uintptr) uintptr {
	n := copy(l.args[:], a)
	r1, _, _ := purego.SyscallN(fn, l.args[:n]...)
	return r1
}

// Load opens librime at path and validates its API table.
func Load(path string) (*Lib, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("rime: dlopen %s: %w", path, err)
	}
	sym, err := purego.Dlsym(h, "rime_get_api")
	if err != nil {
		return nil, fmt.Errorf("rime: rime_get_api: %w", err)
	}
	r1, _, _ := purego.SyscallN(sym)
	if r1 == 0 {
		return nil, errors.New("rime: rime_get_api returned NULL")
	}
	l := &Lib{handle: h, api: (*cAPI)(cptr(r1))}
	// Everything up to change_page (librime 1.10) must be present.
	need := unsafe.Offsetof(cAPI{}.changePage)
	if uintptr(l.api.dataSize)+unsafe.Sizeof(int32(0)) <= need {
		return nil, fmt.Errorf("rime: librime too old (api data_size %d)", l.api.dataSize)
	}
	return l, nil
}

func (l *Lib) cstr(s string) uintptr {
	if s == "" {
		return 0
	}
	b := append([]byte(s), 0)
	l.keep = append(l.keep, b)
	return uintptr(unsafe.Pointer(&b[0]))
}

func (l *Lib) fillTraits(t Traits) {
	l.keep = l.keep[:0]
	l.traits = cTraits{
		dataSize:             int32(unsafe.Sizeof(cTraits{}) - unsafe.Sizeof(int32(0))),
		sharedDataDir:        l.cstr(t.SharedDataDir),
		userDataDir:          l.cstr(t.UserDataDir),
		distributionName:     l.cstr(t.DistributionName),
		distributionCodeName: l.cstr(t.DistributionCodeName),
		distributionVersion:  l.cstr(t.DistributionVersion),
		appName:              l.cstr(t.AppName),
		minLogLevel:          int32(t.MinLogLevel),
		prebuiltDataDir:      l.cstr(t.PrebuiltDataDir),
		stagingDir:           l.cstr(t.StagingDir),
	}
	// librime distinguishes NULL (temp dir) from "" (stderr only) for log_dir.
	logDir := append([]byte(t.LogDir), 0)
	l.keep = append(l.keep, logDir)
	l.traits.logDir = uintptr(unsafe.Pointer(&logDir[0]))
	if len(t.Modules) > 0 {
		l.mods = l.mods[:0]
		for _, m := range t.Modules {
			l.mods = append(l.mods, l.cstr(m))
		}
		l.mods = append(l.mods, 0)
		l.traits.modules = uintptr(unsafe.Pointer(&l.mods[0]))
	}
}

// Setup must be called once before Initialize.
func (l *Lib) Setup(t Traits) {
	l.fillTraits(t)
	purego.SyscallN(l.api.setup, uintptr(unsafe.Pointer(&l.traits)))
	runtime.KeepAlive(l)
}

// Initialize starts the librime service with the traits given to Setup.
func (l *Lib) Initialize() {
	purego.SyscallN(l.api.initialize, uintptr(unsafe.Pointer(&l.traits)))
	runtime.KeepAlive(l)
}

// Finalize stops the service and unloads modules.
func (l *Lib) Finalize() { purego.SyscallN(l.api.finalize) }

// InitializeDeployer loads only the deployment service. It does not start
// maintenance (which can upgrade user dictionaries) or create sessions.
func (l *Lib) InitializeDeployer() {
	purego.SyscallN(l.api.deployerInitialize, uintptr(unsafe.Pointer(&l.traits)))
	runtime.KeepAlive(l)
}

// DeploySchema compiles a schema and its dictionaries without opening a
// user dictionary. The caller supplies a private scratch user directory.
func (l *Lib) DeploySchema(path string) bool {
	b := append([]byte(path), 0)
	ok := ctrue(l.call(l.api.deploySchema, uintptr(unsafe.Pointer(&b[0]))))
	runtime.KeepAlive(b)
	return ok
}

// DeployConfig compiles a configuration file using its version key.
func (l *Lib) DeployConfig(name, versionKey string) bool {
	n, v := append([]byte(name), 0), append([]byte(versionKey), 0)
	ok := ctrue(l.call(l.api.deployConfigFile, uintptr(unsafe.Pointer(&n[0])), uintptr(unsafe.Pointer(&v[0]))))
	runtime.KeepAlive(n)
	runtime.KeepAlive(v)
	return ok
}

// StartMaintenance schedules a deploy on librime's maintenance thread.
// With fullCheck false it only runs when data files changed.
func (l *Lib) StartMaintenance(fullCheck bool) bool {
	return ctrue(l.call(l.api.startMaintenance, b2u(fullCheck)))
}

// JoinMaintenanceThread waits for a running deploy to finish.
func (l *Lib) JoinMaintenanceThread() { purego.SyscallN(l.api.joinMaintenanceThread) }

// HasModule reports whether librime has the module name (built in or
// merged), e.g. "shixin" in our own build.
func (l *Lib) HasModule(name string) bool {
	b := append([]byte(name), 0)
	r := l.call(l.api.findModule, uintptr(unsafe.Pointer(&b[0])))
	runtime.KeepAlive(b)
	return r != 0
}

// Version returns the librime version string.
func (l *Lib) Version() string {
	r1, _, _ := purego.SyscallN(l.api.getVersion)
	return string(cbytes(r1, nil))
}

// Session is a librime input session with reusable result buffers.
type Session struct {
	lib    *Lib
	id     uintptr
	ctx    cContext
	commit cCommit
	status cStatus
}

// CreateSession creates a session on the current default schema.
func (l *Lib) CreateSession() (*Session, error) {
	r1, _, _ := purego.SyscallN(l.api.createSession)
	if r1 == 0 {
		return nil, errors.New("rime: create_session failed")
	}
	return &Session{lib: l, id: r1}, nil
}

// Destroy ends the session.
func (s *Session) Destroy() {
	s.lib.call(s.lib.api.destroySession, s.id)
	s.id = 0
}

// ProcessKey feeds an X11 keysym with Rime modifier mask and reports whether
// librime handled it.
func (s *Session) ProcessKey(keycode, mask int) bool {
	return ctrue(s.lib.call(s.lib.api.processKey, s.id, uintptr(keycode), uintptr(mask)))
}

// SelectCandidateOnCurrentPage selects the index-th candidate of the page.
func (s *Session) SelectCandidateOnCurrentPage(index int) bool {
	return ctrue(s.lib.call(s.lib.api.selectCandidateOnCurrentPage, s.id, uintptr(index)))
}

// ChangePage flips the candidate page.
func (s *Session) ChangePage(backward bool) bool {
	return ctrue(s.lib.call(s.lib.api.changePage, s.id, b2u(backward)))
}

// ClearComposition discards the current input.
func (s *Session) ClearComposition() { s.lib.call(s.lib.api.clearComposition, s.id) }

// SimulateKeySequence feeds a key sequence in Rime notation, e.g. "nihao{space}".
// Not for the hot path: it allocates a C string.
func (s *Session) SimulateKeySequence(seq string) bool {
	b := append([]byte(seq), 0)
	ok := ctrue(s.lib.call(s.lib.api.simulateKeySequence, s.id, uintptr(unsafe.Pointer(&b[0]))))
	runtime.KeepAlive(b)
	return ok
}

// SetOption sets a switch such as "ascii_mode". Not for the hot path.
func (s *Session) SetOption(name string, on bool) {
	b := append([]byte(name), 0)
	s.lib.call(s.lib.api.setOption, s.id, uintptr(unsafe.Pointer(&b[0])), b2u(on))
	runtime.KeepAlive(b)
}

// Option reports whether a switch such as "ascii_mode" is on. Not for the
// hot path.
func (s *Session) Option(name string) bool {
	b := append([]byte(name), 0)
	on := ctrue(s.lib.call(s.lib.api.getOption, s.id, uintptr(unsafe.Pointer(&b[0]))))
	runtime.KeepAlive(b)
	return on
}

// SetProperty sets a context property, which librime-shixin components
// read. Not for the hot path.
func (s *Session) SetProperty(name, value string) {
	n, v := append([]byte(name), 0), append([]byte(value), 0)
	s.lib.call(s.lib.api.setProperty, s.id, uintptr(unsafe.Pointer(&n[0])), uintptr(unsafe.Pointer(&v[0])))
	runtime.KeepAlive(n)
	runtime.KeepAlive(v)
}

// AttachRerank registers the engine's 智能排序 slots with librime-shixin
// (DESIGN §7.2): count slots of size bytes at base, which must stay mapped
// until AttachRerank(nil, 0, 0) detaches them. It reports false if librime
// has no shixin_rerank or its slot layout differs.
func (l *Lib) AttachRerank(base unsafe.Pointer, count, size int) bool {
	sym, err := purego.Dlsym(l.handle, "shixin_rerank_attach")
	if err != nil {
		return false
	}
	return l.call(sym, uintptr(base), uintptr(count), uintptr(size)) != 0
}

// SelectSchema switches the session to schema id. Not for the hot path.
func (s *Session) SelectSchema(id string) bool {
	b := append([]byte(id), 0)
	ok := ctrue(s.lib.call(s.lib.api.selectSchema, s.id, uintptr(unsafe.Pointer(&b[0]))))
	runtime.KeepAlive(b)
	return ok
}

// ReadCommit appends pending commit text to dst[:0]. ok is false when there
// is nothing to commit.
func (s *Session) ReadCommit(dst []byte) (out []byte, ok bool) {
	s.commit = cCommit{dataSize: int32(unsafe.Sizeof(cCommit{}) - unsafe.Sizeof(int32(0)))}
	if !ctrue(s.lib.call(s.lib.api.getCommit, s.id, uintptr(unsafe.Pointer(&s.commit)))) {
		return dst[:0], false
	}
	out = cbytes(s.commit.text, dst[:0])
	s.lib.call(s.lib.api.freeCommit, uintptr(unsafe.Pointer(&s.commit)))
	return out, true
}

// Candidate is one menu entry. Text and Comment alias Context's buffer and
// are valid until the next ReadContext into the same Context.
type Candidate struct {
	Text    []byte
	Comment []byte
}

// Context is the composition and menu state, reused across keys.
type Context struct {
	Preedit     []byte
	CursorPos   int
	SelStart    int
	SelEnd      int
	PageSize    int
	PageNo      int
	IsLastPage  bool
	Highlighted int
	Candidates  []Candidate

	buf  []byte
	offs []int
}

// ReadContext fills dst with the current state. ok is false if the session
// is gone.
func (s *Session) ReadContext(dst *Context) bool {
	s.ctx = cContext{dataSize: int32(unsafe.Sizeof(cContext{}) - unsafe.Sizeof(int32(0)))}
	if !ctrue(s.lib.call(s.lib.api.getContext, s.id, uintptr(unsafe.Pointer(&s.ctx)))) {
		return false
	}
	c := &s.ctx
	dst.Preedit = cbytes(c.composition.preedit, dst.Preedit[:0])
	dst.CursorPos = int(c.composition.cursorPos)
	dst.SelStart = int(c.composition.selStart)
	dst.SelEnd = int(c.composition.selEnd)
	dst.PageSize = int(c.menu.pageSize)
	dst.PageNo = int(c.menu.pageNo)
	dst.IsLastPage = c.menu.isLastPage != 0
	dst.Highlighted = int(c.menu.highlightedCandidateIndex)

	n := int(c.menu.numCandidates)
	dst.buf = dst.buf[:0]
	dst.offs = dst.offs[:0]
	if n > 0 && c.menu.candidates != 0 {
		cands := unsafe.Slice((*cCandidate)(cptr(c.menu.candidates)), n)
		for i := range cands {
			dst.offs = append(dst.offs, len(dst.buf))
			dst.buf = cbytes(cands[i].text, dst.buf)
			dst.offs = append(dst.offs, len(dst.buf))
			dst.buf = cbytes(cands[i].comment, dst.buf)
		}
	}
	dst.offs = append(dst.offs, len(dst.buf))
	if cap(dst.Candidates) < n {
		dst.Candidates = make([]Candidate, n)
	}
	dst.Candidates = dst.Candidates[:n]
	for i := 0; i < n; i++ {
		o := dst.offs[2*i:]
		dst.Candidates[i] = Candidate{Text: dst.buf[o[0]:o[1]:o[1]], Comment: dst.buf[o[1]:o[2]:o[2]]}
	}
	s.lib.call(s.lib.api.freeContext, uintptr(unsafe.Pointer(&s.ctx)))
	return true
}

// Status is the session's mode, reused across keys.
type Status struct {
	SchemaID      []byte
	IsDisabled    bool
	IsComposing   bool
	IsASCIIMode   bool
	IsFullShape   bool
	IsSimplified  bool
	IsTraditional bool
	IsASCIIPunct  bool
}

// ReadStatus fills dst with the current mode.
func (s *Session) ReadStatus(dst *Status) bool {
	s.status = cStatus{dataSize: int32(unsafe.Sizeof(cStatus{}) - unsafe.Sizeof(int32(0)))}
	if !ctrue(s.lib.call(s.lib.api.getStatus, s.id, uintptr(unsafe.Pointer(&s.status)))) {
		return false
	}
	st := &s.status
	dst.SchemaID = cbytes(st.schemaID, dst.SchemaID[:0])
	dst.IsDisabled = st.isDisabled != 0
	dst.IsComposing = st.isComposing != 0
	dst.IsASCIIMode = st.isASCIIMode != 0
	dst.IsFullShape = st.isFullShape != 0
	dst.IsSimplified = st.isSimplified != 0
	dst.IsTraditional = st.isTraditional != 0
	dst.IsASCIIPunct = st.isASCIIPunct != 0
	s.lib.call(s.lib.api.freeStatus, uintptr(unsafe.Pointer(&s.status)))
	return true
}

// ctrue reads a C Bool (int) return value. Only the low 32 bits are defined.
func ctrue(r1 uintptr) bool { return int32(r1) != 0 }

func b2u(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// cptr converts an address of C memory to a pointer without tripping vet's
// unsafeptr check; the memory is never managed by the Go GC.
func cptr(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// cbytes appends the NUL-terminated C string at p to dst.
func cbytes(p uintptr, dst []byte) []byte {
	if p == 0 {
		return dst
	}
	base := cptr(p)
	n := 0
	for *(*byte)(unsafe.Add(base, n)) != 0 {
		n++
	}
	return append(dst, unsafe.Slice((*byte)(base), n)...)
}
