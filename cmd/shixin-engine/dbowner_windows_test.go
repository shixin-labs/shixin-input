package main

import "testing"

func dbOwner(t *testing.T, user string) int {
	t.Helper()
	t.Skip("Windows exposes no F_GETLK owner PID; dictionary exclusion is checked by internal/engine lock tests")
	return 0
}
