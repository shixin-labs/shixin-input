package main

import (
	"testing"

	"shixin/internal/rerank"
	"shixin/internal/settings"
)

// Every 智能排序 setting maps to a distinct protocol strength, and anything
// else (off, or a value from a newer version) leaves shixin-ai stopped.
func TestRerankStrength(t *testing.T) {
	for setting, want := range map[string]uint8{
		settings.RerankCautious: rerank.StrengthCautious,
		settings.RerankStandard: rerank.StrengthStandard,
		settings.RerankOff:      0,
		"":                      0,
		"aggressive":            0,
	} {
		if got := strength(setting); got != want {
			t.Errorf("strength(%q) = %d, want %d", setting, got, want)
		}
	}
	if rerank.StrengthCautious == 0 || rerank.StrengthStandard == 0 || rerank.StrengthCautious == rerank.StrengthStandard {
		t.Fatal("strengths must be distinct and nonzero")
	}
}
