package engine

import (
	"testing"

	"shixin/internal/ipc"
)

func TestSharedModeAcrossContexts(t *testing.T) {
	e := testEngine(t)
	e.SetApps(AppModes{})
	cl := dial(t, e)
	focus := func(ctx uint32, app string) *ipc.Result {
		cl.send((&ipc.FocusIn{Ctx: ctx, App: []byte(app)}).Append(cl.buf[:0]))
		return cl.result()
	}
	set := func(ctx uint32, english bool) {
		cl.seq++
		cl.send((&ipc.SetOption{Ctx: ctx, Seq: cl.seq, Name: []byte("ascii_mode"), On: english}).Append(cl.buf[:0]))
		cl.result()
	}
	focus(1, "first")
	set(1, true)
	if r := focus(2, "second"); r.Flags&ipc.ResultASCII == 0 {
		t.Fatal("new app context did not inherit the front's current mode")
	}
	set(2, false)
	if r := focus(1, "first"); r.Flags&ipc.ResultASCII != 0 {
		t.Fatal("remembering was off but returning to an app restored its stale mode")
	}
	set(1, true)
	other := dial(t, e)
	if other.res.Flags&ipc.ResultASCII != 0 {
		t.Fatal("an independent connection inherited another front's mode")
	}
}

func TestSecureReassignedContextDoesNotOverwriteRememberedMode(t *testing.T) {
	e := testEngine(t)
	e.SetApps(AppModes{Remember: true})
	t.Cleanup(func() { e.SetApps(AppModes{}) })
	cl := dial(t, e)
	focus := func(ctx uint32, app string, secure bool) *ipc.Result {
		m := ipc.FocusIn{Ctx: ctx, App: []byte(app)}
		if secure {
			m.Flags = ipc.FocusSecure
		}
		cl.send(m.Append(cl.buf[:0]))
		return cl.result()
	}
	set := func(ctx uint32, english bool) {
		cl.seq++
		cl.send((&ipc.SetOption{Ctx: ctx, Seq: cl.seq, Name: []byte("ascii_mode"), On: english}).Append(cl.buf[:0]))
		cl.result()
	}
	focus(1, "reassigned-A", false)
	set(1, true)
	focus(2, "reassigned-B", false)
	focus(1, "reassigned-X", true) // slot 1 still holds A's pre-secure mode
	focus(2, "reassigned-A", false)
	set(2, false)
	cl.send((&ipc.FocusOut{Ctx: 2}).Append(cl.buf[:0]))
	focus(1, "reassigned-X", false)
	if r := focus(2, "reassigned-A", false); r.Flags&ipc.ResultASCII != 0 {
		t.Fatal("leaving the reassigned secure field overwrote A's newer remembered mode")
	}
}
