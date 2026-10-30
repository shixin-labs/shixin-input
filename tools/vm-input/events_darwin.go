package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ebitengine/purego"
)

func openKeyboard() (func(stroke) error, func(), error) {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, nil, err
	}
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, nil, err
	}
	var allowed, request func() bool
	var sourceCreate func(int32) uintptr
	var create func(uintptr, uint16, bool) uintptr
	var setFlags func(uintptr, uint64)
	var post func(uint32, uintptr)
	var release func(uintptr)
	purego.RegisterLibFunc(&allowed, cg, "CGPreflightPostEventAccess")
	purego.RegisterLibFunc(&request, cg, "CGRequestPostEventAccess")
	purego.RegisterLibFunc(&sourceCreate, cg, "CGEventSourceCreate")
	purego.RegisterLibFunc(&create, cg, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&setFlags, cg, "CGEventSetFlags")
	purego.RegisterLibFunc(&post, cg, "CGEventPost")
	purego.RegisterLibFunc(&release, cf, "CFRelease")
	if !allowed() && !request() {
		return nil, nil, errors.New("enable Accessibility for shixin-vm-input in the VM's System Settings, then rerun")
	}
	source := sourceCreate(-1) // kCGEventSourceStatePrivate, isolated from VNC keys
	if source == 0 {
		return nil, nil, errors.New("create keyboard event source")
	}
	press := func(k stroke) error {
		// Construct the complete stroke before posting so allocation failure
		// cannot leave a synthetic modifier pressed.
		var pending []uintptr
		defer func() {
			for _, e := range pending {
				release(e)
			}
		}()
		for _, e := range events(k) {
			p := create(source, e.code, e.down)
			if p == 0 {
				return fmt.Errorf("create key event for code %d", e.code)
			}
			setFlags(p, e.flags)
			pending = append(pending, p)
		}
		for _, p := range pending {
			post(0, p) // kCGHIDEventTap: through the normal input-method path
		}
		return nil
	}
	return press, func() { release(source) }, nil
}

func readClipboard() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/pbpaste")
	cmd.Env = append(os.Environ(), "LC_ALL=en_US.UTF-8")
	b, err := cmd.Output()
	return string(b), err
}
