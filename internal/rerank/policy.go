package rerank

import "math"

// MaxOptions bounds how many candidates one order covers (N, DESIGN §7.2).
const MaxOptions = 64

// Option is one of rime's candidates, in rime's order, as the model saw it.
type Option struct {
	// LogP is log P(text | context). Options the scorer pruned have no
	// score and never reach page 1.
	LogP   float64
	Scored bool
	// Prior is the candidate's log weight in rime's dictionary
	// (Entry.Weight; the spike used log(w + 1) of rime-ice's weights).
	Prior float64
	// User marks a learned user_phrase: it drops at most one place.
	User bool
	// Fixed options never move: English, translators, custom phrases.
	Fixed bool
}

// Params are a strength setting (保守, 标准), fitted on the evaluation set
// and shipped with the model.
type Params struct {
	// Beta weighs rime's order: the fused score is
	// LogP + Beta·(Prior − Prior₀), where ₀ is rime's #1.
	Beta float64
	// Margin is how far the best option must lead rime's #1 to replace it.
	Margin float64
	// Guard, when above 0, lets only rime's #1 and options with
	// Prior ≥ Prior₀ + log(Guard), a weight at least Guard times #1's,
	// become #1.
	Guard float64
	// Page is the page size: the slots the model fills.
	Page int
}

// Order writes the new order of opts into perm, as indexes into opts, and
// returns it; perm needs room for len(opts) entries, at most MaxOptions.
//
// The order is "stable" (DESIGN §7.2): the best fused option becomes #1 if
// it leads rime's #1 by the margin, the other page slots go to the best
// remaining scored options in rime's relative order, and everything after
// the page keeps rime's order. Equal scores keep rime's order. Fixed
// options keep their places, and the others are ordered around them.
func Order(perm []uint8, opts []Option, p Params) []uint8 {
	n := min(len(opts), MaxOptions)
	perm = perm[:n]
	var (
		movable [MaxOptions]uint8
		sub     [MaxOptions]Option
		order   [MaxOptions]uint8
	)
	m, page := 0, 0
	for i := range n {
		if !opts[i].Fixed {
			movable[m], sub[m] = uint8(i), opts[i]
			m++
			if i < p.Page {
				page++
			}
		}
	}
	q := p
	q.Page = page
	stable(order[:m], sub[:m], q)
	for i, j := 0, 0; i < n; i++ {
		perm[i] = uint8(i)
		if !opts[i].Fixed {
			perm[i] = movable[order[j]]
			j++
		}
	}
	return perm
}

// stable is Order without fixed options.
func stable(perm []uint8, opts []Option, p Params) {
	n := len(opts)
	for i := range perm {
		perm[i] = uint8(i)
	}
	if n < 2 || !opts[0].Scored {
		return
	}
	var fused [MaxOptions]float64
	for i := range n {
		fused[i] = math.Inf(-1)
		if opts[i].Scored {
			fused[i] = opts[i].LogP + p.Beta*(opts[i].Prior-opts[0].Prior)
		}
	}
	// Ranked by fused score, ties in rime order (insertion sort: n ≤ 64).
	var ranked [MaxOptions]uint8
	for i := range n {
		j := i
		for ; j > 0 && fused[ranked[j-1]] < fused[i]; j-- {
			ranked[j] = ranked[j-1]
		}
		ranked[j] = uint8(i)
	}
	// #1: the best option the guard allows, if it leads by the margin.
	first := uint8(0)
	for _, c := range ranked[:n] {
		if c == 0 || p.Guard <= 0 || opts[c].Prior >= opts[0].Prior+math.Log(p.Guard) {
			first = c
			break
		}
	}
	if fused[first]-fused[0] < p.Margin {
		first = 0
	}
	// The page: first, then the best others while they have a score.
	var onPage [MaxOptions]bool
	onPage[first] = true
	for k, i := 0, 0; k < p.Page-1 && i < n; i++ {
		if c := ranked[i]; c != first && opts[c].Scored {
			onPage[c] = true
			k++
		}
	}
	perm[0] = first
	k := 1
	for _, pass := range [2]bool{true, false} {
		for i := range n {
			if onPage[i] == pass && uint8(i) != first {
				perm[k] = uint8(i)
				k++
			}
		}
	}
	// A learned word drops at most one place. Moving one up only pushes
	// later ones down, so rime order is the right order to fix them in.
	for i := range n {
		if !opts[i].User {
			continue
		}
		at := 0
		for perm[at] != uint8(i) {
			at++
		}
		if at > i+1 {
			copy(perm[i+2:at+1], perm[i+1:at])
			perm[i+1] = uint8(i)
		}
	}
}
