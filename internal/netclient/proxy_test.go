package netclient

import (
	"net/http"
	"strings"
	"testing"
)

func TestParseScutilProxy(t *testing.T) {
	const both = `<dictionary> {
  ExceptionsList : <array> {
    0 : *.local
  }
  HTTPEnable : 1
  HTTPPort : 7890
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7890
  HTTPSProxy : 127.0.0.1
  ProxyAutoConfigEnable : 0
  SOCKSEnable : 1
  SOCKSPort : 7891
  SOCKSProxy : 127.0.0.1
}`
	for _, c := range []struct{ name, out, want string }{
		{"https", both, "http://127.0.0.1:7890"},
		{"socks", strings.Replace(both, "HTTPSEnable : 1", "HTTPSEnable : 0", 1), "socks5://127.0.0.1:7891"},
		{"none", "<dictionary> {\n  HTTPEnable : 0\n  ProxyAutoConfigEnable : 1\n}", ""},
	} {
		got := ""
		if u := parseScutilProxy(c.out); u != nil {
			got = u.String()
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// Local servers (the model's test fixtures, the engine) are never sent
// through the system proxy.
func TestClientSkipsProxyForLoopback(t *testing.T) {
	proxy := Client.Transport.(*http.Transport).Proxy
	for _, raw := range []string{"http://localhost:8080/x", "http://127.0.0.1/x", "https://[::1]:443/x", "http://127.9.9.9/x"} {
		r, err := http.NewRequest("GET", raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if u, err := proxy(r); u != nil || err != nil {
			t.Errorf("%s: proxy %v, %v", raw, u, err)
		}
	}
}

func TestParseScutilProxyIncomplete(t *testing.T) {
	for name, out := range map[string]string{
		"no port":      "HTTPSEnable : 1\nHTTPSProxy : proxy.example\n",
		"no host":      "HTTPSEnable : 1\nHTTPSPort : 8080\n",
		"not enabled":  "HTTPSProxy : proxy.example\nHTTPSPort : 8080\n",
		"empty output": "",
	} {
		if u := parseScutilProxy(out); u != nil {
			t.Errorf("%s: %v", name, u)
		}
	}
	u := parseScutilProxy("HTTPSEnable : 1\nHTTPSProxy : fe80::1\nHTTPSPort : 3128\n")
	if u == nil || u.Host != "[fe80::1]:3128" {
		t.Errorf("IPv6 proxy: %v", u)
	}
}
