package dletest

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	manifestpkg "github.com/thecoooler/dleplugin/internal/manifest"
	"github.com/thecoooler/dleplugin/internal/modules/test/internal/dle"
	"github.com/thecoooler/dleplugin/internal/paths"
	"github.com/thecoooler/dleplugin/internal/plugin"
	"github.com/thecoooler/dleplugin/internal/process"
)

// Each invocation records one tab-separated argv line, preserving spaces in paths.
const orchestrationDocker = `#!/bin/sh
command="$1"
printf '%s' "$1" >> "$DOCKER_LOG"
shift
for arg do printf '\t%s' "$arg" >> "$DOCKER_LOG"; done
printf '\n' >> "$DOCKER_LOG"
set -- "$command" "$@"
case "$1" in
 image) exit 0 ;;
 network) if [ "$2" = "create" ]; then exit "${FAIL_NETWORK:-0}"; fi; exit "${FAIL_CLEANUP:-0}" ;;
 port) if [ "$3" = '8000/tcp' ]; then printf '%s\n' "$INSTALL_ADDR"; else printf '%s\n' "${DB_ADDR:-127.0.0.1:13306}"; fi; exit "${FAIL_PORT:-0}" ;;
 run) case "$*" in *vendor/bin/pest*) exit "${PEST_EXIT:-0}" ;; esac; exit "${FAIL_RUN:-0}" ;;
 exec) case "$*" in
   *healthcheck.sh*) exit "${FAIL_HEALTH:-0}" ;;
   *mariadb-dump*) printf '%s' 'CREATE TABLE example (id INT);'; exit "${FAIL_DUMP:-0}" ;;
   *) cat > "$DOCKER_INPUT"; exit "${FAIL_IMPORT:-0}" ;;
 esac ;;
 rm) exit "${FAIL_CLEANUP:-0}" ;;
esac
exit 2
`

type dockerFixture struct {
	runner     *process.Runner
	log, input string
}

func fakeOrchestrationDocker(t *testing.T) *dockerFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake docker")
	}
	oldTimeout, oldPoll := readyTimeout, pollInterval
	readyTimeout, pollInterval = 2*time.Second, time.Millisecond
	t.Cleanup(func() { readyTimeout, pollInterval = oldTimeout, oldPoll })
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "docker")
	write(t, bin, orchestrationDocker)
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &dockerFixture{runner: process.NewRunner(process.Options{}), log: filepath.Join(dir, "docker.log"), input: filepath.Join(dir, "stdin.sql")}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_LOG", f.log)
	t.Setenv("DOCKER_INPUT", f.input)
	t.Setenv("DB_ADDR", "127.0.0.1:13306")
	t.Setenv("INSTALL_ADDR", "127.0.0.1:1")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	for _, key := range []string{"FAIL_NETWORK", "FAIL_CLEANUP", "FAIL_PORT", "FAIL_RUN", "FAIL_HEALTH", "FAIL_DUMP", "FAIL_IMPORT", "PEST_EXIT"} {
		t.Setenv(key, "0")
	}
	return f
}

func (f *dockerFixture) commands(t *testing.T) [][]string {
	t.Helper()
	b, err := os.ReadFile(f.log)
	if err != nil {
		t.Fatal(err)
	}
	var commands [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		commands = append(commands, strings.Split(line, "\t"))
	}
	return commands
}

func containsArgs(args []string, want ...string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func requireArgs(t *testing.T, args []string, want ...string) {
	t.Helper()
	if !containsArgs(args, want...) {
		t.Errorf("command %q lacks %q", args, want)
	}
}

func TestEnvironmentLifecycle(t *testing.T) {
	f := fakeOrchestrationDocker(t)
	ctx := context.Background()
	e, err := startEnv(ctx, f.runner)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(e.Name, "dleplugin-test-") || e.DBAddr != "127.0.0.1:13306" {
		t.Fatalf("environment = %+v", e)
	}
	dump := []byte("INSERT INTO example VALUES (42);\n")
	if err := e.Import(ctx, dump); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(f.input); err != nil || string(got) != string(dump) {
		t.Fatalf("stdin = %q, %v", got, err)
	}
	if got, err := e.Dump(ctx); err != nil || string(got) != "CREATE TABLE example (id INT);" {
		t.Fatalf("dump = %q, %v", got, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := e.Close(canceled, e.Name+"-php"); err != nil {
		t.Fatal(err)
	}
	cmds := f.commands(t)
	if len(cmds) != 8 {
		t.Fatalf("commands = %q", cmds)
	}
	requireArgs(t, cmds[0], "network", "create", e.Name)
	for _, pair := range [][]string{{"--network", e.Name}, {"--network-alias", "db"}, {"--tmpfs", "/var/lib/mysql"}, {"--publish", "127.0.0.1::3306"}, {MariaDBImage}} {
		requireArgs(t, cmds[1], pair...)
	}
	if containsArgs(cmds[1], "--network", "host") {
		t.Error("database uses host network")
	}
	requireArgs(t, cmds[3], "healthcheck.sh", "--connect", "--innodb_initialized")
	requireArgs(t, cmds[4], "exec", "--interactive", e.dbContainer(), "mariadb", "--user=dle", "--password=dle", "dle")
	requireArgs(t, cmds[5], "mariadb-dump", "--user=dle", "--password=dle", "--single-transaction", "--skip-dump-date", "dle")
	requireArgs(t, cmds[6], "rm", "--force", "--volumes", e.dbContainer(), e.Name+"-php")
	requireArgs(t, cmds[7], "network", "rm", e.Name)
	if want := []string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())}; !reflect.DeepEqual(userArgs(), want) {
		t.Errorf("userArgs = %q, want %q", userArgs(), want)
	}
}

func TestEnvironmentFailuresAndCleanup(t *testing.T) {
	for _, stage := range []string{"FAIL_NETWORK", "FAIL_RUN", "FAIL_PORT", "FAIL_HEALTH"} {
		t.Run(stage, func(t *testing.T) {
			f := fakeOrchestrationDocker(t)
			t.Setenv(stage, "7")
			old := readyTimeout
			readyTimeout = 0
			t.Cleanup(func() { readyTimeout = old })
			e, err := startEnv(context.Background(), f.runner)
			if err == nil {
				t.Fatal("expected setup failure")
			}
			if stage == "FAIL_HEALTH" && !strings.Contains(err.Error(), "database is not ready") {
				t.Errorf("error = %v", err)
			}
			t.Setenv("FAIL_CLEANUP", "9")
			if err := e.Close(context.Background(), e.Name+"-web"); err == nil || !strings.Contains(err.Error(), "network rm") || !strings.Contains(err.Error(), "rm --force") {
				t.Errorf("cleanup error = %v", err)
			}
			cmds := f.commands(t)
			requireArgs(t, cmds[len(cmds)-2], "rm", "--force", "--volumes", e.dbContainer(), e.Name+"-web")
			requireArgs(t, cmds[len(cmds)-1], "network", "rm", e.Name)
		})
	}
}

func TestPublishedAddress(t *testing.T) {
	f := fakeOrchestrationDocker(t)
	e := &env{runner: f.runner}
	for _, tc := range []struct{ out, want string }{{"[::1]:42\n127.0.0.1:43", "127.0.0.1:43"}, {"invalid", ""}, {"[::1]:42", ""}} {
		t.Setenv("DB_ADDR", tc.out)
		got, err := e.publishedAddr(context.Background(), "db", "3306/tcp")
		if tc.want == "" {
			if err == nil || !strings.Contains(err.Error(), "unexpected output") {
				t.Errorf("output %q: %v", tc.out, err)
			}
		} else if err != nil || got != tc.want {
			t.Errorf("address = %q, %v", got, err)
		}
	}
}

func TestWaitFor(t *testing.T) {
	oldTimeout, oldPoll := readyTimeout, pollInterval
	readyTimeout, pollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { readyTimeout, pollInterval = oldTimeout, oldPoll })
	e := &env{}
	attempts := 0
	if err := e.waitFor(context.Background(), "database", func() error {
		attempts++
		if attempts < 3 {
			return errors.New("starting")
		}
		return nil
	}); err != nil || attempts != 3 {
		t.Fatalf("retry: %d, %v", attempts, err)
	}
	cause := errors.New("unavailable")
	if err := e.waitFor(context.Background(), "installer", func() error { return cause }); !errors.Is(err, cause) || !strings.Contains(err.Error(), "installer is not ready after") {
		t.Errorf("timeout = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.waitFor(ctx, "database", func() error { cancel(); return cause }); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled = %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := e.waitFor(ctx, "database", func() error { return cause }); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline = %v", err)
	}
}

func TestCacheRootEnvironment(t *testing.T) {
	for _, xdg := range []string{filepath.Join(t.TempDir(), "xdg"), ""} {
		t.Setenv("XDG_CACHE_HOME", xdg)
		t.Setenv("HOME", t.TempDir())
		composer, err := paths.ComposerCacheDir()
		if err != nil {
			t.Fatal(err)
		}
		got, err := CacheRoot()
		want := filepath.Join(filepath.Dir(composer), "test") // dleplugin/test next to dleplugin/composer
		if xdg != "" {
			want = filepath.Join(xdg, "dleplugin", "test")
		}
		if err != nil || got != want {
			t.Errorf("CacheRoot = %q, %v; want %q", got, err, want)
		}
	}
}

type recordingRegistry struct {
	names []string
	fail  error
}

func (r *recordingRegistry) Add(_ context.Context, info *manifestpkg.Info) error {
	r.names = append(r.names, info.Slug)
	return r.fail
}

func injectRegistry(t *testing.T, registry dle.Registry) *int {
	t.Helper()
	old := openRegistry
	closed := new(int)
	openRegistry = func(cfg dle.DBConfig) (dle.Registry, func(), error) {
		if cfg.Host != "127.0.0.1:13306" || cfg.Name != dbName || cfg.Pass != dbPass || cfg.Prefix != dbPrefix {
			t.Errorf("database config = %+v", cfg)
		}
		return registry, func() { (*closed)++ }, nil
	}
	t.Cleanup(func() { openRegistry = old })
	return closed
}

func orchestrationPlugin(t *testing.T, dir, slug string) *plugin.Plugin {
	t.Helper()
	write(t, filepath.Join(dir, "dleplugin.yml"), "slug: "+slug+"\nname: "+slug+"\nmount: engine/Acme/"+slug+"\n")
	write(t, filepath.Join(dir, "src", "index.php"), "<?php\n")
	p, err := plugin.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConnectPlugins(t *testing.T) {
	registry := &recordingRegistry{}
	closed := injectRegistry(t, registry)
	dep := orchestrationPlugin(t, filepath.Join(t.TempDir(), "dependency"), "dependency")
	p := orchestrationPlugin(t, filepath.Join(t.TempDir(), "subject"), "subject")
	site := t.TempDir()
	if err := connectPlugins(context.Background(), site, "127.0.0.1:13306", []*plugin.Plugin{dep, p}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(registry.names, []string{"dependency", "subject"}) || *closed != 1 {
		t.Fatalf("registry = %v; closes = %d", registry.names, *closed)
	}
	for _, p := range []*plugin.Plugin{dep, p} {
		link := filepath.Join(site, "engine", "Acme", p.Slug(), "index.php")
		if got, err := os.Readlink(link); err != nil || got != filepath.Join(p.Root(), "src", "index.php") {
			t.Errorf("plugin link = %q, %v", got, err)
		}
	}
	registry.fail = errors.New("registry unavailable")
	if err := connectPlugins(context.Background(), site, "127.0.0.1:13306", []*plugin.Plugin{dep, p}); !errors.Is(err, registry.fail) || !strings.Contains(err.Error(), "connect dependency") {
		t.Errorf("connect error = %v", err)
	}
	if *closed != 2 || len(registry.names) != 3 {
		t.Errorf("failed connection continued: %v, closes %d", registry.names, *closed)
	}
}

func trialServer(t *testing.T) (string, *atomic.Int32, *fakeInstaller) {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "trial.zip")
	writeZip(t, archive, map[string]string{
		"upload/install.php":              "<?php // installer",
		"upload/index.php":                "<?php // index",
		"upload/engine/data/config.php":   "<?php $config = ['version_id' => '21.0', 'http_home_url' => 'http://temporary/'];",
		"upload/engine/cache/system/keep": "cache",
	})
	payload, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	downloads := &atomic.Int32{}
	installer := &fakeInstaller{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trial.zip" {
			downloads.Add(1)
			_, _ = w.Write(payload)
			return
		}
		installer.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("INSTALL_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	return srv.URL + "/trial.zip", downloads, installer
}

func TestRunDownloadsInstallsAndReusesBase(t *testing.T) {
	f := fakeOrchestrationDocker(t)
	url, downloads, installer := trialServer(t)
	registry := &recordingRegistry{}
	closed := injectRegistry(t, registry)
	dep := orchestrationPlugin(t, filepath.Join(t.TempDir(), "dependency"), "dependency")
	p := orchestrationPlugin(t, filepath.Join(t.TempDir(), "subject"), "subject")
	write(t, filepath.Join(p.Root(), "tests", "ExampleTest.php"), "<?php")
	write(t, filepath.Join(p.Root(), "src", "example.php"), "<?php")
	var messages []string
	o := Options{Plugin: p, Dependencies: []*plugin.Plugin{dep}, TrialURL: url, Notify: func(s string) { messages = append(messages, s) }, Filter: "example", Files: []string{"ExampleTest.php"}, CoverageHTML: filepath.Join(t.TempDir(), "report")}
	ctx := context.Background()
	if code, err := Run(ctx, f.runner, o); err != nil || code != 0 {
		t.Fatalf("Run = %d, %v", code, err)
	}
	root, err := CacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	sum, err := FileHash(filepath.Join(root, "dle_trial.zip"))
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "dle-"+sum[:12])
	if got, err := os.ReadFile(filepath.Join(base, ".ready")); err != nil || string(got) != "21.0\n" {
		t.Fatalf("ready = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(base, "site", "install.php")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("installer remains: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(base, "site", "engine/data/config.php")); !strings.Contains(string(got), SiteURL) {
		t.Errorf("config URL = %s", got)
	}
	if len(messages) != 3 || !strings.Contains(messages[0], url) || !strings.Contains(messages[1], "Installing") || messages[2] != "DLE 21.0 installed." {
		t.Errorf("messages = %q", messages)
	}
	cmds := f.commands(t)
	var pest []string
	for _, cmd := range cmds {
		if containsArgs(cmd, "--workdir", containerRoot) {
			pest = cmd
		}
	}
	if pest == nil {
		t.Fatal("Pest never ran")
	}
	requireArgs(t, pest, "--env", "HOME=/tmp")
	requireArgs(t, pest, userArgs()...)
	requireArgs(t, pest, "--volume", dep.Root()+":"+dep.Root()+":ro")
	requireArgs(t, pest, "--filter", "example")
	requireArgs(t, pest, "-c", "phpunit.xml", "ExampleTest.php")
	site := ""
	for i, arg := range pest {
		if arg == "--volume" && strings.HasSuffix(pest[i+1], ":/dle/index.php") {
			site = filepath.Dir(strings.TrimSuffix(pest[i+1], ":/dle/index.php"))
		}
	}
	if site == "" {
		t.Fatal("missing site mount")
	}
	if _, err := os.Stat(site); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run site remains after a successful run: %v", err)
	}
	write(t, filepath.Join(site, "stale.txt"), "stale")
	write(t, filepath.Join(site, "index.php"), "mutated")
	installer.mu.Lock()
	steps := len(installer.steps)
	installer.mu.Unlock()
	t.Setenv("PEST_EXIT", "3")
	if code, err := Run(ctx, f.runner, o); err != nil || code != 3 {
		t.Fatalf("Pest failure = %d, %v", code, err)
	}
	if downloads.Load() != 1 || len(messages) != 3 {
		t.Errorf("cache miss: downloads %d messages %q", downloads.Load(), messages)
	}
	installer.mu.Lock()
	after := len(installer.steps)
	installer.mu.Unlock()
	if steps != after {
		t.Error("cached base was reinstalled")
	}
	if _, err := os.Stat(site); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run site remains after a failed Pest run: %v", err)
	}
	if !reflect.DeepEqual(registry.names, []string{"dependency", "subject", "dependency", "subject"}) || *closed != 2 {
		t.Errorf("connections = %v, closed %d", registry.names, *closed)
	}
	cmds = f.commands(t)
	requireArgs(t, cmds[len(cmds)-1], "network", "rm")
	// Refresh must repeat both the download and installation, even for identical bytes.
	o.Refresh = true
	o.Notify = nil
	if _, err := prepareBase(ctx, f.runner, Image.Tag(), &o); err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != 2 {
		t.Errorf("refresh downloads = %d", downloads.Load())
	}
}

func TestRunSetupFailuresCleanUp(t *testing.T) {
	for _, stage := range []string{"FAIL_NETWORK", "FAIL_IMPORT"} {
		t.Run(stage, func(t *testing.T) {
			f := fakeOrchestrationDocker(t)
			url, _, _ := trialServer(t)
			p := orchestrationPlugin(t, t.TempDir(), "subject")
			write(t, filepath.Join(p.Root(), "tests", "Test.php"), "<?php")
			o := Options{Plugin: p, TrialURL: url}
			if _, err := prepareBase(context.Background(), f.runner, Image.Tag(), &o); err != nil {
				t.Fatal(err)
			}
			t.Setenv(stage, "8")
			_, err := Run(context.Background(), f.runner, o)
			want := "import test database"
			if stage == "FAIL_NETWORK" {
				want = "start test database"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("Run error = %v", err)
			}
			cmds := f.commands(t)
			requireArgs(t, cmds[len(cmds)-2], "rm", "--force", "--volumes")
			requireArgs(t, cmds[len(cmds)-1], "network", "rm")
			root, err := CacheRoot()
			if err != nil {
				t.Fatal(err)
			}
			if runDirs, err := filepath.Glob(filepath.Join(root, "run", "*")); err != nil || len(runDirs) != 0 {
				t.Errorf("run sites remain after setup failure: %q, %v", runDirs, err)
			}
		})
	}
}

func TestInstallBaseFailuresCleanUp(t *testing.T) {
	for _, tc := range []struct{ name, failure, want string }{
		{"database", "FAIL_NETWORK", "start install database"},
		{"dump", "FAIL_DUMP", "dump test database"},
		{"installer", "", "installer checkdb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fakeOrchestrationDocker(t)
			url, _, installer := trialServer(t)
			if tc.failure != "" {
				t.Setenv(tc.failure, "5")
			} else {
				installer.failDB = "database rejected"
			}
			var messages []string
			o := Options{TrialURL: url, Notify: func(s string) { messages = append(messages, s) }}
			if _, err := prepareBase(context.Background(), f.runner, Image.Tag(), &o); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("prepareBase = %v", err)
			}
			root, err := CacheRoot()
			if err != nil {
				t.Fatal(err)
			}
			ready, err := filepath.Glob(filepath.Join(root, "dle-*", ".ready"))
			if err != nil || len(ready) != 0 {
				t.Fatalf("failed install marked ready: %q, %v", ready, err)
			}
			if len(messages) != 2 {
				t.Errorf("failure notifications = %q", messages)
			}
			cmds := f.commands(t)
			requireArgs(t, cmds[len(cmds)-2], "rm", "--force", "--volumes")
			requireArgs(t, cmds[len(cmds)-1], "network", "rm")
			// The archive remains usable after failure, but installation must retry.
			t.Setenv("FAIL_NETWORK", "0")
			t.Setenv("FAIL_DUMP", "0")
			installer.mu.Lock()
			installer.failDB = ""
			installer.mu.Unlock()
			if _, err := prepareBase(context.Background(), f.runner, Image.Tag(), &o); err != nil {
				t.Fatalf("retry = %v", err)
			}
		})
	}
}

func TestDatabaseCommandFailures(t *testing.T) {
	f := fakeOrchestrationDocker(t)
	e := &env{runner: f.runner, Name: "failure"}
	t.Setenv("FAIL_IMPORT", "4")
	if err := e.Import(context.Background(), []byte("sql")); err == nil {
		t.Error("import failure ignored")
	}
	t.Setenv("FAIL_DUMP", "6")
	if _, err := e.Dump(context.Background()); err == nil {
		t.Error("dump failure ignored")
	}
}

func TestConnectPluginsOpenFailure(t *testing.T) {
	old := openRegistry
	t.Cleanup(func() { openRegistry = old })
	cause := errors.New("cannot open registry")
	openRegistry = func(dle.DBConfig) (dle.Registry, func(), error) { return nil, nil, cause }
	if err := connectPlugins(context.Background(), t.TempDir(), "unused", nil); !errors.Is(err, cause) {
		t.Errorf("registry error = %v", err)
	}
}

func TestPruneBases(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "dle-current")
	for _, name := range []string{"dle-old", "dle-previous", "dle-current", "other"} {
		write(t, filepath.Join(root, name, ".ready"), "21.0\n")
	}
	now := time.Now()
	for name, age := range map[string]time.Duration{"dle-old": 3 * time.Hour, "dle-previous": time.Hour, "dle-current": 2 * time.Hour} {
		p := filepath.Join(root, name)
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := pruneBases(root, current, keepBases)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "dle-old")}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %q, want %q", removed, want)
	}
	for _, name := range []string{"dle-previous", "dle-current", "other"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s removed: %v", name, err)
		}
	}
	removed, err = pruneBases(root, current, 1)
	if err != nil || !reflect.DeepEqual(removed, []string{filepath.Join(root, "dle-previous")}) {
		t.Errorf("keep=1 removed = %q, %v", removed, err)
	}
	if removed, err := pruneBases(t.TempDir(), current, keepBases); err != nil || len(removed) != 0 {
		t.Errorf("missing root: %q, %v", removed, err)
	}
}

func TestNewRunSiteReplacesExisting(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	base := t.TempDir()
	write(t, filepath.Join(base, "site", "index.php"), "fresh")
	p := loadPlugin(t, filepath.Join(t.TempDir(), "Plugin"), "Plugin")
	site, err := newRunSite(base, p)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(site, "index.php")); err != nil || string(b) != "fresh" {
		t.Fatalf("site = %q, %v", b, err)
	}
	write(t, filepath.Join(site, "stale.txt"), "stale")
	again, err := newRunSite(base, p)
	if err != nil {
		t.Fatal(err)
	}
	if again != site {
		t.Errorf("site path changed: %q -> %q", site, again)
	}
	if _, err := os.Stat(filepath.Join(site, "stale.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale file survives: %v", err)
	}
}

func TestRunKeepsRunSite(t *testing.T) {
	f := fakeOrchestrationDocker(t)
	url, _, _ := trialServer(t)
	injectRegistry(t, &recordingRegistry{})
	p := orchestrationPlugin(t, t.TempDir(), "subject")
	write(t, filepath.Join(p.Root(), "tests", "Test.php"), "<?php")
	var messages []string
	o := Options{Plugin: p, TrialURL: url, KeepRun: true, Notify: func(s string) { messages = append(messages, s) }}
	if code, err := Run(context.Background(), f.runner, o); err != nil || code != 0 {
		t.Fatalf("Run = %d, %v", code, err)
	}
	root, err := CacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum([]byte(p.Root()))
	site := filepath.Join(root, "run", p.Slug()+"-"+hex.EncodeToString(sum[:])[:6])
	if _, err := os.Stat(filepath.Join(site, "index.php")); err != nil {
		t.Fatalf("run site kept: %v", err)
	}
	if !slices.Contains(messages, "Keeping run site "+site) {
		t.Errorf("messages = %q", messages)
	}
	if err := os.RemoveAll(site); err != nil {
		t.Fatal(err)
	}
}
