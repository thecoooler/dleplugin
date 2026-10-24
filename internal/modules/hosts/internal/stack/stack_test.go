package stack

import "testing"

func TestUnquote(t *testing.T) {
	for in, want := range map[string]string{
		`"abc"`: "abc",
		`'abc'`: "abc",
		`"abc'`: `"abc'`,
		`"`:     `"`,
		`""`:    "",
		`a"b"`:  `a"b"`,
		`abc`:   "abc",
	} {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestPublicURL(t *testing.T) {
	for _, tc := range []struct{ base, origin, port, domain, want string }{
		{"", "", "80", "shop.localhost", "http://shop.localhost/"},
		{"", "", "8090", "shop.localhost", "http://shop.localhost:8090/"},
		{"dle.icooler.opik.net", "", "8090", "shop.dle.icooler.opik.net", "http://shop.dle.icooler.opik.net:8090/"},
		{"dle.icooler.opik.net", "https://dle.icooler.opik.net", "8090", "shop.dle.icooler.opik.net", "https://shop.dle.icooler.opik.net/"},
		{"example.com", "https://example.com:8443", "8090", "shop.example.com", "https://shop.example.com:8443/"},
		{"example.com", "http://example.com", "8090", "shop.example.com", "http://shop.example.com/"},
	} {
		s := &Stack{BaseDomain: tc.base, ExternalOrigin: tc.origin, Env: map[string]string{"HTTP_PORT": tc.port}}
		if got := s.Domain("shop"); got != tc.domain {
			t.Errorf("domain=%s, want %s", got, tc.domain)
		}
		if got := s.HostURL(tc.domain); got != tc.want {
			t.Errorf("URL=%s, want %s", got, tc.want)
		}
		if got := s.LocalHostURL("localhost"); tc.port == "8090" && got != "http://localhost:8090/" {
			t.Errorf("backend=%s", got)
		}
	}
}
