//go:build !linux

package rime

func (l *Lib) watchDateClock() func() { return func() {} }
