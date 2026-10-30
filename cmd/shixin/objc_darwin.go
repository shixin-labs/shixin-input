package main

import (
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Allocation-free objc_msgSend for integer/pointer arguments, used on the
// key path. objc.ID.Send boxes its arguments in interfaces and allocates.
var (
	msgSend uintptr
	msgArgs [10]uintptr // main thread only
)

func initObjC() {
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	msgSend, err = purego.Dlsym(lib, "objc_msgSend")
	must(err)
	poolPush, err = purego.Dlsym(lib, "objc_autoreleasePoolPush")
	must(err)
	poolPop, err = purego.Dlsym(lib, "objc_autoreleasePoolPop")
	must(err)
	loadSecureInput()
	initDispatch()
}

// send calls objc_msgSend on the main thread.
func send(id uintptr, sel objc.SEL, a ...uintptr) uintptr {
	msgArgs[0], msgArgs[1] = id, uintptr(sel)
	n := 2 + copy(msgArgs[2:], a)
	r, _, _ := purego.SyscallN(msgSend, msgArgs[:n]...)
	return r
}

var (
	selAlloc          = objc.RegisterName("alloc")
	selRelease        = objc.RegisterName("release")
	selRetain         = objc.RegisterName("retain")
	selType           = objc.RegisterName("type")
	selKeyCode        = objc.RegisterName("keyCode")
	selModifierFlags  = objc.RegisterName("modifierFlags")
	selCharacters     = objc.RegisterName("characters")
	selCharsIgnMods   = objc.RegisterName("charactersIgnoringModifiers")
	selUTF8String     = objc.RegisterName("UTF8String")
	selInitWithBytes  = objc.RegisterName("initWithBytes:length:encoding:")
	selInsertText     = objc.RegisterName("insertText:replacementRange:")
	selSetMarkedText  = objc.RegisterName("setMarkedText:selectionRange:replacementRange:")
	selAttributesAt   = objc.RegisterName("attributesForCharacterIndex:lineHeightRectangle:")
	selBundleID       = objc.RegisterName("bundleIdentifier")
	selInitServer     = objc.RegisterName("initWithName:bundleIdentifier:")
	selStringWithUTF8 = objc.RegisterName("stringWithUTF8String:")
	selProcessInfo    = objc.RegisterName("processInfo")
	selBeginActivity  = objc.RegisterName("beginActivityWithOptions:reason:")

	classNSString      = uintptr(objc.GetClass("NSString"))
	classNSProcessInfo = uintptr(objc.GetClass("NSProcessInfo"))
)

const (
	nsUTF8StringEncoding = 4
	nsNotFound           = 1<<63 - 1
)

// nsString returns a +1 NSString for s; the caller releases it.
func nsString(s []byte) uintptr {
	var p uintptr
	if len(s) > 0 {
		p = uintptr(unsafe.Pointer(&s[0]))
	}
	return send(send(classNSString, selAlloc), selInitWithBytes, p, uintptr(len(s)), nsUTF8StringEncoding)
}

// cstring returns an autoreleased NSString (cold path).
func cstring(s string) uintptr {
	b := append([]byte(s), 0)
	return send(classNSString, selStringWithUTF8, uintptr(unsafe.Pointer(&b[0])))
}

// goString copies an NSString (cold path).
func goString(ns uintptr) string {
	if ns == 0 {
		return ""
	}
	p := send(ns, selUTF8String)
	if p == 0 {
		return ""
	}
	return string(cbytes(p))
}

func cbytes(p uintptr) []byte {
	base := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	n := 0
	for *(*byte)(unsafe.Add(base, n)) != 0 {
		n++
	}
	return unsafe.Slice((*byte)(base), n)
}

type nsRect struct{ X, Y, W, H float64 }

// disableAppNap keeps macOS from throttling the front while it serves keys
// (DESIGN R7): NSActivityUserInitiated | NSActivityLatencyCritical.
func disableAppNap() {
	const userInitiated = 0x00FFFFFF
	const latencyCritical = 0xFF00000000
	pi := send(classNSProcessInfo, selProcessInfo)
	act := send(pi, selBeginActivity, userInitiated|latencyCritical, cstring("拾新 serves keystrokes"))
	send(act, selRetain) // held for the life of the process
}

// ---------------------------------------------------------------- dispatch

// Results arrive on a background goroutine; wake schedules client.Drain on
// the main thread with dispatch_async_f. It is asynchronous (the main
// thread may itself be waiting for results in a barrier) and allocates
// nothing, unlike mygo.RunOnMain.
var (
	dispatchAsyncF uintptr
	mainQueue      uintptr
	drainCallback  uintptr
	wakeArgs       [3]uintptr // the connection goroutine is the only caller
)

func initDispatch() {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	dispatchAsyncF, err = purego.Dlsym(lib, "dispatch_async_f")
	must(err)
	mainQueue, err = purego.Dlsym(lib, "_dispatch_main_q") // dispatch_get_main_queue()
	must(err)
	drainCallback = purego.NewCallback(func(ctx uintptr) uintptr {
		core.SetSecure(focusedSecureInput())
		core.Drain()
		return 0
	})
}

func wake() {
	wakeArgs = [3]uintptr{mainQueue, 0, drainCallback}
	purego.SyscallN(dispatchAsyncF, wakeArgs[:]...)
}
