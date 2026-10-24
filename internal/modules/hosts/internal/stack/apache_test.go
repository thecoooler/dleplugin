package stack

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt in with an isolated Apache binary; no Docker daemon or system config is used.
// DLEHOST_TEST_APACHE_MODULES points to its modules directory.
func TestApacheUnknownHost(t *testing.T) {
	binary := os.Getenv("DLEHOST_TEST_APACHE")
	if binary == "" {
		t.Skip("set DLEHOST_TEST_APACHE to run the isolated Apache HTTP regression")
	}
	modules := os.Getenv("DLEHOST_TEST_APACHE_MODULES")
	if modules == "" {
		t.Fatal("DLEHOST_TEST_APACHE_MODULES is required")
	}
	dir := t.TempDir()
	if _, err := ensure(dir); err != nil {
		t.Fatal(err)
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Static markers make accidental routing to localhost detectable without PHP.
	for _, name := range []string{"localhost", "shop", "legacy"} {
		write(filepath.Join(dir, "www", name, "index.html"), "site:"+name)
	}
	sites := filepath.Join(dir, "config/apache/sites")
	write(filepath.Join(sites, "60-host-shop.conf"), `<VirtualHost *:80>
 ServerName shop.dle.icooler.opik.net
 DocumentRoot /var/www/shop
 Use PHP 85
</VirtualHost>
`)
	// Every vhost must use the same unprivileged test port for Apache name selection.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(sites)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		path := filepath.Join(sites, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		config := strings.ReplaceAll(string(data), "*:80", fmt.Sprintf("*:%d", port))
		config = strings.ReplaceAll(config, "/var/www", filepath.Join(dir, "www"))
		write(path, config)
	}
	var config strings.Builder
	fmt.Fprintf(&config, "ServerRoot %q\nDefaultRuntimeDir %q\nPidFile %q\nServerName localhost\nListen 127.0.0.1:%d\nErrorLog %q\n", dir, dir, filepath.Join(dir, "apache.pid"), port, filepath.Join(dir, "error.log"))
	for _, name := range []string{"mpm_event", "authz_core", "authz_host", "dir", "macro", "proxy", "proxy_fcgi", "vhost_alias"} {
		fmt.Fprintf(&config, "LoadModule %s_module %q\n", name, filepath.Join(modules, "mod_"+name+".so"))
	}
	fmt.Fprintf(&config, "User #%d\nGroup #%d\nDocumentRoot %q\nDirectoryIndex index.html\nDefine APACHE_LOG_DIR %q\nInclude %q\n", os.Getuid(), os.Getgid(), filepath.Join(dir, "www"), filepath.Join(dir, "logs/apache"), filepath.Join(sites, "*.conf"))
	configPath := filepath.Join(dir, "httpd.conf")
	write(configPath, config.String())
	check := exec.Command(binary, "-t", "-f", configPath)
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("Apache config: %v\n%s", err, output)
	}
	cmd := exec.Command(binary, "-X", "-f", configPath)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			log, _ := os.ReadFile(filepath.Join(dir, "error.log"))
			t.Logf("Apache: %s\n%s", &output, log)
		}
	})
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	t.Cleanup(func() { client.CloseIdleConnections() })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get(base)
		if err == nil {
			response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Apache did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, host := range []string{"unused.dle.icooler.opik.net", "unused.dle.icooler.opik.net:443", "unknown.example", "127.0.0.1"} {
		for _, path := range []string{"/", "/index.php", "/index.html", "/engine/data/dbconfig.php"} {
			t.Run(host+path, func(t *testing.T) {
				request, err := http.NewRequest(http.MethodGet, base+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				request.Host = host
				request.Header.Set("X-Forwarded-Proto", "https")
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != http.StatusForbidden || strings.TrimSpace(string(body)) != "Unknown host" {
					t.Fatalf("unknown host: status=%d body=%q", response.StatusCode, body)
				}
			})
		}
	}
	for host, want := range map[string]string{
		"localhost":                 "site:localhost",
		"shop.dle.icooler.opik.net": "site:shop",
		"legacy.localhost":          "site:legacy",
		"legacy.php74.localhost":    "site:legacy",
		"legacy.php85.localhost":    "site:legacy",
	} {
		request, err := http.NewRequest(http.MethodGet, base+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Host = host
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || string(body) != want {
			t.Errorf("known host %s: status=%d body=%q, want %q", host, response.StatusCode, body, want)
		}
	}
}
