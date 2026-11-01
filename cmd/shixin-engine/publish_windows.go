package main

import "os"

// Windows symlinks require Developer Mode or elevation. Publish an atomic
// discovery record instead; ipc.Dial maps the target to a named pipe.
func publish(path, target string) error {
	tmp := path + ".next"
	if err := os.WriteFile(tmp, []byte(target), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
