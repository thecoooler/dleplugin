package stack

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("DLEPLUGIN_TEST_DOCKER_STUB") == "1" {
		if len(os.Args) != 5 || os.Args[1] != "inspect" || os.Args[2] != "--format" || os.Args[4] != "dleplugin-hosts-apache" {
			os.Exit(1)
		}
		fmt.Print(os.Getenv("DLEPLUGIN_TEST_DOCKER_RESULT"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

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
	if err := s.CheckPorts(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP_PORT") || !strings.Contains(err.Error(), httpPort) || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("HTTP conflict: %v", err)
	}
	http.Close()
	if err := s.CheckPorts(context.Background()); err == nil || !strings.Contains(err.Error(), "DB_PORT") || !strings.Contains(err.Error(), dbPort) || !strings.Contains(err.Error(), "already in use") {
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

func TestClassifyListenError(t *testing.T) {
	for _, tc := range []struct {
		name, goos       string
		err              error
		busy, permission bool
	}{
		{"portable busy", "linux", syscall.EADDRINUSE, true, false},
		{"portable permission", "linux", os.ErrPermission, false, true},
		{"Windows busy", "windows", syscall.Errno(10048), true, false},
		{"Windows access denied", "windows", syscall.Errno(10013), false, true},
		{"Windows error elsewhere", "linux", syscall.Errno(10048), false, false},
		{"other error", "windows", fmt.Errorf("other"), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			busy, permission := classifyListenError(fmt.Errorf("bind: %w", tc.err), tc.goos)
			if busy != tc.busy || permission != tc.permission {
				t.Fatalf("got busy=%v permission=%v, want %v %v", busy, permission, tc.busy, tc.permission)
			}
		})
	}
}

func TestSameStackDir(t *testing.T) {
	for _, tc := range []struct {
		goos, a, b string
		want       bool
	}{
		{"windows", "C:/Users/Work/stack", "c:/users/work/STACK", true},
		{"windows", "C:/Users/Work/stack", "c:/users/work/other", false},
		{"linux", "/home/Work/stack", "/home/work/stack", false},
		{"linux", "/home/work/./stack", "/home/work/stack", true},
	} {
		if got := sameStackDir(tc.a, tc.b, tc.goos); got != tc.want {
			t.Errorf("%s: sameStackDir(%q, %q) = %v, want %v", tc.goos, tc.a, tc.b, got, tc.want)
		}
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
	labelDir := dir
	if runtime.GOOS == "windows" {
		labelDir = strings.ToUpper(dir)
	}
	result := fmt.Sprintf("true\t%s\tapache\t{\"80/tcp\":[{\"HostIp\":\"127.0.0.1\",\"HostPort\":\"%s\"}]}\n", labelDir, port)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	dockerName := "docker"
	if runtime.GOOS == "windows" {
		dockerName += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, dockerName), data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DLEPLUGIN_TEST_DOCKER_STUB", "1")
	t.Setenv("DLEPLUGIN_TEST_DOCKER_RESULT", result)
	t.Setenv("PATH", bin)
	t.Setenv("BIND_ADDRESS", "127.0.0.1")
	t.Setenv("COMPOSE_PROJECT_NAME", "dleplugin-hosts")
	t.Setenv("HTTP_PORT", port)
	t.Setenv("DB_PORT", dbPort)
	if err := (&Stack{Dir: dir}).CheckPorts(context.Background()); err != nil {
		t.Fatal(err)
	}
}
