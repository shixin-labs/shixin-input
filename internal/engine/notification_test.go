package engine

import (
	"fmt"
	"testing"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

func TestPassedNotificationsNeverProduceText(t *testing.T) {
	cl := dial(t, testEngine(t))
	cl.setASCII(false)
	for _, key := range []uint32{',', 'x'} {
		cl.seq++
		cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: key, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
		r := cl.result()
		if len(r.Commit) != 0 || len(r.Preedit) != 0 || r.Flags&ipc.ResultComposing != 0 {
			t.Fatalf("passed key %q produced input", rune(key))
		}
	}
	cl.typ("nihao")
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: '1', Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
	if r := cl.result(); len(r.Commit) != 0 || r.Flags&ipc.ResultComposing == 0 {
		t.Fatal("passed digit selected or lost the composition")
	}
	cl.key(policy.KeyEscape, 0)
}

func notifyPassedKey(cl *client, key, mods uint32) *ipc.Result {
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: key, Mods: mods, Flags: ipc.KeyNotify}).Append(cl.buf[:0]))
	return cl.result()
}

func setNotificationOption(cl *client, name string, on bool) {
	cl.seq++
	cl.send((&ipc.SetOption{Ctx: 1, Seq: cl.seq, Name: []byte(name), On: on}).Append(cl.buf[:0]))
	cl.result()
}

func TestPassedNotificationsIdleModes(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, punct := range []bool{false, true} {
			for _, full := range []bool{false, true} {
				t.Run(fmt.Sprintf("english=%v/punctuation=%v/fullwidth=%v", ascii, punct, full), func(t *testing.T) {
					cl := dial(t, testEngine(t))
					cl.setASCII(ascii)
					setNotificationOption(cl, "ascii_punct", punct)
					setNotificationOption(cl, "full_shape", full)
					for key := uint32(0x20); key <= 0x7e; key++ {
						r := notifyPassedKey(cl, key, 0)
						if len(r.Commit) != 0 || len(r.Preedit) != 0 || r.Flags&(ipc.ResultComposing|ipc.ResultHandled) != 0 {
							t.Fatalf("passed key %q produced input or was handled: flags=%#x", rune(key), r.Flags)
						}
						if (r.Flags&ipc.ResultASCII != 0) != ascii || (r.Flags&ipc.ResultASCIIPunct != 0) != punct || (r.Flags&ipc.ResultFullShape != 0) != full {
							t.Fatalf("passed key %q changed the session's modes", rune(key))
						}
					}
				})
			}
		}
	}
}

func TestPassedNotificationsCannotSelectOrEditComposition(t *testing.T) {
	cl := dial(t, testEngine(t))
	cl.setASCII(false)
	r := cl.typ("nihao")
	preedit, cursor, highlighted, page := string(r.Preedit), r.Cursor, r.Highlighted, r.PageNo
	texts := make([]string, len(r.Candidates))
	for i, c := range r.Candidates {
		texts[i] = string(c.Text)
	}
	for key := uint32(0x20); key <= 0x7e; key++ {
		r := notifyPassedKey(cl, key, 0)
		if len(r.Commit) != 0 || r.Flags&ipc.ResultComposing == 0 || r.Flags&ipc.ResultHandled != 0 || string(r.Preedit) != preedit || r.Cursor != cursor || r.Highlighted != highlighted || r.PageNo != page {
			t.Fatalf("passed key %q selected, changed or committed the composition", rune(key))
		}
		if len(r.Candidates) != len(texts) {
			t.Fatalf("passed key %q changed the menu", rune(key))
		}
		for i, c := range r.Candidates {
			if string(c.Text) != texts[i] {
				t.Fatalf("passed key %q changed candidate %d", rune(key), i)
			}
		}
	}
	if got := string(cl.key(' ', 0).Commit); got != texts[0] {
		t.Fatal("notification handling damaged the eventual selection")
	}
}
