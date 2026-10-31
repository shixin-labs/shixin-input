// Package netclient is the HTTP client for everything Shixin downloads
// (the 智能排序 model, update checks): it follows the user's proxy, which
// apps opened from Finder only find in System Settings.
package netclient

import (
	"net"
	"net/http"
	"net/url"
	"time"
)

// Client follows the user's proxy: the environment's, else the system's
// (apps opened from Finder have no proxy variables). Blocked hosts fail
// within seconds rather than minutes. It sets no overall timeout: callers
// bound a download with a context and their own stall timer.
var Client = &http.Client{Transport: &http.Transport{
	Proxy: func(r *http.Request) (*url.URL, error) {
		if u, err := http.ProxyFromEnvironment(r); u != nil || err != nil {
			return u, err
		}
		if h := r.URL.Hostname(); h == "localhost" || net.ParseIP(h).IsLoopback() {
			return nil, nil
		}
		return systemProxy(), nil
	},
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 20 * time.Second,
	ForceAttemptHTTP2:     true,
}}
