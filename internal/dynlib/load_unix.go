//go:build darwin || linux

// Package dynlib loads native libraries without cgo. Loading is startup work.
package dynlib

import "github.com/ebitengine/purego"

// Open loads path, resolving dependencies from the platform loader.
func Open(path string, global bool) (uintptr, error) {
	flags := purego.RTLD_NOW | purego.RTLD_LOCAL
	if global {
		flags = purego.RTLD_NOW | purego.RTLD_GLOBAL
	}
	return purego.Dlopen(path, flags)
}

// Symbol resolves an exported function.
func Symbol(h uintptr, name string) (uintptr, error) { return purego.Dlsym(h, name) }
