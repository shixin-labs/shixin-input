package main

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var secureTestOnce sync.Once

func initSecureTest() { secureTestOnce.Do(initObjC) }

// Only read the system flag: never enable secure input, start the front,
// or access the installed input method's settings or logs in these tests.
func TestSecureInputDoesNotAllocate(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	if n := testing.AllocsPerRun(1000, func() { secureInputEnabled() }); n != 0 {
		t.Fatalf("%v allocations per secure input check", n)
	}
}

func BenchmarkSecureInput(b *testing.B) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		secureInputEnabled()
	}
}

func TestSecureOwnerPID(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	pool, _, _ := purego.SyscallN(poolPush)
	defer purego.SyscallN(poolPop, pool)
	number := uintptr(objc.GetClass("NSNumber"))
	num := objc.RegisterName("numberWithLongLong:")
	for _, c := range []struct {
		name  string
		value uintptr
		want  int32
	}{
		{"missing", 0, 0},
		{"positive", send(number, num, 42), 42},
		{"zero", send(number, num, 0), 0},
		{"negative", send(number, num, ^uintptr(0)), 0},
		{"overflow", send(number, num, 1<<40), 0},
		{"wrong type", secureAPI.ownerKey, 0},
	} {
		dict := send(uintptr(objc.GetClass("NSMutableDictionary")), objc.RegisterName("dictionary"))
		if c.value != 0 {
			send(dict, objc.RegisterName("setObject:forKey:"), c.value, secureAPI.ownerKey)
		}
		if got := secureOwnerPID(dict); got != c.want {
			t.Errorf("%s: owner %d, want %d", c.name, got, c.want)
		}
		if n := testing.AllocsPerRun(100, func() { secureOwnerPID(dict) }); n != 0 {
			t.Errorf("%s: %v Go allocations per PID lookup", c.name, n)
		}
	}
}

func TestSecureOwnerSession(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	dict, _, _ := purego.SyscallN(secureAPI.copySession)
	if dict == 0 {
		t.Log("CGSessionCopyCurrentDictionary unavailable in this process; verify the active owner key in the VM")
		return
	}
	defer purego.SyscallN(secureAPI.release, dict)
	pid := secureOwnerPID(dict)
	pool, _, _ := purego.SyscallN(poolPush)
	defer purego.SyscallN(poolPop, pool)
	app := send(secureAPI.workspace, selFrontmostApp)
	t.Logf("Secure Event Input %v; owner PID %d; frontmost PID %d, bundle %s",
		secureInputEnabled(), pid, int32(send(app, selProcessID)), goString(send(app, selBundleID)))
}

func TestFocusedSecureInputDoesNotAllocate(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	// Exercise the enabled branch with a cached native lookup regardless
	// of the system flag. Never enable secure input on the developer's Mac.
	var s secureOwnerCache
	now := time.Now()
	s.check(true, 1, now, resolveSecureProcesses)
	if n := testing.AllocsPerRun(1000, func() {
		focusedSecureInput()
		secureInputEnabled()
		s.check(true, 1, now, resolveSecureProcesses)
	}); n != 0 {
		t.Fatalf("%v allocations per native flag/cached ownership check", n)
	}
}

func BenchmarkSecureOwnerCached(b *testing.B) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	var s secureOwnerCache
	now := time.Now()
	s.check(true, 1, now, resolveSecureProcesses)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		secureInputEnabled()
		s.check(true, 1, time.Now(), resolveSecureProcesses)
	}
}

func BenchmarkSecureOwnerResolve(b *testing.B) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	initSecureTest()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolveSecureProcesses()
	}
}
