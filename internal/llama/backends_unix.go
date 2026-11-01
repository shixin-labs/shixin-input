//go:build !windows

package llama

// Unix builds link their backends into libllama's dependency graph.
func (l *Lib) loadBackends(string) error { return nil }
