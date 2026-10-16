package deploy_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/dleadmin"
	"github.com/thecoooler/dleplugin/internal/dleadmin/dleadmintest"
	"github.com/thecoooler/dleplugin/internal/modules/buildcmd"
	"github.com/thecoooler/dleplugin/internal/modules/deploy"
)

func TestMain(m *testing.M) {
	// Never touch the real OS credential store.
	keyring.MockInit()
	os.Exit(m.Run())
}

type env struct {
	rt        *app.Runtime
	out, errB *bytes.Buffer
	work      string
}

func newEnv(t *testing.T, stdin string, interactive bool) *env {
	t.Helper()
	t.Setenv(deploy.PasswordEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	e := &env{out: &bytes.Buffer{}, errB: &bytes.Buffer{}, work: t.TempDir()}
	e.rt = app.NewRuntime(strings.NewReader(stdin), e.out, e.errB, filepath.Join(home, "config.yml"))
	e.rt.SetWorkDir(e.work)
	e.rt.UI.SetInteractive(interactive)
	return e
}

func (e *env) run(args ...string) int {
	return app.Execute(context.Background(), e.rt, args, buildcmd.Module(), deploy.Module())
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, name, dst string) {
	t.Helper()
	copyDir(t, filepath.Join("..", "..", "..", "testdata", "golden", "plugin", name, "input"), dst)
}

func writeXML(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, "ready.xml")
	if err := os.WriteFile(p, []byte(`<?xml version="1.0" encoding="utf-8"?><dleplugin><name>`+name+`</name></dleplugin>`), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRegistersCleanly(t *testing.T) {
	e := newEnv(t, "", false)
	root := app.NewRootCommand(e.rt, buildcmd.Module(), deploy.Module())
	if errs := e.rt.ModuleErrors(); len(errs) != 0 {
		t.Fatalf("module errors: %v", errs)
	}
	if c, _, err := root.Find([]string{"deploy"}); err != nil || c.Name() != "deploy" {
		t.Fatalf("deploy not registered: %v", err)
	}
}

func TestHelp(t *testing.T) {
	e := newEnv(t, "", false)
	if code := e.run("deploy", "upload", "--help"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	for _, s := range []string{"Usage:", "--file", "--login", "--admin-path", "--password-stdin", "--new", "--dry-run", "--insecure", deploy.PasswordEnv} {
		if !strings.Contains(e.out.String(), s) {
			t.Errorf("help lacks %q", s)
		}
	}
	if strings.Contains(e.out.String(), "--password ") {
		t.Error("help has --password")
	}
}

func TestBuildAndUpdate(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 7, Name: "Demo Plugin"})
	e := newEnv(t, "secret\n", false)
	fixture(t, "admin_section_only", e.work)
	if code := e.run("deploy", "upload", s.AdminURL(), "-u", "admin", "--password-stdin"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	ups := s.Uploads()
	if len(ups) != 1 || ups[0].ID != "7" || ups[0].Filename != "Demo.xml" || !strings.Contains(ups[0].Content, "<name>Demo Plugin</name>") {
		t.Fatalf("uploads = %+v", ups)
	}
	built, err := os.ReadFile(filepath.Join(e.work, "dist", "Demo.xml"))
	if err != nil || string(built) != ups[0].Content {
		t.Fatalf("uploaded content differs from dist: %v", err)
	}
	host := strings.TrimPrefix(s.URL, "http://")
	want := "✔ Plugin built successfully\n✔ Logged in to " + host + "\n✔ Plugin \"Demo Plugin\" updated on " + host + "\n"
	if e.out.String() != want {
		t.Errorf("out = %q", e.out.String())
	}
}

func TestBuildVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"manifest", nil, "2.0.0"},
		{"flag", []string{"--version", "3.0.0"}, "3.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 7, Name: "Demo Plugin"})
			e := newEnv(t, "secret\n", false)
			fixture(t, "admin_section_only", e.work)
			manifest := filepath.Join(e.work, "dleplugin.yml")
			b, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifest, append([]byte("version: 2.0.0\n"), b...), 0o644); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"deploy", "upload", s.AdminURL(), "-u", "admin", "--password-stdin"}, tc.args...)
			if code := e.run(args...); code != 0 {
				t.Fatalf("exit %d: %s", code, e.errB)
			}
			if ups := s.Uploads(); len(ups) != 1 || !strings.Contains(ups[0].Content, "<version>"+tc.want+"</version>") {
				t.Fatalf("uploads = %+v", ups)
			}
		})
	}
}

func TestBuildSplitUploadsZip(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 2, Name: "QF Plugin"})
	e := newEnv(t, "", false)
	t.Setenv(deploy.PasswordEnv, "secret")
	fixture(t, "quirks_files", e.work)
	manifest := filepath.Join(e.work, "dleplugin.yml")
	b, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, bytes.Replace(b, []byte("name: QF\n"), []byte("name: QF Plugin\n"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := e.run("deploy", "upload", s.AdminURL(), "-u", "admin"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	ups := s.Uploads()
	if len(ups) != 1 || !strings.HasSuffix(ups[0].Filename, ".zip") || ups[0].ID != "2" {
		t.Fatalf("uploads = %+v", ups)
	}
	if _, err := zip.NewReader(bytes.NewReader([]byte(ups[0].Content)), int64(len(ups[0].Content))); err != nil {
		t.Fatalf("not a zip: %v", err)
	}
}

func TestFileSkipsBuild(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 4, Name: "Ready Plugin"})
	e := newEnv(t, "secret", false)
	writeXML(t, e.work, "Ready Plugin")
	if code := e.run("deploy", "upload", s.AdminURL(), "-u", "admin", "--password-stdin", "-f", "ready.xml"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	if strings.Contains(e.out.String(), "built") {
		t.Error("plugin was built")
	}
	if ups := s.Uploads(); len(ups) != 1 || ups[0].ID != "4" || ups[0].Filename != "ready.xml" {
		t.Fatalf("uploads = %+v", ups)
	}
}

func TestUpdateVerifiesPluginIdentity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		plugins    []dleadmin.Plugin
		versions   map[int]string
		editPage   string
		wantError  string
		wantEdits  int
		wantUpload bool
	}{
		{
			name:      "ambiguous zero version",
			plugins:   []dleadmin.Plugin{{ID: 4, Name: "Demo"}, {ID: 7, Name: "Demo0"}},
			versions:  map[int]string{4: "0", 7: "1"},
			wantError: "multiple installed plugins", wantEdits: 0,
		},
		{
			name:      "single wrong identity",
			plugins:   []dleadmin.Plugin{{ID: 4, Name: "Demo"}},
			versions:  map[int]string{4: "0"},
			wantError: `has name "Demo"`, wantEdits: 1,
		},
		{
			name:      "single correct identity",
			plugins:   []dleadmin.Plugin{{ID: 7, Name: "Demo0"}, {ID: 8, Name: "Other"}},
			versions:  map[int]string{7: "1"},
			wantEdits: 1, wantUpload: true,
		},
		{
			name:      "missing edit form",
			plugins:   []dleadmin.Plugin{{ID: 7, Name: "Demo0"}},
			editPage:  `<form>Access denied</form>`,
			wantError: "no unique non-empty name", wantEdits: 1,
		},
	} {
		for _, mode := range []string{"update", "dry-run", "allow-new"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				s := dleadmintest.New(t, "admin", "secret", tc.plugins...)
				s.Versions, s.EditPage = tc.versions, tc.editPage
				e := newEnv(t, "secret\n", false)
				writeXML(t, e.work, "Demo0")
				args := []string{"deploy", "upload", s.AdminURL(), "-u", "admin", "--password-stdin", "-f", "ready.xml"}
				if mode == "dry-run" {
					args = append(args, "--dry-run")
				} else if mode == "allow-new" {
					args = append(args, "--new")
				}
				code := e.run(args...)
				if tc.wantError != "" {
					if code != 1 || !strings.Contains(e.errB.String(), tc.wantError) {
						t.Fatalf("exit %d: %s", code, e.errB)
					}
				} else if code != 0 {
					t.Fatalf("exit %d: %s", code, e.errB)
				}
				if edits := s.Edits(); len(edits) != tc.wantEdits || (len(edits) == 1 && edits[0] != tc.plugins[0].ID) {
					t.Fatalf("edit requests = %v", edits)
				}
				ups := s.Uploads()
				if tc.wantUpload && mode != "dry-run" {
					if len(ups) != 1 || ups[0].ID != "7" {
						t.Fatalf("uploads = %+v", ups)
					}
				} else if len(ups) != 0 {
					t.Fatalf("unexpected upload: %+v", ups)
				}
			})
		}
	}
}

func TestPlainVersionUpdate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested string
		plugins   []dleadmin.Plugin
		versions  map[int]string
		plain     bool
		wantError string
		wantID    string
	}{
		{"semver", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo"}}, map[int]string{7: "0.0.1"}, true, "", "7"},
		{"hash", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo"}}, map[int]string{7: "abc123"}, true, "", "7"},
		{"prerelease", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo"}}, map[int]string{7: "1.2.0-beta+1"}, true, "", "7"},
		{"name ends with v", "Demo v.", []dleadmin.Plugin{{ID: 7, Name: "Demo v."}}, map[int]string{7: "1"}, true, "", "7"},
		{"empty version", "Demo v.", []dleadmin.Plugin{{ID: 7, Name: "Demo v."}}, map[int]string{7: ""}, true, "", "7"},
		{"plain zero", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo"}}, map[int]string{7: "0"}, true, "", "7"},
		{"span zero", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo"}}, map[int]string{7: "0"}, false, "", "7"},
		{"literal version in name", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo v.0.0.1"}}, map[int]string{7: ""}, true, `has name "Demo v.0.0.1"`, ""},
		{"literal zero in name", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo0"}}, map[int]string{7: ""}, true, `has name "Demo0"`, ""},
		{"ambiguous fallback", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo"}, {ID: 8, Name: "Demo v.1"}}, map[int]string{7: "1", 8: ""}, true, "multiple installed plugins", ""},
		{"mixed fallback", "Demo", []dleadmin.Plugin{{ID: 7, Name: "Demo"}, {ID: 8, Name: "Demo0"}}, map[int]string{7: "1", 8: ""}, true, "multiple installed plugins", ""},
		{"exact match preferred", "Demo", []dleadmin.Plugin{{ID: 8, Name: "Demo v.1"}, {ID: 7, Name: "Demo"}}, map[int]string{7: "", 8: ""}, true, "", "7"},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry-run=%v", tc.name, dryRun), func(t *testing.T) {
				s := dleadmintest.New(t, "admin", "secret", tc.plugins...)
				s.Versions, s.PlainVersion = tc.versions, tc.plain
				e := newEnv(t, "secret\n", false)
				writeXML(t, e.work, tc.requested)
				args := []string{"deploy", "upload", s.AdminURL(), "-u", "admin", "--password-stdin", "-f", "ready.xml"}
				if dryRun {
					args = append(args, "--dry-run")
				}
				code := e.run(args...)
				if tc.wantError != "" {
					if code != 1 || !strings.Contains(e.errB.String(), tc.wantError) {
						t.Fatalf("exit %d: %s", code, e.errB)
					}
				} else if code != 0 {
					t.Fatalf("exit %d: %s", code, e.errB)
				}
				edits := s.Edits()
				wantEdits := 1
				if strings.Contains(tc.wantError, "multiple") {
					wantEdits = 0
				}
				if len(edits) != wantEdits || (tc.wantID != "" && fmt.Sprint(edits[0]) != tc.wantID) {
					t.Fatalf("edit requests = %v", edits)
				}
				ups := s.Uploads()
				if tc.wantID != "" && !dryRun {
					if len(ups) != 1 || ups[0].ID != tc.wantID {
						t.Fatalf("uploads = %+v", ups)
					}
				} else if len(ups) != 0 {
					t.Fatalf("unexpected upload: %+v", ups)
				}
			})
		}
	}
}

func TestInteractive(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	e := newEnv(t, s.AdminURL()+"\nadmin\nsecret\ny\n", true)
	writeXML(t, e.work, "New Plugin")
	if code := e.run("deploy", "upload", "-f", "ready.xml"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	out := e.out.String()
	for _, s := range []string{"Site host or admin URL: ", "Login: ", "Password: ", `Plugin "New Plugin" is not installed`, `✔ Plugin "New Plugin" installed on`} {
		if !strings.Contains(out, s) {
			t.Errorf("out lacks %q: %q", s, out)
		}
	}
	if ups := s.Uploads(); len(ups) != 1 || ups[0].ID != "" {
		t.Fatalf("uploads = %+v", ups)
	}
}

func TestInteractiveDeclineNew(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	e := newEnv(t, "secret\nn\n", true)
	writeXML(t, e.work, "New Plugin")
	if code := e.run("deploy", "upload", s.AdminURL(), "-u", "admin", "-f", "ready.xml"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if len(s.Uploads()) != 0 {
		t.Fatal("uploaded")
	}
}

func TestNew(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret")
	e := newEnv(t, "secret", false)
	writeXML(t, e.work, "New Plugin")
	if code := e.run("deploy", "upload", s.AdminURL(), "-u", "admin", "--password-stdin", "-f", "ready.xml", "--new"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	if ups := s.Uploads(); len(ups) != 1 || ups[0].ID != "" {
		t.Fatalf("uploads = %+v", ups)
	}
}

func TestDryRun(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 9, Name: "Ready Plugin"})
	host := strings.TrimPrefix(s.URL, "http://")
	for _, tc := range []struct {
		name, plugin string
		args         []string
		code         int
		out, err     string
	}{
		{name: "update", plugin: "Ready Plugin", out: `Dry run: plugin "Ready Plugin" would be updated on ` + host + " (id 9)\n"},
		{name: "install", plugin: "New Plugin", args: []string{"--new"}, out: `Dry run: plugin "New Plugin" would be installed on ` + host + "\n"},
		{name: "missing", plugin: "New Plugin", code: 1, err: "use --new to install it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, "secret", true)
			writeXML(t, e.work, tc.plugin)
			args := append([]string{"deploy", "upload", s.AdminURL(), "-u", "admin", "--password-stdin", "-f", "ready.xml", "--dry-run"}, tc.args...)
			if code := e.run(args...); code != tc.code {
				t.Fatalf("exit %d: %s", code, e.errB)
			}
			if tc.out != "" && !strings.HasSuffix(e.out.String(), tc.out) {
				t.Errorf("out = %q", e.out.String())
			}
			if !strings.Contains(e.errB.String(), tc.err) {
				t.Errorf("err = %q", e.errB.String())
			}
		})
	}
	if len(s.Uploads()) != 0 {
		t.Fatal("dry run uploaded")
	}
}

func TestErrors(t *testing.T) {
	s := dleadmintest.New(t, "admin", "secret", dleadmin.Plugin{ID: 1, Name: "Ready Plugin"})
	for _, tc := range []struct {
		name, stdin string
		args        []string
		err         string
	}{
		{name: "no host", args: []string{"-u", "admin"}, err: "missing HOST argument"},
		{name: "no login", args: []string{s.AdminURL()}, err: "missing --login"},
		{name: "no password", args: []string{s.AdminURL(), "-u", "admin"}, err: "missing password: use --password-stdin or " + deploy.PasswordEnv},
		{name: "stdin needs host", args: []string{"-u", "admin", "--password-stdin"}, err: "--password-stdin requires HOST and --login"},
		{name: "empty stdin", args: []string{s.AdminURL(), "-u", "admin", "--password-stdin"}, err: "password from stdin is empty"},
		{name: "bad login", stdin: "nope", args: []string{s.AdminURL(), "-u", "admin", "--password-stdin"}, err: "login failed"},
		{name: "not installed", stdin: "secret", args: []string{s.AdminURL(), "-u", "admin", "--password-stdin", "-f", "other.xml"}, err: `plugin "Other Plugin" is not installed on`},
		{name: "admin path twice", stdin: "secret", args: []string{s.AdminURL(), "--admin-path", "x.php", "-u", "admin", "--password-stdin"}, err: "admin path is set both"},
		{name: "missing file", args: []string{"-f", "nope.xml"}, err: "plugin file:"},
		{name: "version with file", args: []string{"-f", "ready.xml", "--version", "1.0"}, err: "[file version] were all set"},
		{name: "outside plugin", args: []string{"-f", ""}, err: "dleplugin.yml was not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, tc.stdin, false)
			writeXML(t, e.work, "Ready Plugin")
			if err := os.WriteFile(filepath.Join(e.work, "other.xml"), []byte("<dleplugin><name>Other Plugin</name></dleplugin>"), 0o644); err != nil {
				t.Fatal(err)
			}
			args := tc.args
			if !strings.Contains(strings.Join(args, " "), "-f") {
				args = append(args, "-f", "ready.xml")
			}
			if code := e.run(append([]string{"deploy", "upload"}, args...)...); code != 1 {
				t.Fatalf("exit %d", code)
			}
			if !strings.Contains(e.errB.String(), tc.err) {
				t.Errorf("err = %q, want %q", e.errB.String(), tc.err)
			}
		})
	}
	if len(s.Uploads()) != 0 {
		t.Fatal("uploaded")
	}
}

func TestShortName(t *testing.T) {
	e := newEnv(t, "", false)
	writeXML(t, e.work, "QF")
	if code := e.run("deploy", "upload", "example.com", "-f", "ready.xml"); code != 1 || !strings.Contains(e.errB.String(), "too short") {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
}
