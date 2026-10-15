package dletest

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/thecoooler/dleplugin/internal/config"
	"github.com/thecoooler/dleplugin/internal/plugin"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, p string, files map[string]string) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUnpack(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "trial.zip")
	writeZip(t, archive, map[string]string{
		"readme.txt":                   "readme",
		"Documentation/index.html":     "docs",
		"upload/index.php":             "<?php // index",
		"upload/engine/data/.htaccess": "deny",
	})
	site := filepath.Join(dir, "site")
	if err := Unpack(archive, site); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{"index.php": "<?php // index", "engine/data/.htaccess": "deny"} {
		if b, err := os.ReadFile(filepath.Join(site, p)); err != nil || string(b) != want {
			t.Errorf("%s = %q, %v", p, b, err)
		}
	}
	for _, p := range []string{"readme.txt", "Documentation", "upload"} {
		if _, err := os.Stat(filepath.Join(site, p)); err == nil {
			t.Errorf("%s extracted", p)
		}
	}

	writeZip(t, archive, map[string]string{"upload/../../evil.php": "x", "upload/a/../../evil2.php": "x", "upload/ok.php": "x"})
	if err := Unpack(archive, filepath.Join(dir, "slip", "site")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"evil.php", "evil2.php", "slip/evil.php", "slip/evil2.php", "slip/site/evil2.php"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("zip slip: %s written", p)
		}
	}
	writeZip(t, archive, map[string]string{"other/index.php": "x"})
	if err := Unpack(archive, filepath.Join(dir, "other")); err == nil || !strings.Contains(err.Error(), "not a DLE archive") {
		t.Errorf("not DLE: %v", err)
	}
}

func TestDownloadAndHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dle.zip" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, "archive")
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "cache", "dle.zip")
	if err := Download(context.Background(), srv.Client(), srv.URL+"/dle.zip", dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "archive" {
		t.Errorf("content = %q", b)
	}
	want := sha256.Sum256([]byte("archive"))
	if sum, err := FileHash(dest); err != nil || sum != hex.EncodeToString(want[:]) {
		t.Errorf("hash = %q, %v", sum, err)
	}

	err := Download(context.Background(), srv.Client(), srv.URL+"/missing.zip", filepath.Join(t.TempDir(), "x.zip"))
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("err = %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(dest)); len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
}

func TestSiteURLAndVersion(t *testing.T) {
	site := t.TempDir()
	conf := filepath.Join(site, "engine", "data", "config.php")
	write(t, conf, "<?php\n$config = array (\n'version_id' => '21.0',\n'http_home_url' => 'http://127.0.0.1:54321/',\n'skin' => 'Air',\n);\n")
	if v := Version(site); v != "21.0" {
		t.Errorf("version = %q", v)
	}
	if err := SetSiteURL(site, SiteURL); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(conf)
	if !strings.Contains(string(b), "'http_home_url' => 'http://localhost/',") || !strings.Contains(string(b), "'skin' => 'Air'") {
		t.Errorf("config = %s", b)
	}
	if v := Version(t.TempDir()); v != "" {
		t.Errorf("missing config version = %q", v)
	}
	if err := SetSiteURL(t.TempDir(), SiteURL); err == nil {
		t.Error("missing config: no error")
	}
}

// fakeInstaller emulates the DLE installer steps; requests out of order or
// without the session cookie fail like the real one.
type fakeInstaller struct {
	mu      sync.Mutex
	steps   []string
	checkdb url.Values
	failDB  string
}

func (f *fakeInstaller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	action := r.URL.Query().Get("action")
	if action == "" {
		action = r.PostForm.Get("action")
	}
	if action == "" {
		action = "start"
	}
	f.steps = append(f.steps, action)
	if action == "start" {
		http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "s1"})
		fmt.Fprint(w, `<form><input name="action" value="eula"></form>`)
		return
	}
	if c, err := r.Cookie("PHPSESSID"); err != nil || c.Value != "s1" {
		fmt.Fprint(w, "<p>Установка скрипта была начата не с начала.</p>")
		return
	}
	batch := func() {
		off, _ := strconv.Atoi(r.PostForm.Get("offset"))
		off += 4
		if off > 10 {
			off = 10
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "offset": off, "total": 10})
	}
	switch action {
	case "eula", "userconfig":
		fmt.Fprint(w, "<p>ok</p>")
	case "function_check":
		http.Redirect(w, r, "/install.php?action=dbconfig", http.StatusFound)
	case "dbconfig":
		fmt.Fprint(w, `<input name="dbhost">`)
	case "checkdb":
		f.checkdb = r.PostForm
		if f.failDB != "" {
			fmt.Fprintf(w, `{"status":"error","error":%q}`, f.failDB)
			return
		}
		fmt.Fprint(w, `{"status":"ok"}`)
	case "doinstall":
		fmt.Fprint(w, `<script>$.post("?action=installtemplate")</script>`)
	case "installtemplate", "docreatedb":
		batch()
	case "dosaveconfig":
		fmt.Fprint(w, `{"status":"ok"}`)
	default:
		http.NotFound(w, r)
	}
}

func TestInstall(t *testing.T) {
	f := &fakeInstaller{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	if err := Install(context.Background(), srv.URL+"/install.php"); err != nil {
		t.Fatal(err)
	}
	want := []string{"start", "eula", "function_check", "dbconfig", "checkdb", "userconfig", "doinstall",
		"installtemplate", "installtemplate", "installtemplate", "docreatedb", "docreatedb", "docreatedb", "dosaveconfig"}
	if !reflect.DeepEqual(f.steps, want) {
		t.Errorf("steps = %v", f.steps)
	}
	if f.checkdb.Get("dbhost") != "db" || f.checkdb.Get("dbuser") != "dle" || f.checkdb.Get("dbprefix") != "dle" {
		t.Errorf("checkdb = %v", f.checkdb)
	}

	f = &fakeInstaller{failDB: "Нет доступа к базе"}
	srv2 := httptest.NewServer(f)
	defer srv2.Close()
	if err := Install(context.Background(), srv2.URL+"/install.php"); err == nil || !strings.Contains(err.Error(), "?action=checkdb: Нет доступа к базе") {
		t.Errorf("err = %v", err)
	}
}

func TestPageText(t *testing.T) {
	got := pageText("<html><style>p{}</style><script>var x;</script><h1>Ошибка</h1>\n<p>a &amp; b</p></html>")
	if got != "Ошибка a & b" {
		t.Errorf("got %q", got)
	}
}

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yml")
	write(t, p, "host_dir: x\ntest:\n  trial_url: http://x/dle.zip\n  version: 18.0\ndependency_roots: [/r1, /r2]\n")
	c, err := LoadConfig(config.NewStore(p))
	if err != nil {
		t.Fatal(err)
	}
	if c.TrialURL != "http://x/dle.zip" || !reflect.DeepEqual(c.DependencyRoots, []string{"/r1", "/r2"}) {
		t.Errorf("config = %+v", c)
	}
	write(t, p, "test:\n  dependency_roots: [/own]\ndependency_roots: [/r1]\n")
	if c, err := LoadConfig(config.NewStore(p)); err != nil || !reflect.DeepEqual(c.DependencyRoots, []string{"/own"}) {
		t.Errorf("own roots = %+v, %v", c, err)
	}
	c, err = LoadConfig(config.NewStore(filepath.Join(dir, "missing.yml")))
	if err != nil || c.TrialURL != "" || c.DependencyRoots != nil {
		t.Errorf("missing config = %+v, %v", c, err)
	}
	write(t, p, "test: [1]\n")
	if _, err := LoadConfig(config.NewStore(p)); err == nil || !strings.Contains(err.Error(), "test must be a mapping") {
		t.Errorf("err = %v", err)
	}
}

func manifest(t *testing.T, dir, slug, name string, deps ...string) {
	t.Helper()
	y := "slug: " + slug + "\nname: " + name + "\n"
	if len(deps) > 0 {
		y += "dependencies: [" + strings.Join(deps, ", ") + "]\n"
	}
	write(t, filepath.Join(dir, "dleplugin.yml"), y)
}

func slugs(ps []*plugin.Plugin) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Slug())
	}
	return out
}

func TestDependencies(t *testing.T) {
	dir := t.TempDir()
	modules, clients := filepath.Join(dir, "modules"), filepath.Join(dir, "clients")
	roots := []string{modules, clients}
	manifest(t, filepath.Join(modules, "ExampleDependency"), "ExampleDependency", "ExampleDependency")
	manifest(t, filepath.Join(modules, "kdb-dir"), "KDB", "KDB Base", "ExampleDependency")
	manifest(t, filepath.Join(clients, "Series"), "Series", "Series", "KDB Base", "ExampleDependency")
	manifest(t, filepath.Join(clients, "Fixture"), "Fixture", "Fixture", "ExampleDependency")
	manifest(t, filepath.Join(clients, "Plugin"), "Plugin", "Plugin", "Series")
	write(t, filepath.Join(clients, "Plugin", ConfFile), "depends:\n  - ../Fixture\n  - ExampleDependency\n")
	write(t, filepath.Join(modules, "broken", "dleplugin.yml"), "slug: [\n")

	p, err := plugin.Load(filepath.Join(clients, "Plugin"))
	if err != nil {
		t.Fatal(err)
	}
	conf, err := LoadPluginConf(p.Root())
	if err != nil {
		t.Fatal(err)
	}
	// Each plugin after its dependencies, every plugin once; KDB found by
	// name in a differently named directory.
	deps, err := Dependencies(p, conf, roots)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ExampleDependency", "Fixture", "KDB", "Series"}; !reflect.DeepEqual(slugs(deps), want) {
		t.Errorf("deps = %v, want %v", slugs(deps), want)
	}

	deps, err = Dependencies(p, &PluginConf{}, []string{})
	if err == nil || !strings.Contains(err.Error(), "Dependency not found: Series") || !strings.Contains(err.Error(), "(required by Plugin)") {
		t.Errorf("deps = %v, err = %v", slugs(deps), err)
	}

	manifest(t, filepath.Join(modules, "ExampleDependency"), "ExampleDependency", "ExampleDependency", "Plugin")
	_, err = Dependencies(p, &PluginConf{}, roots)
	if err == nil || !strings.Contains(err.Error(), "Dependency cycle: Plugin -> Series -> KDB Base -> ExampleDependency -> Plugin") {
		t.Errorf("err = %v", err)
	}

	if c, err := LoadPluginConf(t.TempDir()); err != nil || len(c.Depends) != 0 {
		t.Errorf("missing dletest.yml = %+v, %v", c, err)
	}
}

func loadPlugin(t *testing.T, dir, slug string) *plugin.Plugin {
	t.Helper()
	write(t, filepath.Join(dir, "dleplugin.yml"), "slug: "+slug+"\nname: "+slug+"\n")
	write(t, filepath.Join(dir, "src", "index.php"), "<?php\n")
	p, err := plugin.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPestArgs(t *testing.T) {
	min := 80.5
	p := loadPlugin(t, filepath.Join(t.TempDir(), "plugin with spaces"), "demo")
	got := pestArgs(Options{Plugin: p, Filter: "it works", Coverage: true, CoverageMin: &min, CoverageHTML: "/out", UpdateSnapshots: true, Files: []string{"tests/A.php"}})
	want := []string{"php", "-d", "pcov.enabled=1", "-d", "pcov.directory=" + filepath.Join(p.Root(), "src"), pestBin, "--display-incomplete", "--testdox", "--colors=always", "--profile",
		"--filter", "it works", "--coverage", "--min=80.5", "--coverage-html=/out", "-c", "phpunit.xml", "--update-snapshots", "tests/A.php"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %q", got)
	}
	got = pestArgs(Options{})
	want = []string{"php", pestBin, "--display-incomplete", "--testdox", "--colors=always", "--profile", "-c", "phpunit.xml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %q", got)
	}
}

func TestMounts(t *testing.T) {
	dir := t.TempDir()
	site := filepath.Join(dir, "site")
	for _, p := range []string{"engine/x.php", "index.php", "bootstrap.php"} {
		write(t, filepath.Join(site, p), "")
	}
	p := loadPlugin(t, filepath.Join(dir, "work", "Plugin"), "Plugin")
	inside := loadPlugin(t, filepath.Join(dir, "work", "Plugin", "vendor", "Lib"), "Lib")
	dep := loadPlugin(t, filepath.Join(dir, "modules", "ExampleDependency"), "ExampleDependency")
	got, err := mounts(site, Options{Plugin: p, Dependencies: []*plugin.Plugin{dep, inside}, CoverageHTML: filepath.Join(dir, "cov")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		site + "/bootstrap.php:/dle/bootstrap.php",
		site + "/engine:/dle/engine",
		site + "/index.php:/dle/index.php",
		p.Root() + "/tests:/dle/tests",
		filepath.Join(dir, "cov") + ":" + filepath.Join(dir, "cov"),
		p.Root() + ":" + p.Root(),
		dep.Root() + ":" + dep.Root() + ":ro",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mounts =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestWriteRunFilesAndCopyTree(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	write(t, filepath.Join(base, "engine", "data", "config.php"), "<?php")
	if err := os.Chmod(filepath.Join(base, "engine", "data", "config.php"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("config.php", filepath.Join(base, "engine", "data", "link.php")); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(dir, "site")
	if err := copyTree(base, site); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(site, "engine", "data", "config.php")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("copied file: %v %v", fi, err)
	}
	if l, err := os.Readlink(filepath.Join(site, "engine", "data", "link.php")); err != nil || l != "config.php" {
		t.Errorf("copied link = %q, %v", l, err)
	}

	p := loadPlugin(t, filepath.Join(dir, "A&B"), "AB")
	if err := writeRunFiles(site, p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(site, "phpunit.xml"))
	if !strings.Contains(string(b), "<directory>"+filepath.Join(dir, "A&amp;B", "src")+"</directory>") {
		t.Errorf("phpunit.xml = %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(site, "bootstrap.php")); !strings.Contains(string(b), `function xfieldsload(`) {
		t.Errorf("bootstrap.php lacks the xfields shim")
	}
}
