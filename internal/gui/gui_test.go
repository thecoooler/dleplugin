package gui

import (
	"context"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/zalando/go-keyring"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/deploysites"
	"github.com/thecoooler/dleplugin/internal/modules/buildcmd"
	"github.com/thecoooler/dleplugin/internal/modules/deploy"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

// fakeDesktop answers dialogs with the paths queued in it.
type fakeDesktop struct {
	mu     sync.Mutex
	dirs   []string
	files  []string
	opened []string
}

func (d *fakeDesktop) chooseDirectory(string) (string, error) { return d.pop(&d.dirs), nil }
func (d *fakeDesktop) chooseFile(string) (string, error)      { return d.pop(&d.files), nil }
func (d *fakeDesktop) wake()                                  {}

func (d *fakeDesktop) openPath(path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.opened = append(d.opened, path)
	return nil
}

func (d *fakeDesktop) pop(q *[]string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(*q) == 0 {
		return ""
	}
	s := (*q)[0]
	*q = (*q)[1:]
	return s
}

// fakeRun records commands and answers with code and output.
type fakeRun struct {
	mu     sync.Mutex
	calls  []call
	code   int
	output string
}

type call struct {
	dir   string
	args  []string
	stdin string
}

func (f *fakeRun) run(_ context.Context, dir string, args []string, stdin string, out io.Writer) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{dir, args, stdin})
	io.WriteString(out, f.output)
	return f.code
}

func (f *fakeRun) last(t *testing.T) call {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("no command ran")
	}
	return f.calls[len(f.calls)-1]
}

type env struct {
	t    *testing.T
	app  *App
	tt   *ui.Tester
	desk *fakeDesktop
	run  *fakeRun
	cfg  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config", "config.yml")
	desk, run := &fakeDesktop{}, &fakeRun{}
	a := newApp(run.run, desk, cfg, "test")
	return &env{t: t, app: a, tt: ui.NewTester(a.view, 1120, 1500), desk: desk, run: run, cfg: cfg}
}

// settle waits for background work and draws the frames it asks for.
func (e *env) settle() {
	e.app.wait()
	e.tt.Frame()
	e.app.wait()
	e.tt.Frame()
}

func (e *env) click(label string) {
	e.t.Helper()
	if err := e.tt.Click(label); err != nil {
		e.t.Fatalf("click %q: %v; texts %q", label, err, e.tt.Texts())
	}
	e.settle()
}

func (e *env) has(text string) {
	e.t.Helper()
	if !e.tt.HasText(text) {
		e.t.Fatalf("no %q in %q", text, e.tt.Texts())
	}
}

// snapshot writes the frame to $DLEPLUGIN_GUI_SNAPSHOTS/name.png, to look at.
func (e *env) snapshot(name string) {
	dir := os.Getenv("DLEPLUGIN_GUI_SNAPSHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, e.tt.Image()); err != nil {
		e.t.Fatal(err)
	}
}

func writeProject(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "slug: demo_plugin\nname: Demo Plugin\ndescription: A plugin for tests\nversion: 1.2.0\n"
	if err := os.WriteFile(filepath.Join(dir, "dleplugin.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAddProjectAndBuild(t *testing.T) {
	e := newEnv(t)
	e.has("No project selected")
	e.snapshot("empty")
	root := writeProject(t, filepath.Join(t.TempDir(), "demo"))
	e.desk.dirs = []string{filepath.Join(root, "src")} // a subdirectory finds the root
	e.click("Add project…")
	e.has("Demo Plugin")
	e.has("demo_plugin")
	e.snapshot("overview")

	s, err := LoadSettings(e.app.settingsPath)
	if err != nil || len(s.Projects) != 1 || s.Projects[0].Path != root {
		t.Fatalf("settings %+v, %v", s, err)
	}

	e.click("Build")
	e.app.projects[root].build.version = "2.0"
	e.tt.Frame()
	e.snapshot("build")
	e.click("Build plugin")
	if c := e.run.last(t); !slices.Equal(c.args, []string{"build", "--version", "2.0"}) || c.dir != root {
		t.Fatalf("build call %+v", c)
	}
}

func TestProjectPersistsAndRemoves(t *testing.T) {
	e := newEnv(t)
	root := writeProject(t, filepath.Join(t.TempDir(), "demo"))
	e.app.selectProject(root)
	e.settle()

	again := newApp(e.run.run, e.desk, e.cfg, "test")
	if again.selected != root || again.projects[root] == nil || again.projects[root].info.Slug != "demo_plugin" {
		t.Fatalf("reloaded app selected %q", again.selected)
	}

	e.click("Remove from list…")
	e.has("Remove “Demo Plugin”?")
	if err := e.tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	e.settle()
	if len(e.app.settings.Projects) != 0 || e.app.selected != "" {
		t.Fatalf("projects %+v, selected %q", e.app.settings.Projects, e.app.selected)
	}
	if !exists(root + "/dleplugin.yml") {
		t.Fatal("remove deleted the project")
	}
}

func TestNewProjectDialog(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(t.TempDir(), "fresh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	e.desk.dirs = []string{dir}
	e.click("Add project…")
	e.has("No dleplugin.yml")
	e.click("Create project…")
	e.has("New plugin project")
	if e.app.initForm.slug != "fresh" {
		t.Fatalf("slug %q", e.app.initForm.slug)
	}
	e.app.initForm.name = "Fresh Plugin"
	e.tt.Frame()
	e.snapshot("new-project")
	e.click("Create")
	if !exists(filepath.Join(dir, "dleplugin.yml")) {
		t.Fatalf("no dleplugin.yml; error %q", e.app.initErr)
	}
	if e.app.selected != dir {
		t.Fatalf("selected %q", e.app.selected)
	}
	e.has("Fresh Plugin")
}

func TestSitesAndDeploy(t *testing.T) {
	e := newEnv(t)
	root := writeProject(t, filepath.Join(t.TempDir(), "demo"))
	global, project := registryPaths(e.cfg, root)
	if err := deploysites.Save(global, &deploysites.Document{Sites: map[string]deploysites.Site{
		"shared.example": {Login: "admin"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := deploysites.Save(project, &deploysites.Document{Sites: map[string]deploysites.Site{
		"dev.localhost": {URL: "http://dev.localhost", Login: "dev"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := deploysites.SavePassword("dev.localhost", "dev", "secret"); err != nil {
		t.Fatal(err)
	}
	e.app.selectProject(root)
	e.settle()
	pv := e.app.projects[root]
	pv.tab = tabSites
	e.settle()
	e.has("dev.localhost")
	e.has("shared.example")
	e.has("saved")
	e.snapshot("sites")

	// Saved password: the upload passes no password.
	pv.tab = tabDeploy
	e.settle()
	if pv.deploy.site != "dev.localhost" {
		t.Fatalf("site %q", pv.deploy.site)
	}
	pv.deploy.allowNew = true
	e.tt.Frame()
	e.snapshot("deploy")
	e.click("Upload")
	c := e.run.last(t)
	if !slices.Equal(c.args, []string{"deploy", "upload", "dev.localhost", "--new"}) || c.stdin != "" {
		t.Fatalf("upload %+v", c)
	}

	// Without a saved password the button waits for one, which goes to
	// stdin and, when asked, to the credential store.
	pv.deploy.site = "shared.example"
	e.tt.Frame()
	e.has("Enter the password: none is saved for admin@shared.example.")
	pv.deploy.password, pv.deploy.remember = "pw", true
	e.tt.Frame()
	e.click("Upload")
	c = e.run.last(t)
	if !slices.Equal(c.args, []string{"deploy", "upload", "shared.example", "--password-stdin", "--new"}) || c.stdin != "pw" {
		t.Fatalf("upload %+v", c)
	}
	e.settle()
	if p, ok, _ := deploysites.Password("shared.example", "admin"); !ok || p != "pw" {
		t.Fatalf("password not remembered: %q %v", p, ok)
	}
	if pv.deploy.password != "" {
		t.Fatal("the password stays in the form")
	}
	if ps := e.app.settings.Project(root); ps.DeploySite != "" && ps.DeploySite != "shared.example" {
		t.Fatalf("deploy site %q", ps.DeploySite)
	}
}

func TestSiteDialogShowsCommandError(t *testing.T) {
	e := newEnv(t)
	root := writeProject(t, filepath.Join(t.TempDir(), "demo"))
	e.app.selectProject(root)
	e.app.projects[root].tab = tabSites
	e.settle()
	e.click("Add site…")
	e.has("Add site")
	f := &e.app.siteForm
	f.target, f.login, f.password = "example.com", "admin", "pw"
	e.run.code, e.run.output = 1, "✖ Error: login failed: wrong password\n"
	e.tt.Frame()
	e.snapshot("site-dialog")
	e.click("Check and save")
	c := e.run.last(t)
	if !slices.Equal(c.args, []string{"deploy", "sites", "add", "example.com", "--login", "admin", "--password-stdin"}) || c.stdin != "pw" || c.dir != root {
		t.Fatalf("add %+v", c)
	}
	if !e.app.siteOpen || e.app.siteErr != "login failed: wrong password" {
		t.Fatalf("open %v, error %q", e.app.siteOpen, e.app.siteErr)
	}
	e.has("login failed: wrong password")
	e.has("✖ Error: login failed: wrong password")

	e.run.code, e.run.output = 0, "✔ Site example.com saved\n"
	e.click("Check and save")
	if e.app.siteOpen {
		t.Fatal("the dialog stays open after saving")
	}
}

func TestChecksTab(t *testing.T) {
	e := newEnv(t)
	root := writeProject(t, filepath.Join(t.TempDir(), "demo"))
	e.app.selectProject(root)
	e.app.projects[root].tab = tabChecks
	e.settle()
	dle := t.TempDir()
	e.desk.dirs = []string{dle}
	e.click("Add DLE directory…")
	e.has(dle)
	e.snapshot("checks")
	e.click("Run check")
	if c := e.run.last(t); !slices.Equal(c.args, []string{"check", dle}) {
		t.Fatalf("check %+v", c)
	}
	e.app.projects[root].static.phpVersion = "8.1"
	e.tt.Frame()
	e.click("Run analysis")
	if c := e.run.last(t); !slices.Equal(c.args, []string{"static", "--php-version", "8.1"}) {
		t.Fatalf("static %+v", c)
	}
	e.app.settings.Verbose = true
	e.app.projects[root].test.filter = "Smoke"
	e.tt.Frame()
	e.click("Run tests")
	if c := e.run.last(t); !slices.Equal(c.args, []string{"test", "--filter", "Smoke", "--verbose"}) {
		t.Fatalf("test %+v", c)
	}
}

func TestConsole(t *testing.T) {
	e := newEnv(t)
	e.app.selected = pageSettings
	e.run.output = "\x1b[32m✔ done\x1b[0m\r\nprogress 10%\rprogress 100%\npartial"
	e.app.start("Build", t.TempDir(), []string{"build"}, "", nil)
	e.settle()
	want := []string{"$ dleplugin build", "✔ done", "progress 100%", "partial"}
	if !slices.Equal(e.app.console.lines, want) {
		t.Fatalf("console %q", e.app.console.lines)
	}
	e.has("Build succeeded in 0s")
	e.snapshot("console")
	e.click("Copy")
	if e.tt.Clipboard() != strings.Join(want, "\n") {
		t.Fatalf("clipboard %q", e.tt.Clipboard())
	}
	e.click("Clear")
	if len(e.app.console.lines) != 0 {
		t.Fatal("console not cleared")
	}
}

// The real runner runs the CLI's commands in process.
func TestRunnerRunsCLI(t *testing.T) {
	root := writeProject(t, filepath.Join(t.TempDir(), "demo"))
	r := &Runner{ConfigPath: filepath.Join(t.TempDir(), "config.yml"), Modules: []app.Module{buildcmd.Module(), deploy.Module()}}
	var lines []string
	w := &lineWriter{emit: func(l []string) { lines = append(lines, l...) }}
	if code := r.Run(context.Background(), root, []string{"build", "--version", "3.0"}, "", w); code != 0 {
		t.Fatalf("build exit %d: %q", code, lines)
	}
	w.Flush()
	if !slices.Contains(lines, "✔ Plugin built successfully") {
		t.Fatalf("output %q", lines)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "demo_plugin.xml"))
	if err != nil || !strings.Contains(string(data), "<version>3.0</version>") {
		t.Fatalf("xml %v: %s", err, data)
	}

	lines = nil
	if code := r.Run(context.Background(), root, []string{"deploy", "sites", "list"}, "", w); code != 0 {
		t.Fatalf("sites list exit %d: %q", code, lines)
	}
	lines = nil
	code := r.Run(context.Background(), root, []string{"deploy", "upload", "example.com"}, "", w)
	w.Flush()
	if code != 1 || errorLine(lines) != "missing --login" {
		t.Fatalf("upload without login: %d %q", code, lines)
	}
}

func TestFormsArgs(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
		want  []string
	}{
		{"build", (&buildForm{version: " 1.0 ", upgradeURL: "https://x/u"}).args(), "", []string{"build", "--version", "1.0", "--upgrade-url", "https://x/u"}},
		{"custom upload", func() []string {
			a, _ := (&deployForm{site: customSite, target: "https://x/panel.php", login: "u", source: 1, file: "/f.zip", version: "ignored", dryRun: true, insecure: true}).args()
			return a
		}(), "", []string{"deploy", "upload", "https://x/panel.php", "--login", "u", "--file", "/f.zip", "--dry-run", "--insecure"}},
		{"edit site", func() []string {
			a, _ := (&siteForm{edit: true, global: true, target: "x.com", login: "u"}).args()
			return a
		}(), "", []string{"deploy", "sites", "edit", "x.com", "--login", "u", "--admin-path", "", "--password-env", "", "--global"}},
		{"remove", removeSiteArgs("x.com", false), "", []string{"deploy", "sites", "remove", "x.com"}},
		{"test", (&testForm{updateSnapshots: true, coverage: true, refresh: true}).args(), "", []string{"test", "--update-snapshots", "--coverage", "--refresh-dle"}},
	}
	for _, c := range cases {
		if !slices.Equal(c.args, c.want) {
			t.Errorf("%s: %q, want %q", c.name, c.args, c.want)
		}
	}
	if got := commandLine([]string{"deploy", "upload", "a b", ""}); got != "dleplugin deploy upload 'a b' ''" {
		t.Errorf("commandLine %q", got)
	}
	if err := (&siteForm{target: "x", login: "u"}).validate(); err == nil {
		t.Error("add without a password validates")
	}
	if err := (&siteForm{edit: true, target: "x", login: "u"}).validate(); err != nil {
		t.Errorf("edit without a password: %v", err)
	}
}

func TestErrorLine(t *testing.T) {
	lines := []string{"✔ Plugin built", "✖ Error: Fill required configuration values in /c.yml:", "- host_dir", "  caused by [*errors.errorString]: x"}
	if got := errorLine(lines); got != "Fill required configuration values in /c.yml:\n- host_dir" {
		t.Errorf("errorLine %q", got)
	}
	if got := errorLine([]string{"✖ Plugin tests failed", ""}); got != "Plugin tests failed" {
		t.Errorf("errorLine %q", got)
	}
}

func TestSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gui.yaml")
	s, err := LoadSettings(path)
	if err != nil || len(s.Projects) != 0 {
		t.Fatalf("missing file: %+v %v", s, err)
	}
	if err := os.WriteFile(path, []byte("projects:\n- path: /a\n- path: /a/\n- path: ''\n- path: /b\n  check_dirs: [/dle]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err = LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(s.Projects); got != "[{/a [] } {/b [/dle] }]" {
		t.Fatalf("projects %s", got)
	}
	if !s.AddProject("/c") || s.AddProject("/b/") {
		t.Fatal("AddProject")
	}
	s.RemoveProject("/a")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	s, _ = LoadSettings(path)
	if got := fmt.Sprint(s.Projects); got != "[{/b [/dle] } {/c [] }]" {
		t.Fatalf("saved projects %s", got)
	}
	if err := os.WriteFile(path, []byte("projects: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(path); err == nil {
		t.Fatal("invalid file loads")
	}
}

func TestDeployTargetClearsPassword(t *testing.T) {
	e := newEnv(t)
	root := writeProject(t, filepath.Join(t.TempDir(), "demo"))
	global, _ := registryPaths(e.cfg, root)
	if err := deploysites.Save(global, &deploysites.Document{Sites: map[string]deploysites.Site{
		"first.example": {Login: "admin"}, "second.example": {Login: "admin"},
	}}); err != nil {
		t.Fatal(err)
	}
	e.app.selectProject(root)
	e.settle()
	pv := e.app.projects[root]
	pv.tab = tabDeploy
	e.settle()
	for _, target := range []string{"second.example", customSite} {
		pv.deploy.password, pv.deploy.remember = "secret", true
		e.tt.Frame()
		e.click(pv.deploy.site)
		e.click(target)
		if pv.deploy.password != "" || pv.deploy.remember {
			t.Fatalf("credentials retained for %s", target)
		}
	}
	for _, field := range []string{"Host or admin URL", "Login"} {
		pv.deploy.password, pv.deploy.remember = "secret", true
		e.tt.Frame()
		e.click(field)
		e.tt.Type("changed")
		if pv.deploy.password != "" || pv.deploy.remember {
			t.Fatalf("credentials retained after editing %s", field)
		}
	}
	// A removed selection must not transfer credentials to the default site.
	pv.deploy.site, pv.deploy.password, pv.deploy.remember = "removed.example", "secret", true
	e.tt.Frame()
	if pv.deploy.password != "" || pv.deploy.remember {
		t.Fatal("credentials retained after removing site")
	}
}

func TestEditSitePreservesResolvedAdminPath(t *testing.T) {
	for _, site := range []deploysites.Site{
		{URL: "https://example.com/dle/panel.php", Login: "admin"},
		{URL: "https://example.com", AdminPath: "nested/admin.php", Login: "admin"},
		{Login: "admin"},
	} {
		t.Run(site.URL+site.AdminPath, func(t *testing.T) {
			e := newEnv(t)
			global, _ := registryPaths(e.cfg, "")
			if err := deploysites.Save(global, &deploysites.Document{Sites: map[string]deploysites.Site{"example.com": site}}); err != nil {
				t.Fatal(err)
			}
			e.app.selected = pageSites
			e.settle()
			e.app.globalSites.selected = 0
			e.tt.Frame()
			e.click("Edit…")
			admin, err := site.AdminURL("example.com")
			if err != nil {
				t.Fatal(err)
			}
			want := strings.TrimPrefix(admin.Path, "/")
			if e.app.siteForm.adminPath != want {
				t.Fatalf("admin path %q, want %q", e.app.siteForm.adminPath, want)
			}
			e.click("Check and save")
			args := e.run.last(t).args
			i := slices.Index(args, "--admin-path")
			if i < 0 || args[i+1] != want {
				t.Fatalf("edit args %q", args)
			}
		})
	}
}

func TestSiteDialogCanCancelLoginCheck(t *testing.T) {
	for _, escape := range []bool{false, true} {
		t.Run(fmt.Sprint("escape=", escape), func(t *testing.T) {
			e := newEnv(t)
			e.app.selected = pageSites
			e.settle()
			e.click("Add site…")
			e.app.siteForm.target, e.app.siteForm.login, e.app.siteForm.password = "example.com", "admin", "pw"
			canceled := make(chan struct{})
			release := make(chan struct{})
			t.Cleanup(func() { e.app.cancelJob(); e.app.wait() })
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			e.app.run = func(ctx context.Context, _ string, _ []string, _ string, _ io.Writer) int {
				<-ctx.Done()
				close(canceled)
				<-release
				return 1
			}
			e.tt.Frame()
			if err := e.tt.Click("Check and save"); err != nil {
				t.Fatal(err)
			}
			if escape {
				e.tt.Key(0, ui.KeyEscape)
			} else if err := e.tt.Click("Cancel"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("dialog did not cancel login check")
			}
			if e.app.siteOpen {
				t.Fatal("canceled dialog remains open")
			}
			unblock()
			e.settle()
			if e.app.siteOpen {
				t.Fatal("completion reopened the canceled dialog")
			}
		})
	}
}

func TestShutdownWaitsForCleanup(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint("timeout=", timeout), func(t *testing.T) {
			e := newEnv(t)
			canceled, release := make(chan struct{}), make(chan struct{})
			e.app.run = func(ctx context.Context, _ string, _ []string, _ string, _ io.Writer) int {
				<-ctx.Done()
				close(canceled)
				<-release
				return 1
			}
			e.app.start("Tests", "", []string{"test"}, "", nil)
			result := make(chan bool, 1)
			bound := time.Second
			if timeout {
				bound = time.Millisecond
			}
			go func() { result <- e.app.shutdown(bound) }()
			<-canceled
			if timeout {
				if <-result {
					t.Fatal("shutdown finished before cleanup")
				}
			} else {
				select {
				case <-result:
					t.Fatal("shutdown did not wait for cleanup")
				case <-time.After(10 * time.Millisecond):
				}
			}
			close(release)
			if !timeout && !<-result {
				t.Fatal("shutdown timed out after cleanup")
			}
			e.app.wait()
			if e.app.busy() {
				t.Fatal("job remains busy")
			}
		})
	}
}

func TestCommandFailureAndCancellationMessages(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint("canceled=", canceled), func(t *testing.T) {
			e := newEnv(t)
			release := make(chan struct{})
			e.app.run = func(context.Context, string, []string, string, io.Writer) int { <-release; return 7 }
			e.app.start("Build", "", []string{"build"}, "", nil)
			want := "✖ Build failed (exit code 7)"
			if canceled {
				e.app.cancelJob()
				want = "⚠ Build canceled"
			}
			close(release)
			e.settle()
			if got := e.app.console.lines[len(e.app.console.lines)-1]; got != want {
				t.Fatalf("message %q, want %q", got, want)
			}
		})
	}
}

func TestSiteChangesRefreshAllViews(t *testing.T) {
	for _, change := range []string{"save", "remove", "password"} {
		t.Run(change, func(t *testing.T) {
			e := newEnv(t)
			host := change + ".example"
			global, _ := registryPaths(e.cfg, "")
			doc := &deploysites.Document{Sites: map[string]deploysites.Site{host: {Login: "admin"}}}
			if err := deploysites.Save(global, doc); err != nil {
				t.Fatal(err)
			}
			for i := range 2 {
				root := writeProject(t, filepath.Join(t.TempDir(), fmt.Sprint(i)))
				e.app.selectProject(root)
				e.settle()
			}
			e.app.selected = pageSites
			e.settle()
			e.app.run = func(_ context.Context, _ string, _ []string, _ string, _ io.Writer) int {
				if change == "remove" {
					delete(doc.Sites, host)
				} else {
					doc.Sites[host] = deploysites.Site{Login: "updated"}
				}
				if err := deploysites.Save(global, doc); err != nil {
					return 1
				}
				return 0
			}
			switch change {
			case "save":
				e.app.openSite(siteForm{edit: true, global: true, target: host, login: "updated"})
				e.app.saveSite()
			case "remove":
				e.app.removeSite(e.app.globalSites.rows[0])
			case "password":
				e.app.savePassword(host, "admin", "secret")
			}
			e.settle()
			views := []*sitesView{&e.app.globalSites}
			for _, pv := range e.app.projects {
				views = append(views, &pv.sites)
			}
			for _, sv := range views {
				row, found := findSite(sv.rows, host)
				switch change {
				case "remove":
					if found {
						t.Fatalf("removed site remains in %+v", sv.rows)
					}
				case "save":
					if !found || row.site.Login != "updated" {
						t.Fatalf("stale sites %+v", sv.rows)
					}
				case "password":
					if !found || row.password != passwordStored {
						t.Fatalf("stale credentials %+v", sv.rows)
					}
				}
			}
		})
	}
}

func TestRefreshSitesDuringLoad(t *testing.T) {
	e := newEnv(t)
	global, _ := registryPaths(e.cfg, "")
	e.app.loadSites(&e.app.globalSites, "")
	e.app.bg.Wait() // leave the old result queued, as if a mutation completed during a read
	if err := deploysites.Save(global, &deploysites.Document{Sites: map[string]deploysites.Site{"new.example": {Login: "admin"}}}); err != nil {
		t.Fatal(err)
	}
	e.app.refreshSites()
	e.settle()
	if _, found := findSite(e.app.globalSites.rows, "new.example"); !found {
		t.Fatal("in-flight read hid the new site")
	}
}
