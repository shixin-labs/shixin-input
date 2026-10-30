//go:build !darwin

package models

import "net/url"

// systemProxy is nil off macOS: the environment's proxy variables apply.
func systemProxy() *url.URL { return nil }
