package llama

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
	"unsafe"
)

// testLib loads build/llama/lib ($SHIXIN_LLAMA overrides), or skips.
func testLib(t testing.TB) *Lib {
	dir := os.Getenv("SHIXIN_LLAMA")
	if dir == "" {
		dir, _ = filepath.Abs("../../build/llama/lib")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skip("llama.cpp not built; run scripts/build-llama.sh")
	}
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// testModel loads the 8-bit ERNIE model ($SHIXIN_MODEL overrides), or
// skips.
func testModel(t testing.TB) (*Lib, *Model) {
	l := testLib(t)
	path := os.Getenv("SHIXIN_MODEL")
	if path == "" {
		path, _ = filepath.Abs("../../build/models/ernie-4.5-0.3b-base-q8_0.gguf")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("model missing; see DESIGN §7.2")
	}
	dev, ok := l.Accelerator()
	if !ok {
		t.Skip("no GPU")
	}
	m, err := l.LoadModel(path, dev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return l, m
}

// TestParamsLayout reads the library's defaults through the mirrored
// structs: a layout mismatch shows up as wrong values.
func TestParamsLayout(t *testing.T) {
	if s := unsafe.Sizeof(modelParams{}); s != 80 {
		t.Errorf("sizeof(llama_model_params) = %d, want 80", s)
	}
	if s := unsafe.Sizeof(contextParams{}); s != 168 {
		t.Errorf("sizeof(llama_context_params) = %d, want 168", s)
	}
	l := testLib(t)
	m := l.modelDefaultParams()
	if m.nGPULayers != -1 || m.splitMode != 1 || m.loadMode != -1 || m.lazyMode != 1 || m.mainGPU != 0 ||
		m.vocabOnly || m.checkTensors || !m.useExtraBufts || m.noHost || m.noAlloc || m.loadMTP {
		t.Errorf("model defaults read wrong: %+v", m)
	}
	c := l.contextDefaultParams()
	if c.nCtx != 512 || c.nBatch != 2048 || c.nUbatch != 512 || c.nSeqMax != 1 || c.nOutputsMaxPerSeq != 1 ||
		c.ropeScalingType != -1 || c.flashAttnType != -1 || c.yarnExtFactor != -1 || c.defragThold != -1 ||
		c.typeK != 1 || c.typeV != 1 || c.embeddings || !c.offloadKQV || !c.noPerf || !c.opOffload ||
		!c.swaFull || c.kvUnified || c.samplers != 0 || c.ctxOther != 0 {
		t.Errorf("context defaults read wrong: %+v", c)
	}
}

func TestAccelerator(t *testing.T) {
	l := testLib(t)
	d, ok := l.Accelerator()
	if !ok {
		t.Skip("no GPU")
	}
	t.Logf("accelerator: %s (%s)", d.Name, d.Description)
}

// logProb is log p(tok) from a probabilities row.
func logProb(row []float32, tok int32) float64 { return math.Log(float64(row[tok])) }

// TestScoreContinuations scores two words after a context with shared
// prefixes and checks the batched result against decoding each alone.
func TestScoreContinuations(t *testing.T) {
	_, m := testModel(t)
	c, err := m.NewContext(ContextOptions{Tokens: 1024, Batch: 256, Sequences: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := m.Tokenize(nil, []byte("我们明天一起去"))
	opts := [][]int32{m.Tokenize(nil, []byte("台湾旅游")), m.Tokenize(nil, []byte("太晚回家"))}
	t.Logf("context %d tokens, options %v", len(ctx), opts)
	for i, tok := range ctx {
		c.Add(tok, int32(i), 0, i == len(ctx)-1)
	}
	if err := c.Decode(); err != nil {
		t.Fatal(err)
	}
	first := append([]float32(nil), c.Probs(len(ctx)-1)...)
	// Batched: each option in its own sequence, sharing the context.
	L := int32(len(ctx))
	var at [][]int
	for s, o := range opts {
		c.Copy(0, int32(s+1), -1, -1)
		at = append(at, nil)
		for j, tok := range o[:len(o)-1] {
			at[s] = append(at[s], c.Add(tok, L+int32(j), int32(s+1), true))
		}
	}
	if err := c.Decode(); err != nil {
		t.Fatal(err)
	}
	batched := make([]float64, len(opts))
	for s, o := range opts {
		batched[s] = logProb(first, o[0])
		for j := 1; j < len(o); j++ {
			batched[s] += logProb(c.Probs(at[s][j-1]), o[j])
		}
		c.Remove(int32(s+1), -1, -1)
	}
	// One at a time, from a fresh cache.
	for s, o := range opts {
		c.Clear()
		seq := append(append([]int32(nil), ctx...), o...)
		for i, tok := range seq[:len(seq)-1] {
			c.Add(tok, int32(i), 0, i >= len(ctx)-1)
		}
		if err := c.Decode(); err != nil {
			t.Fatal(err)
		}
		var alone float64
		for j := range o {
			alone += logProb(c.Probs(len(ctx)-1+j), o[j])
		}
		if math.Abs(alone-batched[s]) > 0.05 {
			t.Errorf("option %d: batched %.3f, alone %.3f", s, batched[s], alone)
		}
		t.Logf("option %d: log p %.3f", s, alone)
	}
	if !(batched[0] > batched[1]) {
		t.Errorf("我们明天一起去: 台湾旅游 %.2f should beat 太晚回家 %.2f", batched[0], batched[1])
	}
}

// TestProbeLatency measures the scorer's building blocks: a 64-token
// prefill and one batched step for 13 options with 1–3 tokens each.
func TestProbeLatency(t *testing.T) {
	_, m := testModel(t)
	c, err := m.NewContext(ContextOptions{Tokens: 1024, Batch: 64, Sequences: 20})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	text := []byte("光大银行黄鹤楼支行的理财经理测算，结婚生子后的家庭开支会明显增加，所以我们建议年轻人尽早规划自己的财务，比如每个月固定存下一部分收入")
	ctx := m.Tokenize(nil, text)
	if len(ctx) > 64 {
		ctx = ctx[len(ctx)-64:]
	}
	var pre, step []time.Duration
	for range 30 {
		c.Clear()
		t0 := time.Now()
		for i, tok := range ctx {
			c.Add(tok, int32(i), 0, i == len(ctx)-1)
		}
		if err := c.Decode(); err != nil {
			t.Fatal(err)
		}
		_ = c.Probs(len(ctx) - 1)[0]
		pre = append(pre, time.Since(t0))
		t0 = time.Now()
		L := int32(len(ctx))
		rows := 0
		for s := range 13 {
			c.Copy(0, int32(s+4), -1, -1)
			for j := range 1 + s%3 {
				c.Add(ctx[j], L+int32(j), int32(s+4), true)
				rows++
			}
		}
		if err := c.Decode(); err != nil {
			t.Fatal(err)
		}
		for r := range rows {
			_ = c.Probs(r)[0]
		}
		for s := range 13 {
			c.Remove(int32(s+4), -1, -1)
		}
		step = append(step, time.Since(t0))
	}
	pct := func(d []time.Duration) (time.Duration, time.Duration) {
		d = d[5:]
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[len(d)/2], d[len(d)*95/100]
	}
	p50, p95 := pct(pre)
	s50, s95 := pct(step)
	t.Logf("prefill %d tokens: p50 %v p95 %v; 13 options, 26 tokens: p50 %v p95 %v", len(ctx), p50, p95, s50, s95)
}
