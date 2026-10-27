package host

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// All Docker invocations are intercepted; these tests never contact the daemon.
func isolatedStack(t *testing.T) *stack.Stack {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DLEPLUGIN_HOSTS_DIR", dir)
	t.Setenv("DLEPLUGIN_HOSTS_HTTP_PORT", "8090")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\n"+fakeInstallerPort+"cat >/dev/null\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s, err := stack.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	serveTestInstaller(t, s)
	return s
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAddFromCustomNamedArchive(t *testing.T) {
	s := isolatedStack(t)
	calls, _ := fakeDocker(t)
	dists := t.TempDir()
	writeTestDist(t, dists, "21.0")
	customPath := filepath.Join(dists, "custom-dle-package.zip")
	if err := os.Rename(filepath.Join(dists, "21.0.zip"), customPath); err != nil {
		t.Fatal(err)
	}
	res, err := Add(s, AddOptions{Name: "shop", DistArchive: customPath, PHPVersions: []string{"8.5", "7.4"},
		Admin: Credentials{"admin", "test-password"}}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Host.DLE != "21.0" {
		t.Fatalf("detected DLE: got %s, want 21.0", res.Host.DLE)
	}
	if _, err := os.Stat(s.HostDir("shop")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readTestFile(t, calls), "INSERT INTO") {
		t.Fatal("custom archive triggered a direct SQL import")
	}
}

func TestAddRejectsNonCleanArchive(t *testing.T) {
	s := isolatedStack(t)
	dists := t.TempDir()
	f, err := os.Create(filepath.Join(dists, "21.0.zip"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("engine/data/config.php")
	w.Write([]byte("<?php"))
	z.Close()
	f.Close()

	_, err = Add(s, AddOptions{Name: "shop", DLE: "21.0", PHPVersions: []string{"8.5", "7.4"},
		DistsDir: dists, Admin: Credentials{"admin", "test-password"}}, func(string) {})
	if err == nil {
		t.Fatal("accepted non-clean archive")
	}
}

func TestAddPHPWarnings(t *testing.T) {
	for _, tc := range []struct {
		name, dle, requested, preferred string
		want, absent                    []string
	}{
		{"automatic legacy", "11.0", "", "auto", []string{"PHP 7.4 is end-of-life"}, []string{"may not be compatible"}},
		{"configured legacy", "11.0", "", "7.4", []string{"PHP 7.4 is end-of-life"}, nil},
		{"manual legacy", "11.0", "7.4", "auto", []string{"PHP 7.4 is end-of-life"}, nil},
		{"incompatible default", "11.0", "", "8.5", []string{"PHP 8.5 may not be compatible"}, []string{"PHP 7.4 is end-of-life"}},
		{"incompatible manual", "11.0", "8.5", "auto", []string{"PHP 8.5 may not be compatible"}, []string{"PHP 7.4 is end-of-life"}},
		{"below minimum", "21.0", "7.4", "auto", []string{"PHP 7.4 may not be compatible", "PHP 7.4 is end-of-life"}, nil},
		{"explicit auto ignores preference", "11.0", "auto", "8.5", []string{"PHP 7.4 is end-of-life"}, []string{"may not be compatible"}},
		{"modern", "21.0", "", "auto", nil, []string{"may not be compatible", "end-of-life"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := isolatedStack(t)
			dirs := t.TempDir()
			writeTestDist(t, dirs, tc.dle)
			var warnings []string
			_, err := Add(s, AddOptions{Name: "shop", DLE: tc.dle, DistsDir: dirs, PHP: tc.requested,
				DefaultPHP: tc.preferred, Admin: Credentials{"admin", "password"}, Warn: func(msg string) { warnings = append(warnings, msg) }}, func(string) {})
			// An empty PHPVersions stops before installation, after warning selection.
			if err == nil || !strings.Contains(err.Error(), "not available") {
				t.Fatalf("expected unavailable PHP error, got %v", err)
			}
			got := strings.Join(warnings, "\n")
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("warnings %q missing %q", got, want)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("warnings %q unexpectedly contain %q", got, absent)
				}
			}
		})
	}
}

func TestAddKeepsConfiguredPHPBelowInstallerMinimum(t *testing.T) {
	s := isolatedStack(t)
	dirs := t.TempDir()
	writeTestDist(t, dirs, "21.0")
	var warnings []string
	result, err := Add(s, AddOptions{Name: "shop", DLE: "21.0", DistsDir: dirs,
		DefaultPHP: "7.4", PHPVersions: []string{"7.4", "8.5"}, Admin: Credentials{"admin", "password"},
		Warn: func(msg string) { warnings = append(warnings, msg) }}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if result.Host.PHP != "7.4" || !strings.Contains(strings.Join(warnings, "\n"), "may not be compatible") {
		t.Fatalf("PHP=%s warnings=%v", result.Host.PHP, warnings)
	}
}

func TestAddListPortRouting(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "external"}[external], func(t *testing.T) {
			s := isolatedStack(t)
			if external {
				s.BaseDomain = "dle.icooler.opik.net"
				s.ExternalOrigin = "https://dle.icooler.opik.net"
			}
			dists := t.TempDir()
			writeTestDist(t, dists, "21.0")
			res, err := Add(s, AddOptions{Name: "shop", DLE: "21.0", PHP: "8.5", PHPVersions: []string{"8.5", "7.4"}, DistsDir: dists, Admin: Credentials{"admin", "test-password"}}, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			want := "http://shop.localhost:8090/"
			if external {
				want = "https://shop.dle.icooler.opik.net/"
			}
			if res.URL != want {
				t.Fatalf("add URL=%s", res.URL)
			}
			vhost := readTestFile(t, vhostPath(s, "shop"))
			for _, expected := range []string{"ServerName " + res.Host.Domain(), "DocumentRoot /var/www/shop", "Use PHP 85"} {
				if !strings.Contains(vhost, expected) {
					t.Fatalf("vhost missing %q: %s", expected, vhost)
				}
			}
			if external {
				for _, expected := range []string{
					`ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" HTTPS "on"`,
					`ProxyFCGISetEnvIf "req('X-Forwarded-Proto') == 'https'" REQUEST_SCHEME "https"`,
				} {
					if !strings.Contains(vhost, expected) {
						t.Fatalf("missing FastCGI scheme: %s", vhost)
					}
				}
			}
			if strings.Contains(vhost, "ProxyFCGISetEnvIf") != external {
				t.Fatal("forwarded scheme scope")
			}
			configPath := filepath.Join(s.HostDir("shop"), "engine/data/config.php")
			configBefore := readTestFile(t, configPath)
			if !strings.Contains(configBefore, "'"+want+"'") {
				t.Fatalf("DLE config=%s", configBefore)
			}
			// A subsequent global config change must not rename existing sites.
			s.BaseDomain, s.ExternalOrigin = "other.test", "https://other.test"
			rows, err := List(s)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.Name == "shop" {
					found = true
					if !row.Managed || row.URL(s) != want {
						t.Fatalf("list=%+v", row)
					}
				}
			}
			if !found {
				t.Fatal("shop missing in list")
			}
			if err := os.MkdirAll(filepath.Join(s.HostDir("shop"), "engine/cache"), 0755); err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(s.HostDir("shop"), "engine/cache/page.tmp")
			if err := os.WriteFile(cache, []byte("cached"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := SetHTTPPort(s, 8091, func(string) {}); err != nil {
				t.Fatal(err)
			}
			if s.HTTPPort() != "8091" {
				t.Fatal("port not changed")
			}
			configAfter := readTestFile(t, configPath)
			if external {
				if configAfter != configBefore || readTestFile(t, cache) != "cached" {
					t.Fatal("external site rewritten by port")
				}
			} else {
				if !strings.Contains(configAfter, "http://shop.localhost:8091/") {
					t.Fatalf("local URL not updated: %s", configAfter)
				}
				if _, err := os.Stat(cache); !os.IsNotExist(err) {
					t.Fatal("local cache not cleared")
				}
			}
			if got := readTestFile(t, vhostPath(s, "shop")); got != vhost {
				t.Fatal("vhost rewritten")
			}
		})
	}
}

func TestLegacyRegistryNotMigrated(t *testing.T) {
	s := isolatedStack(t)
	legacy := `[{"name":"dle","dle":"21.0","php":"8.5","database":"dle_dle"}]`
	if err := os.WriteFile(s.RegistryPath(), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	s.BaseDomain, s.ExternalOrigin = "dle.icooler.opik.net", "https://dle.icooler.opik.net"
	hosts, err := loadRegistry(s)
	if err != nil {
		t.Fatal(err)
	}
	h := hosts["dle"]
	if h.Domain() != "dle.localhost" || h.URL(s) != "http://dle.localhost:8090/" {
		t.Fatalf("legacy host changed: %+v", h)
	}
	if got := readTestFile(t, s.RegistryPath()); got != legacy {
		t.Fatal("registry rewritten")
	}
	// Port changes still update old local sites without adopting the new base domain.
	configPath := filepath.Join(s.HostDir("dle"), "engine/data/config.php")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("<?php $config = array('http_home_url' => 'http://dle.localhost:8090/');"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SetHTTPPort(s, 8092, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, configPath); !strings.Contains(got, "http://dle.localhost:8092/") {
		t.Fatalf("legacy port: %s", got)
	}
	if got := readTestFile(t, s.RegistryPath()); got != legacy {
		t.Fatal("legacy registry migrated by port")
	}
	unmanaged := Host{Name: "localhost"}
	if unmanaged.URL(s) != "http://localhost:8092/" {
		t.Fatal("default host advertised externally")
	}
}
