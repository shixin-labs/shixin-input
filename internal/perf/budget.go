package perf

import (
	"os"
	"strconv"
	"time"
)

// Tolerance scales the tests' latency budgets (SHIXIN_PERF_TOLERANCE,
// default 1). "off" turns the latency gates off and returns 0: CI's shared
// runners and sanitizer builds can't measure latency reliably.
func Tolerance() float64 {
	v := os.Getenv("SHIXIN_PERF_TOLERANCE")
	if v == "off" {
		return 0
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
		return f
	}
	return 1
}

// OverBudget reports whether d exceeds budget scaled by Tolerance, and the
// scaled limit. With the gates off it never does.
func OverBudget(d, budget time.Duration) (limit time.Duration, over bool) {
	tol := Tolerance()
	if tol == 0 {
		return 0, false
	}
	limit = time.Duration(float64(budget) * tol)
	return limit, d > limit
}
