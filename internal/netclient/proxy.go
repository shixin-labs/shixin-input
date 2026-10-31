package netclient

import (
	"bufio"
	"net"
	"net/url"
	"strings"
)

// parseScutilProxy picks the proxy for HTTPS from `scutil --proxy` output:
// the HTTPS proxy if enabled, else the SOCKS one; nil for none. Automatic
// configuration (PAC) isn't evaluated.
func parseScutilProxy(out string) *url.URL {
	kv := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), " : ")
		if ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for _, p := range []struct{ key, scheme string }{{"HTTPS", "http"}, {"SOCKS", "socks5"}} {
		host, port := kv[p.key+"Proxy"], kv[p.key+"Port"]
		if kv[p.key+"Enable"] == "1" && host != "" && port != "" {
			return &url.URL{Scheme: p.scheme, Host: net.JoinHostPort(host, port)}
		}
	}
	return nil
}
