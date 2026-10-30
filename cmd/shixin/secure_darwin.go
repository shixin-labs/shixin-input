package main

import (
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	isSecureEventInputEnabled uintptr
	secureOwner               secureOwnerCache
	secureOwnerReset          atomic.Bool // workspace activation may arrive off main
	secureClientBundle        uintptr     // retained NSString; changed with the IMK client
	secureAPI                 struct {
		copySession, dictGet, numberGet, getType, numberType, release uintptr
		ownerKey, workspace                                           uintptr
		pid                                                           int32
	}
	secureArgs      [3]uintptr // reusable purego arguments, main thread only
	selFrontmostApp = objc.RegisterName("frontmostApplication")
	selProcessID    = objc.RegisterName("processIdentifier")
	selStringEqual  = objc.RegisterName("isEqualToString:")
)

// Resolve and warm HIToolbox before accepting keys. No framework loading
// or symbol lookup on the key path.
func loadSecureInput() {
	lib, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	isSecureEventInputEnabled, err = purego.Dlsym(lib, "IsSecureEventInputEnabled")
	must(err)
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	sym := func(h uintptr, name string) uintptr {
		p, err := purego.Dlsym(h, name)
		must(err)
		return p
	}
	secureAPI.copySession = sym(cg, "CGSessionCopyCurrentDictionary")
	secureAPI.dictGet = sym(cf, "CFDictionaryGetValue")
	secureAPI.numberGet = sym(cf, "CFNumberGetValue")
	secureAPI.getType = sym(cf, "CFGetTypeID")
	secureAPI.numberType, _, _ = purego.SyscallN(sym(cf, "CFNumberGetTypeID"))
	secureAPI.release = sym(cf, "CFRelease")
	// One immutable key and the workspace singleton live for the process.
	secureAPI.ownerKey = nsString([]byte("kCGSSessionSecureInputPID"))
	pool, _, _ := purego.SyscallN(poolPush)
	ws := send(uintptr(objc.GetClass("NSWorkspace")), objc.RegisterName("sharedWorkspace"))
	secureAPI.workspace = send(ws, selRetain)
	resolveSecureProcesses() // warm the lookup before accepting keys
	secureCall(poolPop, pool)
	secureInputEnabled()
}

// secureInputEnabled reads the global Secure Event Input signal, not the
// app's text. Check every key and before draining asynchronous results:
// IMK can reuse the proxy and omit activation on a same-app field change.
// Carbon documents this API as not thread safe; call on the main thread.
func secureInputEnabled() bool {
	r, _, _ := purego.SyscallN(isSecureEventInputEnabled)
	return r&0xff != 0 // Carbon Boolean is one byte
}

func secureCall(fn uintptr, args ...uintptr) uintptr {
	n := copy(secureArgs[:], args)
	r, _, _ := purego.SyscallN(fn, secureArgs[:n]...)
	return r
}

// focusedSecureInput guards only the app that owns secure input. The flag
// remains the cheap per-key read; only an enabled flag can resolve PIDs.
func focusedSecureInput() bool {
	if secureOwnerReset.Swap(false) {
		secureOwner.invalidate()
	}
	if !secureInputEnabled() {
		return secureOwner.check(false, imkClient, time.Time{}, nil)
	}
	return secureOwner.check(true, imkClient, time.Now(), resolveSecureProcesses)
}

// setSecureClient records the public IMK bundle identifier on activation
// or a proxy change. Never ask the app for it on a steady-state key.
func setSecureClient(bundle uintptr) {
	prev := secureClientBundle
	secureClientBundle = send(bundle, selRetain)
	if prev != 0 {
		send(prev, selRelease)
	}
	secureOwner.invalidate()
}

// resolveSecureProcesses reads system metadata, never the client's text.
// IMK exposes no public client PID. Validate the frontmost app's bundle
// against the IMK bundle before using its PID (DESIGN §5.5).
func resolveSecureProcesses() secureProcesses {
	pool, _, _ := purego.SyscallN(poolPush)
	var p secureProcesses
	dict, _, _ := purego.SyscallN(secureAPI.copySession)
	if dict != 0 {
		p.owner = secureOwnerPID(dict)
		secureCall(secureAPI.release, dict)
	}
	app := send(secureAPI.workspace, selFrontmostApp)
	bundle := send(app, selBundleID)
	if secureClientBundle != 0 && bundle != 0 && send(bundle, selStringEqual, secureClientBundle)&0xff != 0 {
		p.client = int32(send(app, selProcessID))
	}
	secureCall(poolPop, pool)
	return p
}

func secureOwnerPID(dict uintptr) int32 {
	n := secureCall(secureAPI.dictGet, dict, secureAPI.ownerKey)
	if n == 0 {
		return 0
	}
	typ := secureCall(secureAPI.getType, n)
	if typ != secureAPI.numberType {
		return 0
	}
	secureAPI.pid = 0
	ok := secureCall(secureAPI.numberGet, n, 3, uintptr(unsafe.Pointer(&secureAPI.pid))) // kCFNumberSInt32Type
	if ok&0xff == 0 || secureAPI.pid <= 0 {
		return 0
	}
	return secureAPI.pid
}
