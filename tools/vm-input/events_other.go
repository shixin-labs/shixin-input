//go:build !darwin

package main

import "errors"

func openKeyboard() (func(stroke) error, func(), error) {
	return nil, nil, errors.New("native VM input requires macOS")
}

func readClipboard() (string, error) {
	return "", errors.New("native VM clipboard requires macOS")
}
