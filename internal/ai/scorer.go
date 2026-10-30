// Package ai is shixin-ai's scorer for 智能排序 (DESIGN §7.2): it keeps the
// committed text of a few apps prefilled on the GPU, scores candidates as
// continuations of it with pruning, and orders them with the stable
// policy (rerank.Order).
//
// A Scorer is used by one goroutine.
package ai

import (
	"errors"
	"fmt"
	"math"

	"shixin/internal/llama"
	"shixin/internal/rerank"
)

const (
	// ContextTokens is how much of an app's text the model sees.
	ContextTokens = 64
	// maxContextTokens lets a prefilled context grow before it is cut back
	// to ContextTokens, so most commits only decode the new text.
	maxContextTokens = 96
	// WarmApps is how many apps stay prefilled; switching between them
	// needs no new prefill.
	WarmApps = 4
	// Pruning: the options scored in full are the best M first-token
	// bounds plus rime's top RimeTop; the rest keep their bound.
	M       = 6
	RimeTop = 5
	// optionSeqs is how many options get their own sequence per step.
	optionSeqs = M + RimeTop
	batch      = 64
	outputs    = 20
)

// Strengths are the shipped policy settings by strength code (DESIGN
// §7.2, rerank.StrengthCautious 保守 and StrengthStandard 标准).
// Provisional: fitted on the spike's 简拼 items (β, margin) and to be
// refitted on real menus (phase 4).
var Strengths = map[uint8]rerank.Params{
	rerank.StrengthCautious: {Beta: 0.5, Margin: 3, Page: 5},
	rerank.StrengthStandard: {Beta: 0.3, Margin: 0.5, Page: 5},
}

// Candidate is one exported candidate.
type Candidate struct {
	Text   []byte
	Weight float32 // dictionary log weight (rerank.Entry.Weight)
	Flags  uint8   // rerank.EntryFixed, rerank.EntryUser
}

// Scorer holds the model and the warm app contexts.
type Scorer struct {
	model *llama.Model
	ctx   *llama.Context
	apps  [WarmApps]warmApp
	clock uint64

	// Reused per request.
	text  []int32
	toks  [rerank.MaxOptions][]int32
	opts  [rerank.MaxOptions]rerank.Option
	bound [rerank.MaxOptions]float64
	at    [rerank.MaxOptions][]int
}

// warmApp is an app context prefilled in sequence index of its slot.
type warmApp struct {
	app    int // -1: free
	rev    uint32
	tokens []int32
	// next is log P(token | context) for every token, copied from the
	// GPU after the prefill: the first-token bound of every option.
	next []float32
	used uint64
}

// ErrNoAccelerator means llama.cpp found no GPU: the model never runs on
// the CPU, so shixin-ai refuses to start.
var ErrNoAccelerator = errors.New("ai: no supported accelerator")

// New loads the model at path onto the first GPU of lib.
func New(lib *llama.Lib, path string) (*Scorer, error) {
	dev, ok := lib.Accelerator()
	if !ok {
		return nil, ErrNoAccelerator
	}
	m, err := lib.LoadModel(path, dev)
	if err != nil {
		return nil, err
	}
	c, err := m.NewContext(llama.ContextOptions{
		Tokens:    WarmApps*maxContextTokens + optionSeqs*16 + batch,
		Batch:     batch,
		Sequences: WarmApps + optionSeqs,
		Outputs:   outputs,
	})
	if err != nil {
		m.Close()
		return nil, err
	}
	s := &Scorer{model: m, ctx: c}
	for i := range s.apps {
		s.apps[i] = warmApp{app: -1, next: make([]float32, m.VocabSize())}
	}
	return s, nil
}

// Close frees the model.
func (s *Scorer) Close() {
	s.ctx.Close()
	s.model.Close()
}

// Warm reports whether app's context at rev is prefilled.
func (s *Scorer) Warm(app int, rev uint32) bool {
	w := s.find(app)
	return w >= 0 && s.apps[w].rev == rev
}

func (s *Scorer) find(app int) int {
	for i := range s.apps {
		if s.apps[i].app == app {
			return i
		}
	}
	return -1
}

// Prefill makes text, app's committed text at rev, the context its
// candidates are scored after. Text the warm context already holds is
// not decoded again.
func (s *Scorer) Prefill(app int, rev uint32, text []byte) error {
	s.clock++
	w := s.find(app)
	if w < 0 {
		w = 0
		for i := range s.apps {
			if s.apps[i].used < s.apps[w].used {
				w = i
			}
		}
		s.ctx.Remove(int32(w), -1, -1)
		s.apps[w].tokens = s.apps[w].tokens[:0]
	}
	a := &s.apps[w]
	a.app, a.rev, a.used = app, rev, s.clock
	s.text = s.model.Tokenize(s.text[:0], text)
	if len(s.text) == 0 {
		// Nothing written yet: score after the start of a text.
		tok := s.model.BOS()
		if tok < 0 {
			tok = s.model.Newline()
		}
		s.text = append(s.text, tok)
	}
	// Keep the decoded prefix if the new text continues it within the
	// window; otherwise start again from the last ContextTokens.
	keep := 0
	if k := continues(s.text, a.tokens); k >= 0 && len(s.text)-k <= maxContextTokens {
		keep = len(a.tokens)
		s.text = s.text[k:]
	} else {
		s.text = s.text[max(0, len(s.text)-ContextTokens):]
	}
	if keep == len(s.text) && keep > 0 {
		return nil // unchanged
	}
	if keep == 0 {
		s.ctx.Remove(int32(w), -1, -1)
	}
	a.tokens = append(a.tokens[:keep], s.text[keep:]...)
	last := -1
	for i := keep; i < len(a.tokens); i++ {
		last = s.ctx.Add(a.tokens[i], int32(i), int32(w), i == len(a.tokens)-1)
		if s.ctx.Pending() == batch || i == len(a.tokens)-1 {
			if err := s.ctx.Decode(); err != nil {
				a.app = -1
				return err
			}
		}
	}
	probs := s.ctx.Probs(last)
	if probs == nil {
		a.app = -1
		return errors.New("ai: no probabilities from the GPU")
	}
	for i, p := range probs {
		a.next[i] = float32(math.Log(float64(max(p, 1e-38))))
	}
	return nil
}

// continues returns the offset at which old appears in text, so text
// continues it, or -1.
func continues(text, old []int32) int {
	if len(old) == 0 {
		return -1
	}
	for k := 0; k+len(old) <= len(text); k++ {
		match := true
		for j := range old {
			if text[k+j] != old[j] {
				match = false
				break
			}
		}
		if match {
			return k
		}
	}
	return -1
}

// Score orders cands, rime's candidates in rime's order, after app's warm
// context, and writes the order into perm (rerank.Order).
func (s *Scorer) Score(app int, cands []Candidate, p rerank.Params, perm []uint8) ([]uint8, error) {
	n, err := s.score(app, cands, M)
	if err != nil {
		return nil, err
	}
	return rerank.Order(perm, s.opts[:n], p), nil
}

// score fills s.opts for cands, scoring in full rime's top RimeTop and the
// m best first-token bounds.
func (s *Scorer) score(app int, cands []Candidate, m int) (int, error) {
	w := s.find(app)
	if w < 0 {
		return 0, fmt.Errorf("ai: app %d is not prefilled", app)
	}
	a := &s.apps[w]
	s.clock++
	a.used = s.clock
	n := min(len(cands), rerank.MaxOptions)
	L := int32(len(a.tokens))
	// Learned words' weights come from the user dictionary, on another
	// scale: they get the prior of rime's leading dictionary word.
	learned := float32(0)
	for i := range n {
		if cands[i].Flags&(rerank.EntryUser|rerank.EntryFixed) == 0 {
			learned = cands[i].Weight
			break
		}
	}
	// 1. Bound every option by log P(first token), free from the prefill.
	for i := range n {
		c := &cands[i]
		s.toks[i] = s.model.Tokenize(s.toks[i][:0], c.Text)
		prior := c.Weight
		if c.Flags&rerank.EntryUser != 0 {
			prior = learned
		}
		s.opts[i] = rerank.Option{
			Prior: float64(prior),
			User:  c.Flags&rerank.EntryUser != 0,
			Fixed: c.Flags&rerank.EntryFixed != 0 || len(s.toks[i]) == 0,
		}
		s.bound[i] = math.Inf(-1)
		if !s.opts[i].Fixed {
			s.bound[i] = float64(a.next[s.toks[i][0]])
		}
	}
	// 2. Score in full the M best bounds and rime's top RimeTop movable.
	var chosen [rerank.MaxOptions]bool
	top := 0
	for i := 0; i < n && top < RimeTop; i++ {
		if !s.opts[i].Fixed {
			chosen[i] = true
			top++
		}
	}
	for range m {
		best := -1
		for i := range n {
			if !s.opts[i].Fixed && !chosen[i] && (best < 0 || s.bound[i] > s.bound[best]) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		chosen[best] = true
	}
	// One sequence per multi-token option, sharing the context's cells;
	// all their tokens go in one batched step (more only if it overflows).
	seq := int32(WarmApps)
	for i := range n {
		if !chosen[i] {
			continue
		}
		s.opts[i].Scored, s.opts[i].LogP = true, s.bound[i]
		t := s.toks[i]
		s.at[i] = s.at[i][:0]
		if len(t) < 2 {
			continue
		}
		if s.ctx.Pending()+len(t)-1 > outputs || seq == WarmApps+optionSeqs {
			if err := s.step(n, seq); err != nil {
				return 0, err
			}
			seq = WarmApps
		}
		s.ctx.Copy(int32(w), seq, -1, -1)
		for j, tok := range t[:len(t)-1] {
			s.at[i] = append(s.at[i], s.ctx.Add(tok, L+int32(j), seq, true))
		}
		seq++
	}
	return n, s.step(n, seq)
}

// step decodes the pending option tokens and adds their log-probabilities
// to the options whose rows they are, then frees the option sequences.
func (s *Scorer) step(n int, seqs int32) error {
	if s.ctx.Pending() == 0 {
		return nil
	}
	err := s.ctx.Decode()
	for i := range n {
		if err != nil || len(s.at[i]) == 0 {
			continue
		}
		t := s.toks[i]
		for j, at := range s.at[i] {
			probs := s.ctx.Probs(at)
			if probs == nil {
				err = errors.New("ai: no probabilities from the GPU")
				break
			}
			s.opts[i].LogP += math.Log(float64(max(probs[t[j+1]], 1e-38)))
		}
		s.at[i] = s.at[i][:0]
	}
	for q := int32(WarmApps); q < seqs; q++ {
		s.ctx.Remove(q, -1, -1)
	}
	return err
}
