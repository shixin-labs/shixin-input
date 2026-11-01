//go:build darwin || linux

package main

import "os"

func publish(path, target string) error {
	tmp := path + ".next"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
