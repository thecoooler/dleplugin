package deploy_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/dleadmin"
	"github.com/thecoooler/dleplugin/internal/dleadmin/dleadmintest"
	"github.com/thecoooler/dleplugin/internal/modules/deploy"
	"github.com/thecoooler/dleplugin/internal/modules/deploy/internal/deploysites"
	"github.com/zalando/go-keyring"
)

func saveSites(t *testing.T, path string, sites map[string]deploysites.Site) {
	t.Helper()
	if err := deploysites.Save(path, &deploysites.Document{Sites: sites}); err != nil {
		t.Fatal(err)
	}
}

func TestSitesCRUDAndScope(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	s.AdminPath = "panel.php"
	e := newEnv(t, "", false)
	fixture(t, "admin_section_only", e.work)
	t.Setenv("TEST_SITE_PASSWORD", "secret")
	host, _ := deploysites.Host(s.URL)
	project := filepath.Join(e.work, deploysites.Filename)
	global := filepath.Join(filepath.Dir(e.rt.Config.Path()), deploysites.Filename)
	for _, scope := range []string{"project", "global"} {
		args := []string{"deploy", "sites", "add", s.URL, "-u", "admin", "--admin-path", "panel.php", "--password-env", "TEST_SITE_PASSWORD"}
		path := project
		if scope == "global" {
			args = append(args, "--global")
			path = global
		}
		if code := e.run(args...); code != 0 {
			t.Fatalf("add %s: %s", scope, e.errB)
		}
		body, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(body), "secret") {
			t.Fatalf("config contains password or is missing: %s, %v", body, err)
		}
		d, err := deploysites.Load(path)
		if err != nil || d.Sites[host].AdminPath != "panel.php" || d.Sites[host].PasswordEnv != "TEST_SITE_PASSWORD" {
			t.Fatalf("saved site: %+v, %v", d, err)
		}
		if code := e.run(args...); code != 1 || !strings.Contains(e.errB.String(), "already exists") {
			t.Fatalf("duplicate not rejected: %s", e.errB)
		}
	}
	// Mutations and building work from a nested project directory.
	nested := filepath.Join(e.work, "src", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	e.rt.SetWorkDir(nested)
	if code := e.run("deploy", "sites", "edit", host, "--password-env", ""); code != 1 {
		t.Fatal("edit should need a password after clearing password_env")
	}
	t.Setenv(deploy.PasswordEnv, "secret")
	if code := e.run("deploy", "sites", "edit", host, "--password-env", ""); code != 0 {
		t.Fatalf("edit: %s", e.errB)
	}
	d, err := deploysites.Load(project)
	if err != nil || d.Sites[host].PasswordEnv != "" || d.Sites[host].AdminPath != "panel.php" {
		t.Fatalf("edit did not retain settings or clear password_env: %+v, %v", d, err)
	}
	e.out.Reset()
	if code := e.run("deploy", "sites", "list"); code != 0 || !strings.Contains(e.out.String(), "project") || strings.Contains(e.out.String(), "global") {
		t.Fatalf("list: %s, %s", e.out, e.errB)
	}
	e.out.Reset()
	if code := e.run("deploy", "sites", "show", host); code != 0 || !strings.Contains(e.out.String(), "/panel.php") {
		t.Fatalf("show: %s, %s", e.out, e.errB)
	}
	if code := e.run("deploy", "sites", "remove", host); code != 0 {
		t.Fatalf("remove: %s", e.errB)
	}
	e.out.Reset()
	if code := e.run("deploy", "sites", "list"); code != 0 || !strings.Contains(e.out.String(), "global") {
		t.Fatalf("global record not revealed after remove: %s", e.out)
	}
	if code := e.run("deploy", "sites", "remove", host, "--global"); code != 0 {
		t.Fatalf("remove global: %s", e.errB)
	}
	if len(s.Uploads()) != 0 {
		t.Fatal("site management uploaded a plugin")
	}
}

func TestSitesOutsideProjectAndFailedLogin(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	e := newEnv(t, "", false)
	t.Setenv(deploy.PasswordEnv, "wrong")
	if code := e.run("deploy", "sites", "add", s.URL, "-u", "admin"); code != 1 || !strings.Contains(e.errB.String(), "use --global") {
		t.Fatalf("local add outside project: %s", e.errB)
	}
	if code := e.run("deploy", "sites", "add", s.URL, "-u", "admin", "--global"); code != 1 || !strings.Contains(e.errB.String(), "login failed") {
		t.Fatalf("failed login: %s", e.errB)
	}
	path := filepath.Join(filepath.Dir(e.rt.Config.Path()), deploysites.Filename)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed login created a config: %v", err)
	}
	t.Setenv(deploy.PasswordEnv, "secret")
	if code := e.run("deploy", "sites", "add", s.URL, "-u", "admin", "--global"); code != 0 {
		t.Fatalf("global add outside project: %s", e.errB)
	}
}

func TestAddBareHostAndUploadGlobalOutsideProject(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 5, Name: "Ready Plugin"})
	host := "demo.localhost:" + strings.Split(strings.TrimPrefix(s.URL, "http://"), ":")[1]
	e := newEnv(t, "secret\n", false)
	writeXML(t, e.work, "Ready Plugin")
	if code := e.run("deploy", "sites", "add", host, "--global", "-u", "admin", "--password-stdin"); code != 0 {
		t.Fatalf("bare host add: %s", e.errB)
	}
	path := filepath.Join(filepath.Dir(e.rt.Config.Path()), deploysites.Filename)
	d, err := deploysites.Load(path)
	if err != nil || d.Sites[host].URL != "http://"+host || d.Sites[host].AdminPath != "admin.php" {
		t.Fatalf("localhost defaults not saved: %+v, %v", d, err)
	}
	t.Setenv(deploy.PasswordEnv, "secret")
	if code := e.run("deploy", "upload", host, "-f", "ready.xml"); code != 0 || len(s.Uploads()) != 1 {
		t.Fatalf("global upload outside project: %s", e.errB)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(deploy.PasswordEnv, "wrong")
	if code := e.run("deploy", "sites", "edit", host, "--global"); code != 1 {
		t.Fatal("invalid credentials accepted by edit")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("failed edit changed config: %v", err)
	}
}

func TestUploadSavedSite(t *testing.T) {
	for _, mode := range []string{"env", "stdin", "interactive", "flags", "empty env"} {
		t.Run(mode, func(t *testing.T) {
			s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 3, Name: "Ready Plugin"})
			s.AdminPath = "panel.php"
			input := ""
			if mode == "stdin" {
				input = "secret\n"
			} else if mode == "interactive" {
				input = "1\n"
			}
			e := newEnv(t, input, mode == "interactive")
			fixture(t, "admin_section_only", e.work)
			writeXML(t, e.work, "Ready Plugin")
			host, _ := deploysites.Host(s.URL)
			saveSites(t, filepath.Join(filepath.Dir(e.rt.Config.Path()), deploysites.Filename), map[string]deploysites.Site{
				host: {URL: s.URL, Login: "wrong", AdminPath: "wrong.php", PasswordEnv: "WRONG_SITE_PASSWORD"},
			})
			site := deploysites.Site{URL: s.URL, Login: "admin", AdminPath: "panel.php", PasswordEnv: "TEST_SITE_PASSWORD"}
			if mode == "flags" {
				site.Login, site.AdminPath, site.PasswordEnv = "wrong", "wrong.php", "WRONG_SITE_PASSWORD"
			}
			saveSites(t, filepath.Join(e.work, deploysites.Filename), map[string]deploysites.Site{host: site})
			t.Setenv("TEST_SITE_PASSWORD", "secret")
			t.Setenv("WRONG_SITE_PASSWORD", "wrong")
			args := []string{"deploy", "upload", "-f", "ready.xml"}
			if mode != "interactive" {
				args = append(args, host)
			}
			if mode == "stdin" {
				t.Setenv("TEST_SITE_PASSWORD", "wrong")
				args = append(args, "--password-stdin")
			} else if mode == "flags" {
				args = append(args, "-u", "admin", "--admin-path", "panel.php", "--password-env", "TEST_SITE_PASSWORD")
			} else if mode == "empty env" {
				t.Setenv("TEST_SITE_PASSWORD", "")
				t.Setenv(deploy.PasswordEnv, "secret")
			}
			code := e.run(args...)
			if mode == "empty env" {
				if code != 1 || !strings.Contains(e.errB.String(), "TEST_SITE_PASSWORD") || len(s.Uploads()) != 0 {
					t.Fatalf("empty site env fell back: %s", e.errB)
				}
				return
			}
			if code != 0 || len(s.Uploads()) != 1 || s.Uploads()[0].ID != "3" {
				t.Fatalf("saved upload: %s, %+v", e.errB, s.Uploads())
			}
		})
	}
}

func TestInteractiveAddAndEdit(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	e := newEnv(t, s.URL+"\nadmin\nsecret\n\n\nsecret\n", true)
	fixture(t, "admin_section_only", e.work)
	if code := e.run("deploy", "sites", "add"); code != 0 {
		t.Fatalf("interactive add: %s", e.errB)
	}
	host, _ := deploysites.Host(s.URL)
	if code := e.run("deploy", "sites", "edit", host); code != 0 {
		t.Fatalf("interactive edit: %s", e.errB)
	}
	if !strings.Contains(e.out.String(), "Login [admin]") || !strings.Contains(e.out.String(), "Admin script path [admin.php]") {
		t.Fatalf("edit did not prompt with defaults: %s", e.out)
	}
}

func TestDeployCommandHelp(t *testing.T) {
	for _, args := range [][]string{
		{"deploy"}, {"deploy", "sites"}, {"deploy", "sites", "list", "--help"},
		{"deploy", "sites", "add", "--help"}, {"deploy", "sites", "edit", "--help"},
		{"deploy", "sites", "show", "--help"}, {"deploy", "sites", "remove", "--help"},
	} {
		e := newEnv(t, "", false)
		if code := e.run(args...); code != 0 || !strings.Contains(e.out.String(), "Usage:") {
			t.Fatalf("%v: %s, %s", args, e.out, e.errB)
		}
	}
}

func TestSavedPassword(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 3, Name: "Ready Plugin"})
	e := newEnv(t, "secret\n", false)
	fixture(t, "admin_section_only", e.work)
	writeXML(t, e.work, "Ready Plugin")
	host, _ := deploysites.Host(s.URL)
	creds := e.credentials(t)
	t.Cleanup(func() { creds.DeletePassword(host, "admin") })
	if code := e.run("deploy", "sites", "add", s.URL, "-u", "admin", "--password-stdin"); code != 0 {
		t.Fatalf("add: %s", e.errB)
	}
	body, _ := os.ReadFile(filepath.Join(e.work, deploysites.Filename))
	if strings.Contains(string(body), "secret") {
		t.Fatalf("config contains password: %s", body)
	}
	if p, loc, err := creds.Password(host, "admin"); err != nil || loc != deploysites.InKeyring || p != "secret" {
		t.Fatalf("password not saved: %q, %v, %v", p, loc, err)
	}
	// Non-interactive upload without any password source uses the saved one.
	if code := e.run("deploy", "upload", "-f", "ready.xml", host); code != 0 || len(s.Uploads()) != 1 {
		t.Fatalf("upload with saved password: %s", e.errB)
	}
	e.out.Reset()
	if code := e.run("deploy", "sites", "show", host); code != 0 || !strings.Contains(e.out.String(), "Saved password: yes") {
		t.Fatalf("show: %s, %s", e.out, e.errB)
	}
	// A rejected saved password is asked again and replaced.
	if _, _, err := creds.SavePassword(host, "admin", "stale"); err != nil {
		t.Fatal(err)
	}
	e.rt.UI.In = strings.NewReader("secret\n")
	e.rt.UI.SetInteractive(true)
	if code := e.run("deploy", "upload", "-f", "ready.xml", host); code != 0 || !strings.Contains(e.errB.String(), "saved password was rejected") {
		t.Fatalf("retry after rejected password: %s", e.errB)
	}
	if p, _, _ := creds.Password(host, "admin"); p != "secret" {
		t.Fatalf("password not replaced: %q", p)
	}
	if code := e.run("deploy", "sites", "remove", host); code != 0 {
		t.Fatalf("remove: %s", e.errB)
	}
	if _, loc, _ := creds.Password(host, "admin"); loc != "" {
		t.Fatal("password not deleted with the site")
	}
}

func TestEnvPasswordIsNotSaved(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	e := newEnv(t, "", false)
	fixture(t, "admin_section_only", e.work)
	t.Setenv("TEST_SITE_PASSWORD", "secret")
	host, _ := deploysites.Host(s.URL)
	if code := e.run("deploy", "sites", "add", s.URL, "-u", "admin", "--password-env", "TEST_SITE_PASSWORD"); code != 0 {
		t.Fatalf("add: %s", e.errB)
	}
	if _, loc, _ := e.credentials(t).Password(host, "admin"); loc != "" {
		t.Fatal("password from the environment was saved")
	}
}

func TestPasswordFileFallback(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	t.Cleanup(keyring.MockInit)
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 3, Name: "Ready Plugin"})
	e := newEnv(t, "secret\n", false)
	fixture(t, "admin_section_only", e.work)
	writeXML(t, e.work, "Ready Plugin")
	host, _ := deploysites.Host(s.URL)
	creds := e.credentials(t)
	if code := e.run("deploy", "sites", "add", s.URL, "-u", "admin", "--password-stdin"); code != 0 {
		t.Fatalf("add: %s", e.errB)
	}
	if !strings.Contains(e.errB.String(), "plain text to "+creds.Path) {
		t.Fatalf("no fallback warning: %s", e.errB)
	}
	if p, loc, err := creds.Password(host, "admin"); err != nil || loc != deploysites.InFile || p != "secret" {
		t.Fatalf("password not saved to file: %q, %v, %v", p, loc, err)
	}
	if code := e.run("deploy", "upload", "-f", "ready.xml", host); code != 0 || len(s.Uploads()) != 1 {
		t.Fatalf("upload with file password: %s", e.errB)
	}
	e.out.Reset()
	if code := e.run("deploy", "sites", "show", host); code != 0 || !strings.Contains(e.out.String(), "Saved password: yes ("+creds.Path+")") {
		t.Fatalf("show: %s, %s", e.out, e.errB)
	}
	e.errB.Reset()
	if code := e.run("deploy", "sites", "remove", host); code != 0 || strings.Contains(e.errB.String(), "⚠") {
		t.Fatalf("remove: %s", e.errB)
	}
	if _, loc, _ := creds.Password(host, "admin"); loc != "" {
		t.Fatal("file password not deleted with the site")
	}
}
