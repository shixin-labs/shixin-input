package engine

import (
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
	"shixin/internal/settings"
)

func shiCode(scheme string) string {
	switch scheme {
	case "rime_ice":
		return "shi"
	case "double_pinyin_abc":
		return "vi"
	case "double_pinyin_ziguang", "double_pinyin_jiajia":
		return "ii"
	default:
		return "ui"
	}
}

// checkCandidateShortcuts checks page-relative selection, independence from
// the highlight, idle/English pass-through and recognizer input. Called by
// the real-rime-ice tests for every scheme and both shortcut presets.
func checkCandidateShortcuts(t *testing.T, cl *client, preset, input string, native bool) {
	t.Helper()
	keys, ok := settings.CandidateKeyPair(preset)
	if !ok {
		t.Fatal("missing candidate shortcut preset")
	}
	for _, ascii := range []bool{false, true} {
		cl.setASCII(ascii)
		for _, repr := range keys {
			key, mods, ok := parseKey(repr)
			if !ok {
				t.Fatal(repr)
			}
			if got := cl.policy().Lookup(key, mods); got.Disposition() != policy.Pass {
				t.Errorf("idle ascii=%v %s: %v, want PASS", ascii, repr, got)
			}
			if r := cl.key(key, mods); r.Flags&ipc.ResultHandled != 0 || len(r.Commit) != 0 || len(r.Preedit) != 0 {
				t.Errorf("idle ascii=%v %s was consumed", ascii, repr)
			}
		}
	}
	cl.setASCII(false)
	for _, page := range []int{0, 1} {
		for i, repr := range keys {
			r := cl.typ(input)
			if page == 1 {
				r = cl.key(policy.KeyNext, 0)
			}
			if int(r.PageNo) != page || len(r.Candidates) < 4 {
				t.Fatalf("page %d: got %d with %d candidates", page, r.PageNo, len(r.Candidates))
			}
			want := string(r.Candidates[i+1].Text)
			for range 3 {
				cl.key(policy.KeyDown, 0)
			}
			key, mods, _ := parseKey(repr)
			if got := cl.policy().Lookup(key, mods); got.Disposition() != policy.Ask {
				t.Errorf("composing %s: %v, want ASK", repr, got)
			}
			if r := cl.key(key, mods); string(r.Commit) != want {
				t.Fatalf("page %d %s: commit %q, want %q", page, repr, r.Commit, want)
			}
		}
	}
	if !raceEnabled {
		key, mods, _ := parseKey(keys[0])
		cycle := func() {
			for i := 0; i <= len(input); i++ {
				k, m := key, mods
				if i < len(input) {
					k, m = uint32(input[i]), 0
				}
				cl.seq++
				cl.c.Write((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: k, Mods: m}).Append(cl.buf[:0]))
				for {
					typ, payload, err := cl.r.Next()
					if err != nil {
						t.Fatal(err)
					}
					if typ == ipc.TypeResult {
						if err := cl.res.Decode(payload); err != nil {
							t.Fatal(err)
						}
						break
					}
				}
			}
		}
		if n := testing.AllocsPerRun(20, cycle); n != 0 {
			t.Errorf("%s shortcut cycle: %v Go allocations", preset, n)
		}
	}
	if !native {
		return
	}
	for _, input := range []string{"R123", "cC6*7", "U1f600"} {
		for i, repr := range keys {
			r := cl.typ(input)
			preedit := string(r.Preedit)
			want := ""
			if len(r.Candidates) > i+1 {
				want = string(r.Candidates[i+1].Text)
			}
			key, mods, _ := parseKey(repr)
			r = cl.key(key, mods)
			if string(r.Commit) != want || want == "" && string(r.Preedit) != preedit {
				t.Errorf("%s %s: commit %q, want %q; preedit %q, was %q", input, repr, r.Commit, want, r.Preedit, preedit)
			}
			cl.key(policy.KeyEscape, 0)
		}
	}
}
