//go:build shixin_unsigned_preview

package main

import (
	"strings"
	"testing"
)

func TestUnsignedPreviewRefusesInAppUpdate(t *testing.T) {
	if err := applyWindowsUpdate("unused.exe", "9.9.9"); err == nil || !strings.Contains(err.Error(), "manual installation") {
		t.Fatalf("preview update: %v", err)
	}
}
