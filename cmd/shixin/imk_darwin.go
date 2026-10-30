package main

import (
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"shixin/internal/client"
	"shixin/internal/ipc"
	"shixin/internal/keymap"
	"shixin/internal/perf"
	"shixin/internal/policy"
)

// The IMK controller holds nothing but the client core (DESIGN §5.5):
// handleEvent: classifies the key and returns at once; results are applied
// later through frontApplier, on the main thread.

const (
	nsEventTypeKeyDown      = 10
	nsEventTypeFlagsChanged = 12
	xkVoidSymbol            = 0xffffff
)

// imkClient is the retained IMKTextInput proxy of the focused text field.
var imkClient uintptr

// setClient makes sender the current text field. On a change, or when
// forced (activation), it tells the engine which app has focus.
func setClient(sender uintptr, force bool) {
	if sender == imkClient {
		if force {
			focusModeIndicator(sender)
			focusIn(sender)
		} else {
			core.SetSecure(focusedSecureInput())
		}
		return
	}
	cand.caretValid = false
	focusModeIndicator(sender)
	if imkClient != 0 {
		core.FocusOut() // finish in the old text field before replacing its proxy
		send(imkClient, selRelease)
	}
	imkClient = send(sender, selRetain)
	// A new text field, possibly in an app that never called
	// activateServer: (e.g. after a front restart).
	focusIn(sender)
}

// focusIn tells the engine which app sender's text field is in, and
// whether it is secure. Cold path.
func focusIn(sender uintptr) {
	bundle := send(sender, selBundleID)
	setSecureClient(bundle)
	core.FocusIn(goString(bundle), focusedSecureInput())
}

// handleDur measures handleEvent: the time Shixin holds the app's thread.
var (
	handleDur perf.Hist
	nKeys     int
)

func handleEvent(self objc.ID, cmd objc.SEL, event objc.ID, sender objc.ID) bool {
	t0 := time.Now()
	handled := handleKey(uintptr(event), uintptr(sender))
	handleDur.Add(time.Since(t0))
	if debug {
		logf("debug: event type %d keyCode %d → handled %v", send(uintptr(event), selType), send(uintptr(event), selKeyCode), handled)
	}
	if nKeys++; nKeys%500 == 0 {
		reportLatency()
	}
	return handled
}

// reportLatency logs the key-path histograms (no typed text) and resets
// them. Formatting allocates, so it happens on a goroutine.
func reportLatency() {
	h, st := handleDur, core.Stats()
	handleDur = perf.Hist{}
	core.ResetLatency()
	go logf("latency: handleEvent %v | key→candidates %v | key→applied %v | eaten %d asked %d passed %d barriers %d timeouts %d repairs %d mispredicts %d",
		&h, &st.KeyToCandidates, &st.KeyToApplied, st.Eaten, st.Asked, st.Passed, st.Barriers, st.Timeouts, st.Repairs, st.Mispredicts)
}

func handleKey(ev, sender uintptr) bool {
	setClient(sender, false)
	switch send(ev, selType) {
	case nsEventTypeFlagsChanged:
		return handleModifier(uint16(send(ev, selKeyCode)), uint64(send(ev, selModifierFlags)))
	case nsEventTypeKeyDown:
	default:
		return false
	}
	keyCode := uint16(send(ev, selKeyCode))
	mods := keymap.MacMods(uint64(send(ev, selModifierFlags)))
	chars := selCharacters
	if mods&policy.CommandMask != 0 { // the unmodified key
		chars = selCharsIgnMods
	}
	var ch byte
	if p := send(send(ev, chars), selUTF8String); p != 0 {
		ch = *(*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&p)))
	}
	keysym, ok := keymap.MacKeyDown(keyCode, ch)
	if !ok {
		keysym = xkVoidSymbol // e.g. a non-ASCII character: passes, after the ordering barrier
	}
	return core.HandleKey(keysym, mods)
}

func handleModifier(keyCode uint16, flags uint64) bool {
	keysym, mods, ok := keymap.MacModifierKey(keyCode, flags)
	if !ok {
		// Apps can consume shortcuts before IMK sees their key-down: Safari
		// sends Command's flags changes but not Cmd+A. The shortcut may edit
		// or select text, so discard spacing history even without its key.
		core.ResetSpacing()
		return false
	}
	return core.HandleKey(keysym, mods)
}

func activateServer(self objc.ID, cmd objc.SEL, sender objc.ID) {
	setClient(uintptr(sender), true)
	if debug {
		logf("debug: activate")
	}
}

func deactivateServer(self objc.ID, cmd objc.SEL, sender objc.ID) {
	finishInput()
}

// finishInput is also the input-source notification fallback on macOS 26.
// Releasing the proxy makes a later deactivateServer: harmless.
func finishInput() {
	focusModeIndicator(0)
	if imkClient == 0 {
		return
	}
	core.SetSecure(focusedSecureInput())
	core.FocusOut()
	hideCandidates()
	send(imkClient, selRelease)
	imkClient = 0
}

func commitComposition(self objc.ID, cmd objc.SEL, sender objc.ID) {
	// The app ends the composition (e.g. a click elsewhere in the text).
	setClient(uintptr(sender), false)
	core.CommitComposition()
	hideCandidates()
	dismissModeBubble()
}

func recognizedEvents(self objc.ID, cmd objc.SEL, sender objc.ID) uint {
	return 1<<nsEventTypeKeyDown | 1<<nsEventTypeFlagsChanged
}

var imkServer uintptr // keep alive

func registerIMK() {
	_, err := purego.Dlopen("/System/Library/Frameworks/InputMethodKit.framework/InputMethodKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	_, err = objc.RegisterClass("ShixinInputController", objc.GetClass("IMKInputController"), nil, nil, []objc.MethodDef{
		{Cmd: objc.RegisterName("handleEvent:client:"), Fn: handleEvent},
		{Cmd: objc.RegisterName("activateServer:"), Fn: activateServer},
		{Cmd: objc.RegisterName("deactivateServer:"), Fn: deactivateServer},
		{Cmd: objc.RegisterName("commitComposition:"), Fn: commitComposition},
		{Cmd: objc.RegisterName("recognizedEvents:"), Fn: recognizedEvents},
		{Cmd: objc.RegisterName("menu"), Fn: menuFor},
		{Cmd: objc.RegisterName(actionSettings), Fn: menuOpenSettings},
	})
	must(err)
	imkServer = send(send(uintptr(objc.GetClass("IMKServer")), selAlloc), selInitServer, cstring(connName), cstring(bundleID))
	logf("IMK server ready")
}

// frontApplier applies engine output to the focused app (client.Applier).
type frontApplier struct{}

func (frontApplier) Candidates(r *ipc.Result) {
	showCandidates(r)
	// Queue candidate hiding before the mode bubble evaluates candidate
	// priority, notably when Shift clears a composition into English.
	observeModeResult(r)
}

func (frontApplier) Commit(text []byte) {
	if imkClient == 0 {
		return
	}
	ns := nsString(text)
	send(imkClient, selInsertText, ns, nsNotFound, 0)
	send(ns, selRelease)
}

func (frontApplier) Preedit(text []byte, cursor int) {
	if imkClient == 0 {
		return
	}
	ns := nsString(text)
	sel := utf16Len(text[:min(max(cursor, 0), len(text))])
	// selectionRange {sel, 0}, replacementRange {NSNotFound, 0}
	send(imkClient, selSetMarkedText, ns, uintptr(sel), 0, nsNotFound, 0)
	send(ns, selRelease)
}

func (frontApplier) Status(s client.Status) {
	setStatus(s)
	if !s.Connected || focusedSecureInput() {
		dismissModeBubble()
	}
}

// utf16Len is the length of s in UTF-16 code units, as NSString counts.
func utf16Len(s []byte) int {
	n := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRune(s)
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		s = s[size:]
	}
	return n
}
