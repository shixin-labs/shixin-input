package engine

import (
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

func TestEnglishPunctuationPolicies(t *testing.T) {
	cl := dial(t, testEngine(t))
	cl.setPunctuation(false)
	if r := cl.key('.', policy.ControlMask); r.Flags&ipc.ResultASCIIPunct == 0 {
		t.Fatal("punctuation shortcut did not enable English punctuation")
	}
	for _, ascii := range []bool{false, true} {
		cl.setASCII(ascii)
		for _, key := range []uint32{',', '.', '/'} {
			if got := cl.policy().Lookup(key, 0); got.Disposition() != policy.Pass {
				t.Errorf("English punctuation, ascii=%v: %q = %v, want PASS", ascii, rune(key), got)
			}
			if r := cl.key(key, 0); r.Flags&ipc.ResultHandled != 0 {
				t.Errorf("English punctuation consumed idle %q", rune(key))
			}
		}
		digit := policy.PassNotify
		if ascii {
			digit = policy.Pass
		}
		if got := cl.policy().Lookup('1', 0); got.Disposition() != digit {
			t.Errorf("English punctuation, ascii=%v: digit = %v, want %v", ascii, got, digit)
		}
		want := policy.Pass
		if !ascii {
			want = policy.Eat
		}
		if got := cl.policy().Lookup('n', 0); got.Disposition() != want {
			t.Errorf("English punctuation, ascii=%v: n = %v, want %v", ascii, got, want)
		}
	}
	cl.setASCII(false)
	composing := cl.policy().Composing
	cl.typ("ni")
	if cl.cur != composing || cl.policy().Lookup('h', 0).Disposition() != policy.Eat {
		t.Error("English punctuation lost the speculative composing policy")
	}
	cl.key(policy.KeyEscape, 0)
}

func TestPunctuationToggleAfterDigit(t *testing.T) {
	e := testEngine(t)
	if !e.Native() {
		// Ubuntu's librime (CI's Linux job) forgets the digit when an
		// option changes; Shixin ships its own librime.
		t.Skip("needs our librime; build it with scripts/build-librime.sh")
	}
	cl := dial(t, e)
	cl.setPunctuation(true)
	cl.setASCII(false)
	if got := cl.policy().Lookup('3', 0).Disposition(); got != policy.PassNotify {
		t.Fatalf("digit = %v, want asynchronous history notification", got)
	}
	cl.key('3', 0)
	cl.key('.', policy.ControlMask)
	cl.key('.', 0)
	if r := cl.key('5', 0); string(r.Commit) != ".5" {
		t.Fatalf("toggle after 3 lost numeric separator history: commit %q", r.Commit)
	}
}

func (cl *client) setPunctuation(on bool) {
	cl.seq++
	cl.send((&ipc.SetOption{Ctx: 1, Seq: cl.seq, On: on, Name: []byte("ascii_punct")}).Append(cl.buf[:0]))
	if r := cl.result(); (r.Flags&ipc.ResultASCIIPunct != 0) != on {
		cl.t.Fatalf("failed to set English punctuation to %v", on)
	}
}

func checkPunctuation(t *testing.T, cl *client, english bool) {
	t.Helper()
	for round := range 3 {
		ascii := english != (round == 1)
		if got := cl.res.Flags&ipc.ResultASCIIPunct != 0; got != ascii {
			t.Fatalf("punctuation round %d: English %v, want %v", round, got, ascii)
		}
		for _, tc := range []struct {
			key  uint32
			text string
		}{{',', "，"}, {'.', "。"}} {
			action := cl.policy().Lookup(tc.key, 0).Disposition()
			r := cl.key(tc.key, 0)
			got, want := string(r.Commit), tc.text
			if ascii {
				want = string(rune(tc.key))
				if action != policy.Pass || r.Flags&ipc.ResultHandled != 0 {
					t.Errorf("English punctuation %q did not pass through", rune(tc.key))
				}
			}
			if r.Flags&ipc.ResultHandled == 0 {
				got = string(rune(tc.key)) // the app inserts a passed key
			}
			if got != want {
				t.Errorf("punctuation %q = %q, want %q", rune(tc.key), got, want)
			}
		}
		if round < 2 {
			cl.key('.', policy.ControlMask)
		}
	}
}
