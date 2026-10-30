package main

import (
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Auto spacing's macOS sources (DESIGN §7.1): the events that reset it, and
// reading the character before the caret from the app.

var (
	selMarkedRange      = objc.RegisterName("markedRange")
	selSelectedRange    = objc.RegisterName("selectedRange")
	selAttrSubstring    = objc.RegisterName("attributedSubstringFromRange:")
	selString           = objc.RegisterName("string")
	selLength           = objc.RegisterName("length")
	selCharacterAtIndex = objc.RegisterName("characterAtIndex:")

	poolPush, poolPop uintptr // objc_autoreleasePool{Push,Pop}
	poolArgs          [1]uintptr

	resetBlock, activationBlock objc.Block // kept alive: AppKit holds copies
	distCallback                uintptr    // CFNotificationCallback for distributed notifications
)

// send2 is send for methods returning a two-word struct (NSRange), which
// comes back in two registers.
func send2(id uintptr, sel objc.SEL, a ...uintptr) (uintptr, uintptr) {
	msgArgs[0], msgArgs[1] = id, uintptr(sel)
	n := 2 + copy(msgArgs[2:], a)
	r1, r2, _ := purego.SyscallN(msgSend, msgArgs[:n]...)
	return r1, r2
}

// TextBefore implements client.TextReader: the character before the
// caret, or before the marked text while composing. The spacer calls it at
// most once per boundary and stops for an app that is slow or can't answer.
func (frontApplier) TextBefore() (rune, bool) {
	if imkClient == 0 {
		return 0, false
	}
	pool, _, _ := purego.SyscallN(poolPush)
	r, ok := textBefore()
	poolArgs[0] = pool
	purego.SyscallN(poolPop, poolArgs[:]...)
	return r, ok
}

func textBefore() (rune, bool) {
	loc, n := send2(imkClient, selMarkedRange)
	if loc == nsNotFound || n == 0 {
		loc, _ = send2(imkClient, selSelectedRange)
	}
	switch {
	case loc == nsNotFound:
		return 0, false
	case loc == 0:
		return 0, true
	}
	c, ok := charAt(loc - 1)
	if !ok {
		return 0, false
	}
	if utf16.IsSurrogate(rune(c)) {
		hi, ok := uint16(0), false
		if loc >= 2 {
			hi, ok = charAt(loc - 2)
		}
		r := utf16.DecodeRune(rune(hi), rune(c))
		return r, ok && r != 0xfffd
	}
	return rune(c), true
}

// charAt reads one UTF-16 unit of the client's text.
func charAt(i uintptr) (uint16, bool) {
	as := send(imkClient, selAttrSubstring, i, 1) // NSRange{i, 1}
	if as == 0 {
		return 0, false
	}
	s := send(as, selString)
	if s == 0 || send(s, selLength) == 0 {
		return 0, false
	}
	return uint16(send(s, selCharacterAtIndex, 0)), true
}

// Observer tokens for distributed notifications; CF passes them back.
const (
	observeLock = iota + 1
	observeSource
)

// installSpacingResets subscribes to everything that may move the caret or
// change the text without a key reaching Shixin. Reset handlers set a flag
// (client.ResetSpacing); an input-source switch also finishes marked text.
// Global mouse monitors need no permission; key monitors would. Main thread,
// once.
func installSpacingResets() {
	resetBlock = objc.NewBlock(func(objc.Block, objc.ID) {
		core.ResetSpacing()
		dismissModeBubble()
	})
	activationBlock = objc.NewBlock(func(objc.Block, objc.ID) {
		core.ResetSpacing()
		dismissModeBubble()
		secureOwnerReset.Store(true)
	})

	// Left, right and other mouse-down anywhere: in the text, another
	// window, a menu, the Dock. Never mouse moves.
	const mouseDown = 1<<1 | 1<<3 | 1<<25
	objc.ID(objc.GetClass("NSEvent")).Send(objc.RegisterName("addGlobalMonitorForEventsMatchingMask:handler:"), uint64(mouseDown), resetBlock)

	observe := func(center objc.ID, name string) {
		block := resetBlock
		if name == "NSWorkspaceDidActivateApplicationNotification" {
			block = activationBlock
		}
		center.Send(objc.RegisterName("addObserverForName:object:queue:usingBlock:"), objc.ID(cstring(name)), objc.ID(0), objc.ID(0), block)
	}
	// The AppKit constants' values are their names.
	ws := objc.ID(objc.GetClass("NSWorkspace")).Send(objc.RegisterName("sharedWorkspace")).Send(objc.RegisterName("notificationCenter"))
	for _, name := range []string{
		"NSWorkspaceDidActivateApplicationNotification",
		"NSWorkspaceActiveSpaceDidChangeNotification",
		"NSWorkspaceScreensDidSleepNotification",
		"NSWorkspaceWillSleepNotification",
		"NSWorkspaceSessionDidResignActiveNotification",
	} {
		observe(ws, name)
	}
	// Distributed notifications go through CF to ask for immediate
	// delivery: the default behavior (and the block API's) holds them while
	// the process is inactive, and an input method's process always is.
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	carbon, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	getCenter, err := purego.Dlsym(cf, "CFNotificationCenterGetDistributedCenter")
	must(err)
	addObserver, err := purego.Dlsym(cf, "CFNotificationCenterAddObserver")
	must(err)
	sourceChanged, err := purego.Dlsym(carbon, "kTISNotifySelectedKeyboardInputSourceChanged")
	must(err)
	tis := loadTIS()
	distCallback = purego.NewCallback(func(center, observer, name, object, info uintptr) uintptr {
		core.ResetSpacing()
		dismissModeBubble()
		// macOS 26 doesn't call deactivateServer: when another process
		// switches the input source (TISSelectInputSource): finish the
		// composition here, as Squirrel's finalizeStrandedComposition
		// does. A menu switch has already deactivated, and finishInput
		// does nothing then.
		if observer == observeSource && tis.current() != inputMode {
			finishInput()
		}
		return 0
	})
	center, _, _ := purego.SyscallN(getCenter)
	const deliverImmediately = 4 // CFNotificationSuspensionBehaviorDeliverImmediately
	for _, o := range []struct{ observer, name uintptr }{
		{observeLock, cstring("com.apple.screenIsLocked")},
		{observeSource, **(**uintptr)(unsafe.Pointer(&sourceChanged))}, // the constant's CFString
	} {
		purego.SyscallN(addObserver, center, o.observer, distCallback, o.name, 0, deliverImmediately)
	}
}
