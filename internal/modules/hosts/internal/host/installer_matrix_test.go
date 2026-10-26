package host

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/modules/hosts/internal/dist"
)

// DLEPLUGIN_HOSTS_TEST_DISTS=/path/to/dists go test ./internal/modules/hosts/internal/host -run TestInstallerMatrix -v -timeout 20m
func TestInstallerMatrix(t *testing.T) {
	dir := os.Getenv("DLEPLUGIN_HOSTS_TEST_DISTS")
	if dir == "" {
		t.Skip("set DLEPLUGIN_HOSTS_TEST_DISTS to licensed clean distributions")
	}
	versions, err := dist.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := installerTestStack(t)
	installed := 0
	for _, version := range versions {
		installed++
		t.Run(version, func(t *testing.T) {
			name := "v" + strings.ReplaceAll(version, ".", "-")
			result, err := Add(s, AddOptions{Name: name, DLE: version, DefaultPHP: "8.5", PHPVersions: []string{"7.4", "8.5"},
				DistsDir: dir, AdminPath: "control.php", Admin: Credentials{"owner", "matrix-test-password"}}, func(msg string) { t.Log(msg) })
			if err != nil {
				t.Fatal(err)
			}
			if got, err := GetSetting(s, name, "admin_path"); err != nil || got != "control.php" {
				t.Fatalf("created admin_path: %q %v", got, err)
			}
			php := result.Host.PHP
			wantPHP := "8.5"
			if dist.VersionLess(version, "16.1") {
				wantPHP = "7.4"
			}
			if php != wantPHP {
				t.Fatalf("automatic PHP: got %s, want %s", php, wantPHP)
			}
			root := s.HostDir(name)
			for _, file := range []string{"install.php", "templates/default_templates.zip"} {
				if _, err := os.Stat(filepath.Join(root, file)); !os.IsNotExist(err) {
					t.Errorf("left %s", file)
				}
			}
			// Verify persisted configuration, real schema and the installer-generated user.
			check := fmt.Sprintf(`<?php
define('DATALIFEENGINE', true);
define('ROOT_DIR', '/var/www/%s');
define('ENGINE_DIR', ROOT_DIR.'/engine');
require ENGINE_DIR.'/data/config.php';
if ($config['version_id'] !== '%s' || $config['http_home_url'] !== '%s' || $config['allow_alt_url'] != 1 || $config['site_offline'] != 1) { fwrite(STDERR, 'wrong site configuration'); exit(2); }
if (!is_file(ROOT_DIR.'/templates/'.$config['skin'].'/main.tpl')) exit(3);
require ENGINE_DIR.'/classes/mysql.php';
require ENGINE_DIR.'/data/dbconfig.php';
$db->connect(DBUSER, DBPASS, DBNAME, DBHOST);
$users = $db->super_query('SELECT COUNT(*) AS n FROM '.PREFIX.'_users');
$user = $db->super_query('SELECT * FROM '.PREFIX.'_users WHERE user_id=1');
$passwordOK = version_compare('%s', '11.2', '<') ? hash_equals(md5(md5('matrix-test-password')), $user['password']) : password_verify('matrix-test-password', $user['password']);
if ($users['n'] != 1 || $user['name'] !== 'owner' || $user['user_group'] != 1 || !$passwordOK) { fwrite(STDERR, 'wrong administrator'); exit(4); }
$posts = $db->super_query('SELECT COUNT(*) AS n FROM '.PREFIX.'_post');
if (!$posts['n']) exit(5);
`, name, version, result.URL, version)
			if err := s.Compose(strings.NewReader(check), "exec", "-T", "php"+strings.ReplaceAll(php, ".", ""), "php", "-d", "display_errors=stderr"); err != nil {
				t.Fatal(err)
			}
			client := &gateClient{t: t, base: "http://127.0.0.1:" + s.HTTPPort(), http: &http.Client{Timeout: 20 * time.Second,
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
			for _, adminPath := range []string{"control-test.php", "admin.php"} {
				previous, err := GetSetting(s, name, "admin_path")
				if err != nil {
					t.Fatal(err)
				}
				if err := SetSetting(s, name, "admin_path", adminPath); err != nil {
					t.Fatal(err)
				}
				if got, err := GetSetting(s, name, "admin_path"); err != nil || got != adminPath {
					t.Fatalf("admin_path: %s %v", got, err)
				}
				admin := client.do(result.Host.Domain(), "GET", "/"+adminPath, nil, nil)
				if admin.code != 200 || !strings.Contains(admin.body, `name="username"`) || strings.Contains(admin.body, "Fatal error") {
					t.Fatalf("admin %s: HTTP %d %.1500s", adminPath, admin.code, admin.body)
				}
				// DLE submits its login form to the current URL (action="").
				runtimeCheck := fmt.Sprintf("<?php define('DATALIFEENGINE', true); require '/var/www/%s/engine/data/config.php'; if ($config['admin_path'] !== '%s' || !is_file('/var/www/%s/' . $config['admin_path'])) exit(2);", name, adminPath, name)
				if err := s.Compose(strings.NewReader(runtimeCheck), "exec", "-T", "php"+strings.ReplaceAll(php, ".", ""), "php"); err != nil {
					t.Fatal(err)
				}
				old := client.do(result.Host.Domain(), "GET", "/"+previous, nil, nil)
				if old.code == 200 && strings.Contains(old.body, `name="username"`) {
					t.Fatalf("old admin still available: %s", previous)
				}
				if got, err := GetSetting(s, name, "site_offline"); err != nil || got != "on" {
					t.Fatalf("maintenance changed: %s %v", got, err)
				}
			}
			resp := client.do(result.Host.Domain(), "GET", "/", nil, nil)
			// Older DLE versions serve their maintenance page with HTTP 200.
			if resp.code != 200 && resp.code != 503 || !strings.Contains(resp.body, "Сайт находится на текущей реконструкции") || strings.Contains(resp.body, "Fatal error") {
				t.Fatalf("site: HTTP %d %.1500s", resp.code, resp.body)
			}
		})
	}
	if installed == 0 {
		t.Fatal("no clean DLE 11.0+ archives found")
	}
}
