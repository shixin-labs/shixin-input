package main

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Replacing the record can briefly collide with a reader that holds it open
// without FILE_SHARE_DELETE (an older client, antivirus, an indexer).
// Retry for a short, bounded time; publish runs on the coordinator, never on
// a key path.
const (
	publishRetryFor   = time.Second
	publishRetryEvery = 10 * time.Millisecond
)

func retryableRename(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// Windows symlinks require Developer Mode or elevation. Publish an atomic
// discovery record instead; ipc.Dial maps the target to a named pipe.
func publish(path, target string) error {
	tmp := path + ".next"
	if err := os.WriteFile(tmp, []byte(target), 0o600); err != nil {
		return err
	}
	deadline := time.Now().Add(publishRetryFor)
	for {
		err := os.Rename(tmp, path)
		if err == nil {
			return nil
		}
		if !retryableRename(err) || !time.Now().Before(deadline) {
			os.Remove(tmp)
			return err
		}
		time.Sleep(publishRetryEvery)
	}
}
