package deploysites_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/deploysites"
)

func TestEffectiveProjectReplacesWholeRecord(t *testing.T) {
	dir := t.TempDir()
	global, project := filepath.Join(dir, "global.yaml"), filepath.Join(dir, "project.yaml")
	for path, body := range map[string]string{
		global:  "sites:\n  example.com:\n    login: global\n    admin_path: panel.php\n    password_env: GLOBAL_PASSWORD\n  other.com:\n    login: other\n",
		project: "sites:\n  example.com:\n    login: project\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := deploysites.Effective(global, project)
	if err != nil {
		t.Fatal(err)
	}
	e := entries["example.com"]
	if e.Scope != "project" || e.Path != project || e.Site.Login != "project" || e.Site.PasswordEnv != "" {
		t.Fatalf("project did not replace the whole record: %+v", e)
	}
	admin, err := e.Site.AdminURL(e.Host)
	if err != nil || admin.String() != "https://example.com/admin.php" {
		t.Fatalf("global admin path leaked: %v, %v", admin, err)
	}
	if entries["other.com"].Scope != "global" {
		t.Fatal("global-only site missing")
	}
}

func TestLoadValidation(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"invalid YAML", "sites: [", "read deployment config"},
		{"unknown field", "sites:\n  example.com:\n    login: admin\n    password: secret\n", "field password not found"},
		{"missing login", "sites:\n  example.com: {}\n", "missing login"},
		{"URL key", "sites:\n  https://example.com:\n    login: admin\n", "invalid host key"},
		{"uppercase key", "sites:\n  EXAMPLE.com:\n    login: admin\n", "invalid host key"},
		{"wrong host", "sites:\n  example.com:\n    url: https://other.com\n    login: admin\n", "different host"},
		{"credentials", "sites:\n  example.com:\n    url: https://admin:secret@example.com\n    login: admin\n", "must not contain credentials"},
		{"multiple documents", "sites: {}\n---\nsites: {}\n", "one YAML document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), deploysites.Filename)
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := deploysites.Load(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSaveAndMissingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", deploysites.Filename)
	d, err := deploysites.Load(path)
	if err != nil || len(d.Sites) != 0 {
		t.Fatalf("missing file = %v, %v", d, err)
	}
	d.Sites["example.com:8443"] = deploysites.Site{Login: "admin"}
	if err := deploysites.Save(path, d); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions: %v, %v", st, err)
	}
	d, err = deploysites.Load(path)
	if err != nil || d.Sites["example.com:8443"].Login != "admin" {
		t.Fatalf("round trip = %v, %v", d, err)
	}
}
