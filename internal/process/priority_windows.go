package process

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Interactive opts the process out of EcoQoS and raises the current native
// thread's priority. Call after locking the engine/scorer goroutine to it.
func Interactive() {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	// PROCESS_POWER_THROTTLING_STATE v1: explicitly disable execution-speed
	// throttling, without altering unrelated power policies.
	state := struct{ Version, ControlMask, StateMask uint32 }{1, 1, 0}
	if fn := kernel.NewProc("SetProcessInformation"); fn.Find() == nil {
		fn.Call(uintptr(windows.CurrentProcess()), 4, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state))
	}
	kernel.NewProc("SetThreadPriority").Call(uintptr(windows.CurrentThread()), 1) // ABOVE_NORMAL
}
