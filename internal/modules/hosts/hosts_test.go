package hosts

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/host"
	config "github.com/thecoooler/dleplugin/internal/modules/hosts/internal/hostconfig"
	"github.com/thecoooler/dleplugin/internal/modules/moduletest"
)

// run executes dleplugin hosts with args and stdin; it returns the exit code
// and the error output.
func run(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	e := moduletest.NewEnv(t, stdin, Module())
	code := e.Run(append([]string{"hosts"}, args...)...)
	return code, e.ErrB.String()
}

func TestHelp(t *testing.T) {
	for _, sub := range []string{"", "up", "down", "compose", "list", "add", "remove", "protect", "path", "port", "versions", "config", "config set"} {
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

func TestProtectArgumentsValidatedBeforeStack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stack")
	t.Setenv("DLEHOST_DIR", dir)
	t.Setenv("DLEHOST_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	for _, args := range [][]string{{"protect", "shop"}, {"protect", "yes"}, {"protect", "shop", "maybe"}, {"add", "shop", "--protect", "yes"}, {"config", "set", "protect_session_ttl", "5x"}} {
		if code, _ := run(t, "", args...); code == 0 {
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
		{"pässwörd\n", "pässwörd", true}, {strings.Repeat("a", 72) + "\r\n", strings.Repeat("a", 72), true},
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
	cli := func(args ...string) (int, string) { return run(t, "cli-secret\n", args...) }
	if code, out := cli("protect", "on"); code == 0 || !strings.Contains(out, "no global password") {
		t.Fatalf("enable without password: %d %s", code, out)
	}
	if code, _ := cli("protect", "password", "--default"); code == 0 {
		t.Fatal("accepted global --default")
	}
	if code, _ := cli("protect", "password", "cli-secret"); code == 0 {
		t.Fatal("accepted positional password")
	}
	if code, out := cli("protect", "password"); code != 0 {
		t.Fatal(out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "data", "protect", "passwords.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cli-secret") || !strings.Contains(string(data), "$2a$10$") {
		t.Fatal("CLI did not store only bcrypt hash")
	}
	if code, out := cli("protect", "on"); code != 0 {
		t.Fatal(out)
	}
	cfg, err := config.Load()
	if err != nil || !cfg.Protect {
		t.Fatal("global protect not persisted")
	}
	if code, out := cli("protect"); code != 0 {
		t.Fatal(out)
	}
}

func TestComposePassthrough(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nshift 5\nprintf '%s\\n' \"$@\"\nexit 3\n" // drops compose --project-directory DIR -f FILE
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DLEHOST_DIR", filepath.Join(t.TempDir(), "stack"))
	t.Setenv("DLEHOST_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	for _, tc := range []struct {
		args    []string
		want    string
		verbose bool
	}{
		{[]string{"-v", "hosts", "compose", "ps"}, "ps\n", true},
		{[]string{"hosts", "compose", "--verbose", "logs", "-f"}, "logs\n-f\n", true},
		{[]string{"hosts", "compose", "-f", "x.yml", "ps", "-v"}, "-f\nx.yml\nps\n-v\n", false},
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
