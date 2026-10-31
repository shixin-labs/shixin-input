package netclient

import (
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
