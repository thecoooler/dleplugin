package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".config", "dleplugin", "config.yml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("HOME", "/h")
	p, err := DefaultPath()
	if err != nil || p != "/h/.config/dleplugin/config.yml" {
		t.Fatalf("%q %v", p, err)
	}
}

func TestMissingCreated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "config.yml")
	s := NewStore(p)
	_, err := s.Core(true)
	if err == nil || !strings.Contains(err.Error(), "Config file was created at "+p) ||
		!strings.Contains(err.Error(), "Fill required configuration values in "+p+":\n- host_dir") {
		t.Fatalf("err = %v", err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "host_dir: null\n" {
		t.Errorf("content %q", b)
	}
	if _, err := NewStore(p).Core(false); err == nil || strings.Contains(err.Error(), "created") {
		t.Errorf("err = %v", err)
	}
}

func TestMissingNotCreated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yml")
	_, err := NewStore(p).Core(false)
	if !errors.Is(err, ErrNotFound) || err.Error() != "Config file not found: "+p {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(p); serr == nil {
		t.Error("file created")
	}
}

func TestCoreValid(t *testing.T) {
	p := write(t, "host_dir: /x/hosts\nstatic: garbage\ntest: null\nhost: {db_host: a}\n")
	c, err := NewStore(p).Core(true)
	if err != nil || c.HostDir != "/x/hosts" || c.DefaultHostPath() != "/x/hosts/dle.localhost" {
		t.Fatalf("%v %v", c, err)
	}
}

func TestInvalidYAMLAndRoot(t *testing.T) {
	p := write(t, "host: [")
	_, err := NewStore(p).Core(true)
	if err == nil || !strings.Contains(err.Error(), "Invalid YAML") || !strings.Contains(err.Error(), p) {
		t.Fatalf("err = %v", err)
	}
	p = write(t, "- a\n")
	_, err = NewStore(p).Core(true)
	if err == nil || !strings.Contains(err.Error(), "root must be a mapping") {
		t.Fatalf("err = %v", err)
	}
	p = write(t, "")
	_, err = NewStore(p).Core(true)
	if err == nil || !strings.Contains(err.Error(), "- host_dir") {
		t.Fatalf("err = %v", err)
	}
}

type inner struct {
	Token string `yaml:"token" required:"true"`
}
type sec struct {
	Host   string `yaml:"db_host" required:"true"`
	User   string `yaml:"db_user"`
	Port   int    `yaml:"port"`
	Nested inner  `yaml:"nested"`
}

func TestSection(t *testing.T) {
	p := write(t, "host_dir: x\nhost:\n  db_host: h\n  nested:\n    token: t\n")
	v := sec{User: "root", Port: 3306}
	if err := NewStore(p).Section("host", &v); err != nil {
		t.Fatal(err)
	}
	if v.Host != "h" || v.User != "root" || v.Port != 3306 || v.Nested.Token != "t" {
		t.Errorf("%+v", v)
	}
}

func TestSectionRequired(t *testing.T) {
	p := write(t, "host_dir: x\nhost:\n  db_host: null\n")
	err := NewStore(p).Section("host", &sec{})
	want := "Fill required configuration values in " + p + ":\n- host.db_host\n- host.nested.token"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	// missing section
	err = NewStore(p).Section("other", &sec{})
	if err == nil || !strings.Contains(err.Error(), "- other.db_host") {
		t.Fatalf("err = %v", err)
	}
	// no required fields: missing and null sections keep defaults
	type d struct {
		A int `yaml:"a"`
	}
	p = write(t, "test: null\n")
	x := d{A: 5}
	if err := NewStore(p).Section("test", &x); err != nil || x.A != 5 {
		t.Fatal(err, x)
	}
	if err := NewStore(p).Section("nope", &x); err != nil || x.A != 5 {
		t.Fatal(err, x)
	}
}

func TestSectionErrors(t *testing.T) {
	p := write(t, "static: garbage\nt:\n  a: str\n")
	type d struct {
		A int `yaml:"a"`
	}
	err := NewStore(p).Section("static", &d{})
	if err == nil || err.Error() != "Invalid config file "+p+": static must be a mapping" {
		t.Fatalf("err = %v", err)
	}
	err = NewStore(p).Section("t", &d{})
	if err == nil || !strings.HasPrefix(err.Error(), "Invalid config file "+p+": ") {
		t.Fatalf("err = %v", err)
	}
	err = NewStore(filepath.Join(t.TempDir(), "no.yml")).Section("t", &d{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestRaw(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "no.yml"))
	if _, ok, err := s.Raw(); ok || err != nil {
		t.Fatal(ok, err)
	}
	p := write(t, "dependency_roots: [a, b]\n")
	m, ok, err := NewStore(p).Raw()
	if !ok || err != nil || len(m["dependency_roots"].([]any)) != 2 {
		t.Fatal(m, ok, err)
	}
}

func TestSaveYAMLFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d", "c.yml")
	type cfg struct {
		Sites map[string]string `yaml:"sites"`
	}
	if err := SaveYAMLFile(p, cfg{Sites: map[string]string{"a": "b"}}, 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "sites:\n  a: b\n" {
		t.Errorf("%q", b)
	}
}
