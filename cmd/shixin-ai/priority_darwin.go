package main

import "github.com/ebitengine/purego"

// setInteractivePriority gives the calling thread QOS_CLASS_USER_INTERACTIVE
// (DESIGN R7), like the engine thread: orders that arrive late are dropped.
func setInteractivePriority() {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	fn, err := purego.Dlsym(lib, "pthread_set_qos_class_self_np")
	if err != nil {
		return
	}
	const qosClassUserInteractive = 0x21
	purego.SyscallN(fn, qosClassUserInteractive, 0)
}
