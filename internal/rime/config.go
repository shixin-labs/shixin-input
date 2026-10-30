package rime

import (
	"fmt"
	"runtime"
	"unsafe"
)

// Config is an open Rime config (a deployed YAML file). Reading config is
// not for the hot path: every call converts strings. Close it when done.
type Config struct {
	lib *Lib
	c   struct{ ptr uintptr } // RimeConfig
}

// OpenConfig opens the deployed config id, e.g. "default".
func (l *Lib) OpenConfig(id string) (*Config, error) {
	return l.open(l.api.configOpen, id)
}

// OpenSchema opens the deployed config of schema id.
func (l *Lib) OpenSchema(id string) (*Config, error) {
	return l.open(l.api.schemaOpen, id)
}

func (l *Lib) open(fn uintptr, id string) (*Config, error) {
	c := &Config{lib: l}
	b := append([]byte(id), 0)
	ok := ctrue(l.call(fn, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&c.c))))
	runtime.KeepAlive(b)
	if !ok {
		return nil, fmt.Errorf("rime: open config %q failed", id)
	}
	return c, nil
}

// Close releases the config.
func (c *Config) Close() {
	c.lib.call(c.lib.api.configClose, uintptr(unsafe.Pointer(&c.c)))
}

// String returns the string at key (e.g. "speller/alphabet"), or ok false.
func (c *Config) String(key string) (s string, ok bool) {
	b := append([]byte(key), 0)
	p := c.lib.call(c.lib.api.configGetCString, uintptr(unsafe.Pointer(&c.c)), uintptr(unsafe.Pointer(&b[0])))
	runtime.KeepAlive(b)
	if p == 0 {
		return "", false
	}
	return string(cbytes(p, nil)), true
}

// SetString changes a value in the process's cached config. Deployed
// configs are not auto-saved; this never rewrites a generation on disk.
func (c *Config) SetString(key, value string) bool {
	k, v := append([]byte(key), 0), append([]byte(value), 0)
	ok := ctrue(c.lib.call(c.lib.api.configSetString, uintptr(unsafe.Pointer(&c.c)), uintptr(unsafe.Pointer(&k[0])), uintptr(unsafe.Pointer(&v[0]))))
	runtime.KeepAlive(k)
	runtime.KeepAlive(v)
	return ok
}

// Bool returns the boolean at key, or ok false.
func (c *Config) Bool(key string) (v, ok bool) {
	var out int32
	b := append([]byte(key), 0)
	ok = ctrue(c.lib.call(c.lib.api.configGetBool, uintptr(unsafe.Pointer(&c.c)), uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&out))))
	runtime.KeepAlive(b)
	return out != 0, ok
}

// Int returns the integer at key, or ok false.
func (c *Config) Int(key string) (v int, ok bool) {
	var out int32
	b := append([]byte(key), 0)
	ok = ctrue(c.lib.call(c.lib.api.configGetInt, uintptr(unsafe.Pointer(&c.c)), uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&out))))
	runtime.KeepAlive(b)
	return int(out), ok
}

// ListSize returns the length of the list at key (0 if absent).
func (c *Config) ListSize(key string) int {
	b := append([]byte(key), 0)
	n := c.lib.call(c.lib.api.configListSize, uintptr(unsafe.Pointer(&c.c)), uintptr(unsafe.Pointer(&b[0])))
	runtime.KeepAlive(b)
	return int(n)
}
