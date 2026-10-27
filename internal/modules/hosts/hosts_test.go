package hosts

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/host"
	config "github.com/thecoooler/dleplugin/internal/modules/hosts/internal/hostconfig"
	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
	"github.com/thecoooler/dleplugin/internal/modules/moduletest"
)

func TestUpReportsOccupiedPortBeforeCompose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := fmt.Sprint(ln.Addr().(*net.TCPAddr).Port)
	t.Setenv("DLEPLUGIN_HOSTS_DIR", filepath.Join(t.TempDir(), "stack"))
	t.Setenv("DLEPLUGIN_HOSTS_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	t.Setenv("BIND_ADDRESS", "127.0.0.1")
	t.Setenv("HTTP_PORT", port)
	t.Setenv("DB_PORT", "3307")
	t.Setenv("PATH", t.TempDir())
	code, msg := run(t, "", "up")
	if code == 0 || !strings.Contains(msg, "HTTP_PORT") || !strings.Contains(msg, port) || !strings.Contains(msg, "already in use") {
		t.Fatalf("up: exit %d, %s", code, msg)
	}
}

func TestGeneratedCredentialsStoredPrivately(t *testing.T) {
	s := &stack.Stack{Dir: t.TempDir()}
	path, err := saveGeneratedCredentials(s, "shop", host.Credentials{Login: "owner", Password: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file permissions: %v %v", info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "Host: \"shop\"\nLogin: owner\nPassword: test-secret\n" {
		t.Fatal("credentials file contents incorrect", err)
	}
}

func TestAdministratorPasswordDefaultsAndOverrides(t *testing.T) {
	t.Setenv("DLEPLUGIN_HOSTS_ADMIN_PASSWORD", "")
	if err := os.Unsetenv("DLEPLUGIN_HOSTS_ADMIN_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("admin-login", "admin", "")
	cmd.Flags().String("admin-password", "", "")
	first, generated, err := adminCredentials(cmd, "admin", "")
	if err != nil || !generated {
		t.Fatal("password was not generated", err)
	}
	decoded, err := hex.DecodeString(first.Password)
	if err != nil || len(decoded) != 24 {
		t.Fatal("generated password is not 24 random bytes")
	}
	second, _, err := adminCredentials(cmd, "admin", "")
	if err != nil || first.Password == second.Password {
		t.Fatal("administrator passwords are not independent", err)
	}
	t.Setenv("DLEPLUGIN_HOSTS_ADMIN_PASSWORD", "explicit-env")
	c, generated, err := adminCredentials(cmd, "admin", "")
	if err != nil || generated || c.Password != "explicit-env" {
		t.Fatal("environment password was not honored", err)
	}
	if err := cmd.Flags().Set("admin-password", "explicit-flag"); err != nil {
		t.Fatal(err)
	}
	c, generated, err = adminCredentials(cmd, "admin", "explicit-flag")
	if err != nil || generated || c.Password != "explicit-flag" {
		t.Fatal("flag password did not override environment", err)
	}
	if err := cmd.Flags().Set("admin-password", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := adminCredentials(cmd, "admin", ""); err == nil {
		t.Fatal("explicit empty password accepted")
	}
}

func TestFailedAddCleansGeneratedCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DLEPLUGIN_HOSTS_DIR", dir)
	t.Setenv("DLEPLUGIN_HOSTS_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	e := moduletest.NewEnv(t, "", Module())
	if code := e.Run("hosts", "add", "shop", "--dist", filepath.Join(t.TempDir(), "missing.zip")); code == 0 {
		t.Fatal("missing archive accepted")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "data", "credentials"))
	if err != nil || len(entries) != 0 {
		t.Fatal("credentials retained after failed add", err)
	}
}

// run executes dleplugin hosts with args and stdin; it returns the exit code
// and the error output.
func run(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	e := moduletest.NewEnv(t, stdin, Module())
	code := e.Run(append([]string{"hosts"}, args...)...)
	return code, e.ErrB.String()
}

func TestHelp(t *testing.T) {
	for _, sub := range []string{"", "up", "down", "compose", "list", "add", "remove", "get", "set", "path", "port", "bind", "versions", "config", "config set"} {
		args := append(strings.Fields(sub), "--help")
		if sub == "compose" {
			continue // flags go to docker compose
		}
		e := moduletest.NewEnv(t, "", Module())
		if code := e.Run(append([]string{"hosts"}, args...)...); code != 0 {
			t.Errorf("hosts %s --help: exit %d: %s", sub, code, e.ErrB.String())
		}
		if !strings.Contains(e.Out.String(), "dleplugin hosts") {
			t.Errorf("hosts %s --help lacks usage:\n%s", sub, e.Out.String())
		}
	}
}

func TestCredentialPrecedence(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("admin-login", "admin", "")
	const variable = "DLEPLUGIN_HOSTS_TEST_ADMIN_LOGIN"
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

func TestHostSettingsCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DLEPLUGIN_HOSTS_DIR", dir)
	t.Setenv("DLEPLUGIN_HOSTS_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	path := filepath.Join(dir, "www/shop/engine/data/config.php")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<?php $config = array('site_offline' => '0', 'admin_path' => 'admin.php');"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "www/shop/admin.php"), []byte("<?php echo 'admin';"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"panel.php", "admin.php"} {
		e := moduletest.NewEnv(t, "", Module())
		if code := e.Run("hosts", "set", "shop", "admin_path", value); code != 0 || !strings.Contains(e.Out.String(), "admin_path = "+value) {
			t.Fatalf("set %s: %s %s", value, e.Out.String(), e.ErrB.String())
		}
		e = moduletest.NewEnv(t, "", Module())
		if code := e.Run("hosts", "get", "shop", "admin_path"); code != 0 || strings.TrimSpace(e.Out.String()) != value {
			t.Fatalf("get admin_path: %s %s", e.Out.String(), e.ErrB.String())
		}
	}
	for _, value := range []string{"on", "off"} {
		if code, msg := run(t, "", "set", "shop", "site_offline", value); code != 0 {
			t.Fatalf("set %s: %s", value, msg)
		}
		e := moduletest.NewEnv(t, "", Module())
		if code := e.Run("hosts", "get", "shop", "site_offline"); code != 0 || strings.TrimSpace(e.Out.String()) != value {
			t.Fatalf("get: %s %s", e.Out.String(), e.ErrB.String())
		}
	}
}

func TestHostSettingsArgumentsValidatedBeforeStack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack")
	t.Setenv("DLEPLUGIN_HOSTS_DIR", dir)
	t.Setenv("DLEPLUGIN_HOSTS_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	for _, args := range [][]string{
		{"get", "shop", "unknown"}, {"set", "shop", "unknown", "on"},
		{"set", "shop", "admin_path", "../admin.php"}, {"set", "shop", "admin_path", ""},
		{"set", "shop", "site_offline", "yes"}, {"set", "shop", "site_offline", ""},
		{"add", "shop", "--site-offline", "yes"}, {"add", "shop", "--site-offline="},
	} {
		if code, _ := run(t, "", args...); code == 0 {
			t.Errorf("accepted %v", args)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid arguments prepared the stack")
	}
}

func TestOpenStackRouting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack")
	t.Setenv("DLEPLUGIN_HOSTS_DIR", dir)
	rt := moduletest.NewEnv(t, "", Module()).RT
	cfg := &config.Config{BaseDomain: "dle.icooler.opik.net", ExternalOrigin: "https://other.test"}
	if _, err := openStack(rt, cfg); err == nil {
		t.Fatal("accepted inconsistent routing")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid routing prepared the stack")
	}
	cfg.ExternalOrigin = "https://dle.icooler.opik.net"
	s, err := openStack(rt, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.Exec != rt.Exec {
		t.Fatal("stack does not use the runtime runner")
	}
	if got := s.HostURL(s.Domain("shop")); got != "https://shop.dle.icooler.opik.net/" {
		t.Fatalf("URL=%s", got)
	}
}

func TestComposePassthrough(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nshift 5\nprintf '%s\\n' \"$@\"\nexit 3\n" // drops compose --project-directory DIR -f FILE
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DLEPLUGIN_HOSTS_DIR", filepath.Join(t.TempDir(), "stack"))
	t.Setenv("DLEPLUGIN_HOSTS_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	for _, tc := range []struct {
		args    []string
		want    string
		verbose bool
	}{
		{[]string{"-v", "hosts", "compose", "ps"}, "ps\n", true},
		{[]string{"hosts", "compose", "--verbose", "logs", "-f"}, "logs\n-f\n", true},
		{[]string{"hosts", "compose", "-f", "x.yml", "ps", "-v"}, "-f\nx.yml\nps\n-v\n", false},
		{[]string{"hosts", "compose", "-vv", "ps"}, "ps\n", true},
		{[]string{"hosts", "compose", "-v=true", "--verbose=false", "ps"}, "ps\n", false},
		{[]string{"-v=false", "hosts", "compose", "ps"}, "ps\n", false},
		{[]string{"hosts", "compose", "-vf", "x.yml", "--", "ps"}, "-vf\nx.yml\n--\nps\n", false},
	} {
		e := moduletest.NewEnv(t, "", Module())
		if code := e.Run(tc.args...); code != 3 {
			t.Fatalf("%v: exit %d: %s", tc.args, code, e.ErrB.String())
		}
		if got := e.Out.String(); got != tc.want {
			t.Errorf("%v: compose args %q, want %q", tc.args, got, tc.want)
		}
		if e.RT.UI.Verbose() != tc.verbose {
			t.Errorf("%v: verbose = %v", tc.args, e.RT.UI.Verbose())
		}
	}
}

func TestDefaultAdminPathCLI(t *testing.T) {
	t.Setenv("DLEPLUGIN_HOSTS_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	t.Setenv("DLEPLUGIN_HOSTS_DIR", filepath.Join(t.TempDir(), "unused-stack"))
	for _, args := range [][]string{
		{"config", "get", "default_admin_path"},
		{"config", "set", "default_admin_path", "control.php"},
		{"config", "get", "default_admin_path"},
		{"config", "unset", "default_admin_path"},
		{"config", "get", "default_admin_path"},
	} {
		e := moduletest.NewEnv(t, "", Module())
		if code := e.Run(append([]string{"hosts"}, args...)...); code != 0 {
			t.Fatalf("%v: %s", args, e.ErrB.String())
		}
		if args[1] == "get" {
			cfg, err := config.Load()
			if err != nil || strings.TrimSpace(e.Out.String()) != cfg.AdminPath() {
				t.Fatalf("get default: %s %v", e.Out.String(), err)
			}
		}
	}
}

func TestBindCLI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack")
	t.Setenv("DLEPLUGIN_HOSTS_DIR", dir)
	t.Setenv("DLEPLUGIN_HOSTS_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	bin := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Default read
	e := moduletest.NewEnv(t, "", Module())
	if code := e.Run("hosts", "bind"); code != 0 || strings.TrimSpace(e.Out.String()) != "127.0.0.1" {
		t.Fatalf("bind get default: code=%d, out=%s, err=%s", code, e.Out.String(), e.ErrB.String())
	}

	// Change via bind
	e = moduletest.NewEnv(t, "", Module())
	if code := e.Run("hosts", "bind", "0.0.0.0"); code != 0 || !strings.Contains(e.Out.String(), "0.0.0.0") {
		t.Fatalf("bind set: code=%d, out=%s, err=%s", code, e.Out.String(), e.ErrB.String())
	}

	// Check updated
	e = moduletest.NewEnv(t, "", Module())
	if code := e.Run("hosts", "bind"); code != 0 || strings.TrimSpace(e.Out.String()) != "0.0.0.0" {
		t.Fatalf("bind get updated: code=%d, out=%s, err=%s", code, e.Out.String(), e.ErrB.String())
	}

	// Change via alias host
	e = moduletest.NewEnv(t, "", Module())
	if code := e.Run("hosts", "host", "127.0.0.1"); code != 0 || !strings.Contains(e.Out.String(), "127.0.0.1") {
		t.Fatalf("host alias set: code=%d, out=%s, err=%s", code, e.Out.String(), e.ErrB.String())
	}

	// Check updated
	e = moduletest.NewEnv(t, "", Module())
	if code := e.Run("hosts", "host"); code != 0 || strings.TrimSpace(e.Out.String()) != "127.0.0.1" {
		t.Fatalf("host alias get: code=%d, out=%s, err=%s", code, e.Out.String(), e.ErrB.String())
	}

	// Invalid IP
	e = moduletest.NewEnv(t, "", Module())
	if code := e.Run("hosts", "bind", "not-an-ip"); code == 0 {
		t.Fatal("accepted invalid IP")
	}
}
