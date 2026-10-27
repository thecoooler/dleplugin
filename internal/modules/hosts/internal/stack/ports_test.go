package stack

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckPorts(t *testing.T) {
	listen := func() net.Listener {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		return ln
	}
	http, db := listen(), listen()
	httpPort := fmt.Sprint(http.Addr().(*net.TCPAddr).Port)
	dbPort := fmt.Sprint(db.Addr().(*net.TCPAddr).Port)
	t.Setenv("BIND_ADDRESS", "127.0.0.1")
	t.Setenv("HTTP_PORT", httpPort)
	t.Setenv("DB_PORT", dbPort)
	// Prevent an unrelated local Docker installation from claiming a test port.
	t.Setenv("PATH", t.TempDir())
	s := &Stack{Dir: t.TempDir()}
	if err := s.CheckPorts(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP_PORT") || !strings.Contains(err.Error(), httpPort) {
		t.Fatalf("HTTP conflict: %v", err)
	}
	http.Close()
	if err := s.CheckPorts(context.Background()); err == nil || !strings.Contains(err.Error(), "DB_PORT") || !strings.Contains(err.Error(), dbPort) {
		t.Fatalf("DB conflict: %v", err)
	}
	db.Close()
	if err := s.CheckPorts(context.Background()); err != nil {
		t.Fatalf("free ports: %v", err)
	}
	t.Setenv("DB_PORT", httpPort)
	if err := s.CheckPorts(context.Background()); err == nil || !strings.Contains(err.Error(), "both use") {
		t.Fatalf("duplicate ports: %v", err)
	}
	t.Setenv("HTTP_PORT", "invalid")
	if err := s.CheckPorts(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid HTTP_PORT") {
		t.Fatalf("invalid port: %v", err)
	}
}

func TestMatchesOwnedPort(t *testing.T) {
	dir := t.TempDir()
	p := publishedPort{service: "apache", containerPort: "80/tcp", port: "8080"}
	output := func(running, directory, service, bindings string) string {
		return strings.Join([]string{running, directory, service, bindings}, "\t")
	}
	correct := `{"80/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}]}`
	for _, tc := range []struct {
		name, output string
		want         bool
	}{
		{"matching", output("true", dir, "apache", correct), true},
		{"stopped", output("false", dir, "apache", correct), false},
		{"other stack", output("true", filepath.Join(dir, "other"), "apache", correct), false},
		{"other service", output("true", dir, "db", correct), false},
		{"other port", output("true", dir, "apache", `{"80/tcp":[{"HostIp":"127.0.0.1","HostPort":"8081"}]}`), false},
		{"other address", output("true", dir, "apache", `{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"8080"}]}`), false},
		{"bad data", "invalid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesOwnedPort(tc.output, dir, "127.0.0.1", p); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckPortsAllowsRunningStack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as a Docker stub")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := fmt.Sprint(ln.Addr().(*net.TCPAddr).Port)
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dbPort := fmt.Sprint(free.Addr().(*net.TCPAddr).Port)
	free.Close()
	dir, bin := t.TempDir(), t.TempDir()
	result := fmt.Sprintf("true\t%s\tapache\t{\"80/tcp\":[{\"HostIp\":\"127.0.0.1\",\"HostPort\":\"%s\"}]}\n", dir, port)
	script := "#!/bin/sh\n[ \"$1\" = inspect ] && [ \"$2\" = --format ] && [ \"$4\" = dleplugin-hosts-apache ] || exit 1\nprintf '%s' '" + result + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("BIND_ADDRESS", "127.0.0.1")
	t.Setenv("COMPOSE_PROJECT_NAME", "dleplugin-hosts")
	t.Setenv("HTTP_PORT", port)
	t.Setenv("DB_PORT", dbPort)
	if err := (&Stack{Dir: dir}).CheckPorts(context.Background()); err != nil {
		t.Fatal(err)
	}
}
