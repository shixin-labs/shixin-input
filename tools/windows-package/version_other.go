//go:build !windows

package main

import "errors"

func stamp(string, string, string) error { return errors.New("version resources require Windows") }
