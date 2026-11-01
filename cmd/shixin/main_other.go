//go:build !darwin && !windows

// Command shixin is the macOS front; the Windows UI process arrives in M3.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "shixin: only macOS is supported so far")
	os.Exit(1)
}
