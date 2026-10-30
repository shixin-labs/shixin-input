package fonts

import "testing"

func TestResolve(t *testing.T) {
	families := []string{"Arial", "Songti SC", "苹方-简"}
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {"missing font", ""}, {"songti sc", "Songti SC"}, {"苹方-简", "苹方-简"},
	} {
		if got := Resolve(tc.input, families); got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestWarm(t *testing.T) {
	// Also exercises fallback with a Latin-only family and an unavailable
	// family; the latter must never stop the input method starting.
	for _, family := range []string{"", "monospace", "Shixin missing font"} {
		Warm(family, 17)
	}
}
