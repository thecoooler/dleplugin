package main

import (
	config "github.com/thecoooler/dleplugin/internal/modules/hosts/internal/hostconfig"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/host"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCredentialPrecedence(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("admin-login", "admin", "")
	const variable = "DLEHOST_TEST_ADMIN_LOGIN"
	t.Setenv(variable, "owner")
	if got := credentialValue(cmd, "admin-login", "admin", variable); got != "owner" {
		t.Fatalf("environment: got %q", got)
	}
	if err := cmd.Flags().Set("admin-login", "admin"); err != nil {
		t.Fatal(err)
	}
	if got := credentialValue(cmd, "admin-login", "admin", variable); got != "admin" {
		t.Fatalf("explicit default must override environment: got %q", got)
	}
	if err := cmd.Flags().Set("admin-login", ""); err != nil {
		t.Fatal(err)
	}
	if got := credentialValue(cmd, "admin-login", "", variable); got != "" {
		t.Fatal("explicit empty flag must reach validation")
	}
}

func TestOpenStackRouting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack")
	t.Setenv("DLEHOST_DIR", dir)
	cfg := &config.Config{BaseDomain: "dle.icooler.opik.net", ExternalOrigin: "https://other.test"}
	if _, err := openStack(cfg); err == nil {
		t.Fatal("accepted inconsistent routing")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid routing prepared the stack")
	}
	cfg.ExternalOrigin = "https://dle.icooler.opik.net"
	s, err := openStack(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.HostURL(s.Domain("shop")); got != "https://shop.dle.icooler.opik.net/" {
		t.Fatalf("URL=%s", got)
	}
}

func TestProtectArgumentsValidatedBeforeStack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack")
	t.Setenv("DLEHOST_DIR", dir)
	t.Setenv("DLEHOST_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	for _, args := range [][]string{{"protect", "shop"}, {"protect", "yes"}, {"protect", "shop", "maybe"}, {"add", "shop", "--protect", "yes"}, {"config", "set", "protect_session_ttl", "5x"}} {
		cmd := rootCmd()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid arguments prepared the stack")
	}
}

func TestProtectState(t *testing.T) {
	for _, tc := range []struct {
		protect string
		global  bool
		want    string
	}{{"", false, "off (default)"}, {"", true, "on (default)"}, {"on", false, "on"}, {"off", true, "off"}} {
		if got := protectState(host.Host{Protect: tc.protect}, tc.global); got != tc.want {
			t.Errorf("%q/%v: %s", tc.protect, tc.global, got)
		}
	}
}

func TestReadProtectPassword(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		valid    bool
	}{
		{"secret\n", "secret", true}, {"secret\r\n", "secret", true}, {" secret ", " secret ", true},
		{"пароль\n", "пароль", true}, {strings.Repeat("a", 72) + "\r\n", strings.Repeat("a", 72), true},
		{"", "", false}, {"\n", "", false}, {"a\nb\n", "", false}, {"a\x00b", "", false}, {strings.Repeat("a", 73), "", false},
	} {
		got, err := readProtectPassword(strings.NewReader(tc.in), io.Discard)
		if (err == nil) != tc.valid || tc.valid && string(got) != tc.want {
			t.Errorf("input length %d: valid %v, got length %d, error %v", len(tc.in), tc.valid, len(got), err)
		}
	}
}

func TestProtectPasswordCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DLEHOST_DIR", dir)
	t.Setenv("DLEHOST_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	run := func(args ...string) error {
		c := rootCmd()
		c.SetArgs(args)
		c.SetIn(strings.NewReader("cli-secret\n"))
		return c.Execute()
	}
	if err := run("protect", "on"); err == nil || !strings.Contains(err.Error(), "не задан глобальный пароль") {
		t.Fatalf("enable without password: %v", err)
	}
	if err := run("protect", "password", "--default"); err == nil {
		t.Fatal("accepted global --default")
	}
	if err := run("protect", "password", "cli-secret"); err == nil {
		t.Fatal("accepted positional password")
	}
	if err := run("protect", "password"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "data", "protect", "passwords.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cli-secret") || !strings.Contains(string(data), "$2a$10$") {
		t.Fatal("CLI did not store only bcrypt hash")
	}
	if err := run("protect", "on"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil || !cfg.Protect {
		t.Fatal("global protect not persisted")
	}
	if err := run("protect"); err != nil {
		t.Fatal(err)
	}
}
