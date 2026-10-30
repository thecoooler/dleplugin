package stack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetDotEnvPreservesSecretsModeAndSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings")
	if err := os.WriteFile(target, []byte("DB_ROOT_PASSWORD=secret\nHTTP_PORT=80\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".env")
	if err := os.Symlink("settings", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := &Stack{Dir: dir, Env: map[string]string{}}
	if err := s.SetDotEnv("HTTP_PORT", "8080"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "DB_ROOT_PASSWORD=secret\nHTTP_PORT=8080\n" {
		t.Fatal("settings were not preserved", err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal("file mode changed", err)
	}
	info, err = os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 || s.Env["HTTP_PORT"] != "8080" {
		t.Fatal("symlink or in-memory value changed unexpectedly", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".env-update-") {
			t.Fatal("temporary credentials file retained")
		}
	}
}

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
