package host

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/stack"
)

// Uses a user-provided licensed distribution, never downloads or embeds DLE.
// DLEHOST_TEST_DLE21=/path/to/21.0.zip go test ./internal/modules/hosts/internal/host -run TestInstallerDocker -v
func TestInstallerDocker(t *testing.T) {
	archive := os.Getenv("DLEHOST_TEST_DLE21")
	if archive == "" {
		t.Skip("set DLEHOST_TEST_DLE21 to an original DLE 21.0 zip")
	}
	archive, err := filepath.Abs(archive)
	if err != nil {
		t.Fatal(err)
	}
	s := installerTestStack(t)
	dists := t.TempDir()
	if err := os.Symlink(archive, filepath.Join(dists, "21.0.zip")); err != nil {
		t.Fatal(err)
	}
	for _, external := range []bool{false, true} {
		name := "clean"
		protect := "off"
		if external {
			name, protect = "secure", "on"
			s.BaseDomain, s.ExternalOrigin = "installer.test", "https://installer.test"
			if err := SetPassword(s, "", []byte("gate-test-password"), false, ProtectSettings{}, func(string) {}); err != nil {
				t.Fatal(err)
			}
		}
		t.Run(name, func(t *testing.T) {
			result, err := Add(s, AddOptions{Name: name, DLE: "21.0", PHP: "8.5", PHPVersions: []string{"8.5"},
				DistsDir: dists, Admin: Credentials{"owner", "installer-test-password"}, Protect: protect}, func(msg string) { t.Log(msg) })
			if err != nil {
				t.Fatal(err)
			}
			root := s.HostDir(name)
			config := readTestFile(t, filepath.Join(root, "engine/data/config.php"))
			for _, want := range []string{"'version_id' => '21.0'", "'http_home_url' => '" + result.URL + "'", "'allow_alt_url' => '1'"} {
				if !strings.Contains(config, want) {
					t.Errorf("config missing %s", want)
				}
			}
			for _, path := range []string{"install.php", "templates/default_templates.zip", "Documentation"} {
				if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
					t.Errorf("unexpected file: %s", path)
				}
			}
			// Check actual schema/data, administrator password, demo images and banners.
			// The standard installer is the only path for creating this site.
			check := fmt.Sprintf(`<?php
define('DATALIFEENGINE', true);
define('ROOT_DIR', '/var/www/%s');
require ROOT_DIR.'/engine/classes/mysql.php';
require ROOT_DIR.'/engine/data/dbconfig.php';
$db->connect(DBUSER, DBPASS, DBNAME, DBHOST);
$users = $db->super_query('SELECT COUNT(*) AS n FROM '.PREFIX.'_users');
$user = $db->super_query('SELECT * FROM '.PREFIX.'_users WHERE user_id=1');
if ($users['n'] != 1 || $user['name'] !== 'owner' || $user['user_group'] != 1 || !password_verify('installer-test-password', $user['password'])) exit(2);
$posts = $db->super_query('SELECT COUNT(*) AS n FROM '.PREFIX.'_post');
if (!$posts['n']) exit(3);
$banners = $db->query('SELECT code FROM '.PREFIX.'_banners');
while ($row = $db->get_row($banners)) { if (strpos($row['code'], '%s') === false) exit(4); }
echo 'verified';
`, name, result.URL)
			if err := s.Compose(strings.NewReader(check), "exec", "-T", "php85", "php"); err != nil {
				t.Fatal(err)
			}
			client := &gateClient{t: t, base: "http://127.0.0.1:" + s.HTTPPort(), http: &http.Client{Timeout: 20 * time.Second,
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
			if external {
				client.expectGate(result.Host.Domain(), "/")
			} else {
				resp := client.do(result.Host.Domain(), "GET", "/", nil, nil)
				if resp.code != 200 || !strings.Contains(resp.body, "<!DOCTYPE html>") && !strings.Contains(resp.body, "<!doctype html>") {
					t.Fatalf("site: HTTP %d %.500s", resp.code, resp.body)
				}
			}
		})
	}
	t.Run("rollback after installer error", func(t *testing.T) {
		badDists := t.TempDir()
		source, err := zip.OpenReader(archive)
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		file, err := os.Create(filepath.Join(badDists, "21.0.zip"))
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		for _, entry := range source.File {
			dest, err := writer.Create(entry.Name)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(entry.Name, "templates/default_templates.zip") {
				if _, err := io.WriteString(dest, "broken template archive"); err != nil {
					t.Fatal(err)
				}
				continue
			}
			src, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(dest, src)
			src.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = Add(s, AddOptions{Name: "broken", DLE: "21.0", PHP: "8.5", PHPVersions: []string{"8.5"},
			DistsDir: badDists, Admin: Credentials{"owner", "installer-test-password"}}, func(msg string) { t.Log(msg) })
		if err == nil || !strings.Contains(err.Error(), "installtemplate") {
			t.Fatalf("expected template error, got %v", err)
		}
		for _, path := range []string{s.HostDir("broken"), vhostPath(s, "broken")} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("rollback left %s", path)
			}
		}
		entries, err := os.ReadDir(s.StagingDir())
		if err != nil || len(entries) != 0 {
			t.Fatalf("staging not cleaned: %v %v", entries, err)
		}
		hosts, err := loadRegistry(s)
		if err != nil {
			t.Fatal(err)
		}
		if _, found := hosts["broken"]; found {
			t.Error("failed host registered")
		}
		// Recreating both succeeds only if rollback removed the DB and its user.
		if err := s.SQL(createDatabaseSQL("dle_broken") + createUserSQL("dle_broken", "temporary-test-password") + dropDatabaseSQL("dle_broken") + dropUserSQL("dle_broken")); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+s.Env["COMPOSE_PROJECT_NAME"],
			"--filter", "label=com.docker.compose.oneoff=True").Output()
		if err != nil || len(strings.TrimSpace(string(out))) != 0 {
			t.Fatalf("installer container left: %s %v", out, err)
		}
	})
}

func installerTestStack(t *testing.T) *stack.Stack {
	t.Helper()
	dir, err := os.MkdirTemp("", "dlehost-install-test-")
	if err != nil {
		t.Fatal(err)
	}
	project := "dlehostinstall" + randomTestHex(t, 4)
	t.Setenv("DLEHOST_DIR", dir)
	t.Setenv("DLEHOST_COMPOSE_PROJECT_NAME", project)
	t.Setenv("DLEHOST_BIND_ADDRESS", "127.0.0.1")
	t.Setenv("DLEHOST_HTTP_PORT", strconv.Itoa(freePort(t)))
	t.Setenv("DLEHOST_DB_PORT", strconv.Itoa(freePort(t)))
	s, err := stack.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			_ = s.ComposeAttached(context.Background(), nil, "logs", "--tail", "30", "apache", "php85")
		}
		_ = s.Compose(nil, "down", "--volumes", "--rmi", "local")
		_ = exec.Command("docker", "run", "--rm", "--network", "none", "-v", dir+":/stack", "--entrypoint", "rm", "mariadb:11.4", "-rf", "/stack/data/mariadb").Run()
		_ = os.RemoveAll(dir)
	})
	if err := s.Compose(nil, "up", "-d", "--build", "--wait"); err != nil {
		t.Fatal(err)
	}
	return s
}
