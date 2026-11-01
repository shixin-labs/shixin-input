package rime

import (
	"context"
	"time"

	"github.com/ebitengine/purego"

	"shixin/internal/dynlib"
)

// watchDateClock refreshes libc's cached time zone off the engine thread.
// Polling also covers systems without timedated/D-Bus and direct replacement
// of /etc/localtime. The native date translator reads only an atomic offset,
// so neither tzset's file I/O nor its libc lock can delay a key.
func (l *Lib) watchDateClock() func() {
	fn, err := dynlib.Symbol(l.handle, "shixin_date_refresh_zone")
	if err != nil {
		return func() {} // A system librime has no Shixin date translator.
	}
	purego.SyscallN(fn) // Initialization, before creating sessions or serving keys.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				// This dedicated thread-safe export calls no Rime APIs and
				// does not use Lib.args, which belongs to the engine thread.
				purego.SyscallN(fn)
			}
		}
	}()
	return cancel
}
