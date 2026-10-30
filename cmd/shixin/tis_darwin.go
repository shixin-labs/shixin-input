package main

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Text Input Sources (Carbon HIToolbox) for the installer and diagnostics:
// register the bundle, enable, disable and select its input sources, and
// report their state and the current one.

type tisAPI struct {
	cfURLFromPath, cfStringCreate, cfDictCreate, cfArrayCount, cfArrayGet, cfStringGetCString uintptr
	register, createList, enable, disable, selectSrc, copyCurrent, getProperty                uintptr
	propBundleID, propInputSourceID, propEnabled, propSelectCapable                           uintptr
	propEnableCapable                                                                         uintptr
	cfBooleanGetValue                                                                         uintptr
	cfRelease                                                                                 uintptr
	dictKeyCallbacks, dictValueCallbacks                                                      uintptr
}

func loadTIS() *tisAPI {
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	carbon, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	sym := func(h uintptr, name string) uintptr {
		p, err := purego.Dlsym(h, name)
		must(err)
		return p
	}
	deref := func(p uintptr) uintptr { return **(**uintptr)(unsafe.Pointer(&p)) }
	return &tisAPI{
		cfURLFromPath:      sym(cf, "CFURLCreateFromFileSystemRepresentation"),
		cfStringCreate:     sym(cf, "CFStringCreateWithCString"),
		cfDictCreate:       sym(cf, "CFDictionaryCreate"),
		cfArrayCount:       sym(cf, "CFArrayGetCount"),
		cfArrayGet:         sym(cf, "CFArrayGetValueAtIndex"),
		cfStringGetCString: sym(cf, "CFStringGetCString"),
		dictKeyCallbacks:   sym(cf, "kCFTypeDictionaryKeyCallBacks"),
		dictValueCallbacks: sym(cf, "kCFTypeDictionaryValueCallBacks"),
		register:           sym(carbon, "TISRegisterInputSource"),
		createList:         sym(carbon, "TISCreateInputSourceList"),
		enable:             sym(carbon, "TISEnableInputSource"),
		disable:            sym(carbon, "TISDisableInputSource"),
		selectSrc:          sym(carbon, "TISSelectInputSource"),
		copyCurrent:        sym(carbon, "TISCopyCurrentKeyboardInputSource"),
		getProperty:        sym(carbon, "TISGetInputSourceProperty"),
		propBundleID:       deref(sym(carbon, "kTISPropertyBundleID")),
		propInputSourceID:  deref(sym(carbon, "kTISPropertyInputSourceID")),
		propEnabled:        deref(sym(carbon, "kTISPropertyInputSourceIsEnabled")),
		propSelectCapable:  deref(sym(carbon, "kTISPropertyInputSourceIsSelectCapable")),
		propEnableCapable:  deref(sym(carbon, "kTISPropertyInputSourceIsEnableCapable")),
		cfBooleanGetValue:  sym(cf, "CFBooleanGetValue"),
		cfRelease:          sym(cf, "CFRelease"),
	}
}

func (t *tisAPI) cfstr(s string) uintptr {
	b := append([]byte(s), 0)
	r, _, _ := purego.SyscallN(t.cfStringCreate, 0, uintptr(unsafe.Pointer(&b[0])), 0x08000100) // UTF-8
	return r
}

func (t *tisAPI) gostr(cf uintptr) string {
	buf := make([]byte, 512)
	purego.SyscallN(t.cfStringGetCString, cf, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0x08000100)
	return string(cbytes(uintptr(unsafe.Pointer(&buf[0]))))
}

// registerBundle tells the system about the bundle at path.
func (t *tisAPI) registerBundle(path string) error {
	b := []byte(path)
	url, _, _ := purego.SyscallN(t.cfURLFromPath, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 1)
	st, _, _ := purego.SyscallN(t.register, url)
	if int32(st) != 0 {
		return fmt.Errorf("TISRegisterInputSource: OSStatus %d", int32(st))
	}
	return nil
}

// sources returns the input sources whose property key equals value.
func (t *tisAPI) sources(key uintptr, value string) []uintptr {
	keys := [1]uintptr{key}
	vals := [1]uintptr{t.cfstr(value)}
	dict, _, _ := purego.SyscallN(t.cfDictCreate, 0, uintptr(unsafe.Pointer(&keys[0])), uintptr(unsafe.Pointer(&vals[0])), 1, t.dictKeyCallbacks, t.dictValueCallbacks)
	list, _, _ := purego.SyscallN(t.createList, dict, 1)
	if list == 0 {
		return nil
	}
	n, _, _ := purego.SyscallN(t.cfArrayCount, list)
	out := make([]uintptr, 0, n)
	for i := uintptr(0); i < n; i++ {
		s, _, _ := purego.SyscallN(t.cfArrayGet, list, i)
		out = append(out, s)
	}
	return out
}

func (t *tisAPI) id(src uintptr) string {
	p, _, _ := purego.SyscallN(t.getProperty, src, t.propInputSourceID)
	return t.gostr(p)
}

func (t *tisAPI) enableBundle(bundleID string) {
	for _, s := range t.sources(t.propBundleID, bundleID) {
		st, _, _ := purego.SyscallN(t.enable, s)
		fmt.Printf("enable %s: OSStatus %d\n", t.id(s), int32(st))
	}
}

// disableBundle turns the bundle's input sources off, so they leave the
// user's list (and can be added again cleanly).
func (t *tisAPI) disableBundle(bundleID string) {
	for _, s := range t.sources(t.propBundleID, bundleID) {
		st, _, _ := purego.SyscallN(t.disable, s)
		fmt.Printf("disable %s: OSStatus %d\n", t.id(s), int32(st))
	}
}

// flag reads a boolean property of an input source; false if it has none.
func (t *tisAPI) flag(src, prop uintptr) bool {
	p, _, _ := purego.SyscallN(t.getProperty, src, prop)
	if p == 0 {
		return false
	}
	v, _, _ := purego.SyscallN(t.cfBooleanGetValue, p)
	return v&0xff != 0
}

// added reports whether the bundle's input sources are all enabled: the
// user added 拾新 in System Settings.
func (t *tisAPI) added(bundleID string) bool {
	srcs := t.sources(t.propBundleID, bundleID)
	for _, s := range srcs {
		if !t.flag(s, t.propEnabled) {
			return false
		}
	}
	return len(srcs) > 0
}

func (t *tisAPI) selectID(id string) {
	for _, s := range t.sources(t.propInputSourceID, id) {
		st, _, _ := purego.SyscallN(t.selectSrc, s)
		fmt.Printf("select %s: OSStatus %d\n", id, int32(st))
	}
}

func (t *tisAPI) current() string {
	s, _, _ := purego.SyscallN(t.copyCurrent)
	if s == 0 {
		return ""
	}
	id := t.id(s)
	purego.SyscallN(t.cfRelease, s)
	return id
}

// status prints the state of the bundle's input sources.
func (t *tisAPI) status(bundleID string) {
	for _, s := range t.sources(t.propBundleID, bundleID) {
		fmt.Printf("%s enabled=%v selectCapable=%v enableCapable=%v\n", t.id(s), t.flag(s, t.propEnabled), t.flag(s, t.propSelectCapable), t.flag(s, t.propEnableCapable))
	}
}
