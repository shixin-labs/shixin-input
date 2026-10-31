package netclient

import (
	"net/url"
	"os/exec"
)

// systemProxy is the proxy set in System Settings → Network, read when a
// download starts (it may change while the settings app runs).
func systemProxy() *url.URL {
	out, err := exec.Command("/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return nil
	}
	return parseScutilProxy(string(out))
}
