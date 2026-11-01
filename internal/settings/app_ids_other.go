//go:build !windows

package settings

// AppKey is the identity used to compare a front's app with a saved rule.
func AppKey(id string) string { return id }

// AppAliases resolves saved app identities off the engine thread.
func AppAliases(id string) []string { return []string{id} }
