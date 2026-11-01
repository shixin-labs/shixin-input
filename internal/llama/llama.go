// Package llama binds llama.cpp (third_party/llama.cpp, built by
// scripts/build-llama.sh) through its C API with purego, without cgo. It
// holds what shixin-ai needs to score text with a causal language model
// (DESIGN §7.2): loading a model onto the GPU, tokenizing, decoding
// batches over several sequences that share a prefix, and reading logits.
//
// The parameter structs mirror include/llama.h at the pinned submodule
// commit; TestParamsLayout checks them against the library's defaults.
// A Context is not safe for concurrent use.
package llama

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"shixin/internal/dynlib"
)

// Mirrors of llama.h (v0.7.0).
type modelParams struct {
	devices                  uintptr // NULL-terminated ggml_backend_dev_t list
	tensorBuftOverrides      uintptr
	nGPULayers               int32
	splitMode                int32
	loadMode                 int32
	lazyMode                 int32
	mainGPU                  int32
	tensorSplit              uintptr
	progressCallback         uintptr
	progressCallbackUserData uintptr
	kvOverrides              uintptr
	vocabOnly                bool
	checkTensors             bool
	useExtraBufts            bool
	noHost                   bool
	noAlloc                  bool
	loadMTP                  bool
}

type contextParams struct {
	nCtx, nBatch, nUbatch, nSeqMax   uint32
	nRsSeq                           uint32
	nOutputsMax, nOutputsMaxPerSeq   uint32
	nThreads, nThreadsBatch          int32
	ctxType                          int32
	ropeScalingType                  int32
	poolingType                      int32
	attentionType                    int32
	flashAttnType                    int32
	ropeFreqBase, ropeFreqScale      float32
	yarnExtFactor, yarnAttnFactor    float32
	yarnBetaFast, yarnBetaSlow       float32
	yarnOrigCtx                      uint32
	defragThold                      float32
	cbEval, cbEvalUserData           uintptr
	typeK, typeV                     int32
	moeCacheSize                     uintptr
	abortCallback, abortCallbackData uintptr
	embeddings, offloadKQV, noPerf   bool
	opOffload, swaFull, kvUnified    bool
	samplers                         uintptr
	nSamplers                        uintptr
	ctxOther                         uintptr
}

type cBatch struct {
	nTokens int32
	token   uintptr // *int32
	embd    uintptr // *float32
	pos     uintptr // *int32
	nSeqID  uintptr // *int32
	seqID   uintptr // **int32
	logits  uintptr // *int8
}

const (
	splitModeNone = 0
	// ggml_backend_dev_type, but CPU (0)
	devGPU   = 1
	devIGPU  = 2
	devAccel = 3
)

// Lib is a loaded llama.cpp.
type Lib struct {
	handle uintptr

	backendInit             func()
	modelDefaultParams      func() modelParams
	contextDefaultParams    func() contextParams
	modelLoadFromFile       func(path string, p modelParams) uintptr
	modelFree               func(model uintptr)
	initFromModel           func(model uintptr, p contextParams) uintptr
	free                    func(ctx uintptr)
	modelGetVocab           func(model uintptr) uintptr
	modelNLayer             func(model uintptr) int32
	modelSize               func(model uintptr) uint64
	vocabNTokens            func(vocab uintptr) int32
	vocabBOS                func(vocab uintptr) int32
	vocabNL                 func(vocab uintptr) int32
	tokenize                func(vocab uintptr, text *byte, n int32, tokens *int32, max int32, addSpecial, parseSpecial bool) int32
	batchInit               func(n, embd, nSeqMax int32) cBatch
	batchFree               func(b cBatch)
	decode                  func(ctx uintptr, b cBatch) int32
	getSampledProbsIth      func(ctx uintptr, i int32) uintptr
	getSampledProbsCountIth func(ctx uintptr, i int32) uint32
	samplerChainInit        func(p chainParams) uintptr
	samplerChainAdd         func(chain, s uintptr)
	samplerInit             func(iface, ctx uintptr) uintptr
	ggmlSoftMax             func(ctx, a uintptr) uintptr
	samplerFree             func(s uintptr)
	getMemory               func(ctx uintptr) uintptr
	memoryClear             func(mem uintptr, data bool)
	memorySeqRm             func(mem uintptr, seq, p0, p1 int32) bool
	memorySeqCp             func(mem uintptr, src, dst, p0, p1 int32)
	memorySeqPosMax         func(mem uintptr, seq int32) int32
	nCtx                    func(ctx uintptr) uint32
	nSeqMax                 func(ctx uintptr) uint32
	devCount                func() uintptr
	devGet                  func(i uintptr) uintptr
	devType                 func(dev uintptr) int32
	devName                 func(dev uintptr) string
	devDescription          func(dev uintptr) string
}

// Load opens libllama from dir (it finds its libggml* next to itself) and
// initializes the backends.
func Load(dir string) (*Lib, error) {
	// The name the libraries link by first: the app bundle has only that.
	names := []string{"libllama.0.dylib", "libllama.dylib"}
	if runtime.GOOS == "linux" {
		names = []string{"libllama.so.0", "libllama.so"}
	}
	if runtime.GOOS == "windows" {
		names = []string{"llama.dll"}
	}
	var h uintptr
	var err error
	for _, name := range names {
		if h, err = dynlib.Open(filepath.Join(dir, name), false); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("llama: dlopen: %w", err)
	}
	l := &Lib{handle: h}
	for _, f := range []struct {
		fn   any
		name string
	}{
		{&l.backendInit, "llama_backend_init"},
		{&l.modelDefaultParams, "llama_model_default_params"},
		{&l.contextDefaultParams, "llama_context_default_params"},
		{&l.modelLoadFromFile, "llama_model_load_from_file"},
		{&l.modelFree, "llama_model_free"},
		{&l.initFromModel, "llama_init_from_model"},
		{&l.free, "llama_free"},
		{&l.modelGetVocab, "llama_model_get_vocab"},
		{&l.modelNLayer, "llama_model_n_layer"},
		{&l.modelSize, "llama_model_size"},
		{&l.vocabNTokens, "llama_vocab_n_tokens"},
		{&l.vocabBOS, "llama_vocab_bos"},
		{&l.vocabNL, "llama_vocab_nl"},
		{&l.tokenize, "llama_tokenize"},
		{&l.batchInit, "llama_batch_init"},
		{&l.batchFree, "llama_batch_free"},
		{&l.decode, "llama_decode"},
		{&l.getSampledProbsIth, "llama_get_sampled_probs_ith"},
		{&l.getSampledProbsCountIth, "llama_get_sampled_probs_count_ith"},
		{&l.samplerChainInit, "llama_sampler_chain_init"},
		{&l.samplerChainAdd, "llama_sampler_chain_add"},
		{&l.samplerInit, "llama_sampler_init"},
		{&l.ggmlSoftMax, "ggml_soft_max"},
		{&l.samplerFree, "llama_sampler_free"},
		{&l.getMemory, "llama_get_memory"},
		{&l.memoryClear, "llama_memory_clear"},
		{&l.memorySeqRm, "llama_memory_seq_rm"},
		{&l.memorySeqCp, "llama_memory_seq_cp"},
		{&l.memorySeqPosMax, "llama_memory_seq_pos_max"},
		{&l.nCtx, "llama_n_ctx"},
		{&l.nSeqMax, "llama_n_seq_max"},
		{&l.devCount, "ggml_backend_dev_count"},
		{&l.devGet, "ggml_backend_dev_get"},
		{&l.devType, "ggml_backend_dev_type"},
		{&l.devName, "ggml_backend_dev_name"},
		{&l.devDescription, "ggml_backend_dev_description"},
	} {
		sym, err := dynlib.Symbol(h, f.name)
		if err != nil {
			return nil, fmt.Errorf("llama: %s: %w", f.name, err)
		}
		purego.RegisterFunc(f.fn, sym)
	}
	l.backendInit()
	return l, nil
}

// Device is a ggml backend device.
type Device struct {
	ptr               uintptr
	Name, Description string
}

// Accelerator returns the first GPU or accelerator device. ok is false if
// there is none: shixin-ai never runs the model on the CPU.
func (l *Lib) Accelerator() (d Device, ok bool) {
	for i := uintptr(0); i < l.devCount(); i++ {
		dev := l.devGet(i)
		switch l.devType(dev) {
		case devGPU, devIGPU, devAccel:
			return Device{ptr: dev, Name: l.devName(dev), Description: l.devDescription(dev)}, true
		}
	}
	return Device{}, false
}

// Model is a loaded model.
type Model struct {
	l       *Lib
	ptr     uintptr
	vocab   uintptr
	nVocab  int
	devices [2]uintptr // kept alive: llama.cpp reads the list at load
}

// LoadModel loads a GGUF file with every layer on dev. Only dev is offered
// to llama.cpp, so no layer can fall back to the CPU.
func (l *Lib) LoadModel(path string, dev Device) (*Model, error) {
	m := &Model{l: l, devices: [2]uintptr{dev.ptr, 0}}
	p := l.modelDefaultParams()
	p.devices = uintptr(unsafe.Pointer(&m.devices[0]))
	p.nGPULayers = -1
	p.splitMode = splitModeNone
	p.mainGPU = 0
	m.ptr = l.modelLoadFromFile(path, p)
	runtime.KeepAlive(m)
	if m.ptr == 0 {
		return nil, fmt.Errorf("llama: load %s failed", path)
	}
	m.vocab = l.modelGetVocab(m.ptr)
	m.nVocab = int(l.vocabNTokens(m.vocab))
	return m, nil
}

// Close frees the model. Its contexts must be closed first.
func (m *Model) Close() {
	if m.ptr != 0 {
		m.l.modelFree(m.ptr)
		m.ptr = 0
	}
}

// VocabSize is the number of tokens, the length of a logits row.
func (m *Model) VocabSize() int { return m.nVocab }

// Layers is the number of transformer layers.
func (m *Model) Layers() int { return int(m.l.modelNLayer(m.ptr)) }

// Size is the size of the model's tensors in bytes.
func (m *Model) Size() uint64 { return m.l.modelSize(m.ptr) }

// BOS is the beginning-of-text token, or -1.
func (m *Model) BOS() int32 { return m.l.vocabBOS(m.vocab) }

// Newline is the newline token, or -1.
func (m *Model) Newline() int32 { return m.l.vocabNL(m.vocab) }

// Tokenize appends text's tokens to dst, without special tokens.
func (m *Model) Tokenize(dst []int32, text []byte) []int32 {
	if len(text) == 0 {
		return dst
	}
	n := len(dst)
	need := len(text) + 1
	for {
		dst = growTokens(dst, need)
		r := m.l.tokenize(m.vocab, &text[0], int32(len(text)), &dst[n:cap(dst)][0], int32(cap(dst)-n), false, false)
		if r >= 0 {
			return dst[:n+int(r)]
		}
		need = int(-r)
	}
}

func growTokens(dst []int32, n int) []int32 {
	if cap(dst)-len(dst) < n {
		d := make([]int32, len(dst), len(dst)+n)
		copy(d, dst)
		dst = d
	}
	return dst[:len(dst):cap(dst)]
}

// ContextOptions configures a Context.
type ContextOptions struct {
	// Tokens is the KV cache size, shared by all sequences.
	Tokens int
	// Batch is the most tokens one Decode takes.
	Batch int
	// Sequences is the number of sequences.
	Sequences int
	// Outputs is the most tokens per Decode that may have output set;
	// 0 means Batch. Each output reserves a vocabulary-sized row.
	Outputs int
	// Threads is the number of CPU threads llama.cpp may use for host
	// ops; 0 means 1.
	Threads int
}

// Context is an inference context with a KV cache shared by its
// sequences, so a copied prefix shares cells instead of copying them.
//
// Every sequence has a backend sampler, so the softmax of each output runs
// on the GPU along with the model: Probs never normalizes on the CPU.
type Context struct {
	m        *Model
	ptr      uintptr
	mem      uintptr
	batch    cBatch
	cap      int
	n        int
	samplers []samplerSeqConfig
	pin      runtime.Pinner
}

type samplerSeqConfig struct {
	seq     int32
	sampler uintptr
}

type chainParams struct{ noPerf bool }

// samplerI mirrors struct llama_sampler_i.
type samplerI struct {
	name, accept, apply, reset, clone, free  uintptr
	backendInit, backendAccept, backendApply uintptr
	backendSetInput, backendReset, copyState uintptr
}

// samplerData mirrors struct llama_sampler_data.
type samplerData struct {
	logits, probs, sampled, candidates uintptr // ggml_tensor *
}

// softmaxSampler is a backend sampler that only computes the softmax of
// the logits: llama.cpp's own dist sampler also draws a token, and its
// cumulative sum over the vocabulary tripled the decode time on Metal.
// The callbacks run inside llama_decode, on the calling thread, while the
// graph is built; the softmax itself runs on the GPU.
var softmaxSampler struct {
	once  sync.Once
	iface samplerI
	pin   runtime.Pinner
	l     *Lib
}

func (l *Lib) softmaxIface() *samplerI {
	s := &softmaxSampler
	s.once.Do(func() {
		s.l = l
		s.iface = samplerI{
			// The CPU path is required but never used: backendInit
			// always accepts.
			apply: purego.NewCallback(func(smpl, cur uintptr) uintptr { return 0 }),
			backendInit: purego.NewCallback(func(smpl, buft uintptr, nOutputs uint32) uintptr {
				return 1
			}),
			backendApply: purego.NewCallback(func(smpl, ctx, gf, data uintptr) uintptr {
				d := (*samplerData)(cptr(data))
				d.probs = s.l.ggmlSoftMax(ctx, d.logits)
				return 0
			}),
		}
		s.pin.Pin(&s.iface)
	})
	return &s.iface
}

// NewContext creates a context. Everything is offloaded to the model's
// device.
func (m *Model) NewContext(o ContextOptions) (*Context, error) {
	l := m.l
	c := &Context{m: m, cap: o.Batch}
	iface := uintptr(unsafe.Pointer(l.softmaxIface()))
	for i := range o.Sequences {
		chain := l.samplerChainInit(chainParams{noPerf: true})
		l.samplerChainAdd(chain, l.samplerInit(iface, 0))
		c.samplers = append(c.samplers, samplerSeqConfig{seq: int32(i), sampler: chain})
	}
	c.pin.Pin(&c.samplers[0])
	p := l.contextDefaultParams()
	p.nCtx = uint32(o.Tokens)
	p.nBatch, p.nUbatch = uint32(o.Batch), uint32(o.Batch)
	p.nSeqMax = uint32(o.Sequences)
	p.nOutputsMax = uint32(o.Outputs)
	p.nOutputsMaxPerSeq = 0 // every token of a sequence may output
	p.nThreads, p.nThreadsBatch = int32(max(o.Threads, 1)), int32(max(o.Threads, 1))
	p.offloadKQV, p.opOffload = true, true
	p.kvUnified = true
	p.noPerf = true
	p.samplers = uintptr(unsafe.Pointer(&c.samplers[0]))
	p.nSamplers = uintptr(len(c.samplers))
	c.ptr = l.initFromModel(m.ptr, p)
	if c.ptr == 0 {
		c.freeSamplers()
		return nil, errors.New("llama: context creation failed")
	}
	if got := int(l.nSeqMax(c.ptr)); got < o.Sequences {
		c.Close()
		return nil, fmt.Errorf("llama: %d sequences, want %d", got, o.Sequences)
	}
	c.mem = l.getMemory(c.ptr)
	c.batch = l.batchInit(int32(o.Batch), 0, 1)
	return c, nil
}

func (c *Context) freeSamplers() {
	for _, s := range c.samplers {
		c.m.l.samplerFree(s.sampler)
	}
	c.samplers = nil
	c.pin.Unpin()
}

// Close frees the context.
func (c *Context) Close() {
	if c.ptr != 0 {
		c.m.l.batchFree(c.batch)
		c.m.l.free(c.ptr)
		c.ptr = 0
		c.freeSamplers()
	}
}

// Add appends a token at pos in seq to the next batch and returns its
// index there, or -1 when the batch is full. With output set, Decode
// computes its logits.
func (c *Context) Add(token, pos, seq int32, output bool) int {
	if c.n == c.cap {
		return -1
	}
	i := c.n
	unsafe.Slice((*int32)(cptr(c.batch.token)), c.cap)[i] = token
	unsafe.Slice((*int32)(cptr(c.batch.pos)), c.cap)[i] = pos
	unsafe.Slice((*int32)(cptr(c.batch.nSeqID)), c.cap)[i] = 1
	seqs := unsafe.Slice((*uintptr)(cptr(c.batch.seqID)), c.cap)
	*(*int32)(cptr(seqs[i])) = seq
	var out int8
	if output {
		out = 1
	}
	unsafe.Slice((*int8)(cptr(c.batch.logits)), c.cap)[i] = out
	c.n++
	return i
}

// Pending is the number of tokens in the next batch.
func (c *Context) Pending() int { return c.n }

// Decode runs the batch and empties it.
func (c *Context) Decode() error {
	if c.n == 0 {
		return nil
	}
	b := c.batch
	b.nTokens = int32(c.n)
	c.n = 0
	if r := c.m.l.decode(c.ptr, b); r != 0 {
		return fmt.Errorf("llama: decode: %d", r)
	}
	return nil
}

// Probs returns the next-token probabilities after the token at batch
// index i of the last Decode, which must have been added with output set,
// in vocabulary order, computed on the GPU. The slice aliases llama.cpp's
// buffer until the next Decode; it is nil if there are none.
func (c *Context) Probs(i int) []float32 {
	p := c.m.l.getSampledProbsIth(c.ptr, int32(i))
	if p == 0 || int(c.m.l.getSampledProbsCountIth(c.ptr, int32(i))) != c.m.nVocab {
		return nil
	}
	return unsafe.Slice((*float32)(cptr(p)), c.m.nVocab)
}

// Remove drops seq's tokens at positions [p0, p1); -1 is open-ended.
func (c *Context) Remove(seq, p0, p1 int32) bool { return c.m.l.memorySeqRm(c.mem, seq, p0, p1) }

// Copy makes dst share src's tokens at positions [p0, p1).
func (c *Context) Copy(src, dst, p0, p1 int32) { c.m.l.memorySeqCp(c.mem, src, dst, p0, p1) }

// Last is the highest position in seq, or -1 if it is empty.
func (c *Context) Last(seq int32) int32 { return c.m.l.memorySeqPosMax(c.mem, seq) }

// Clear empties every sequence.
func (c *Context) Clear() { c.m.l.memoryClear(c.mem, false) }

// cptr converts an address in C memory, which the Go GC doesn't manage.
func cptr(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }
