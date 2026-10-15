package static_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/buildcmd"
	staticmod "github.com/thecoooler/dleplugin/internal/modules/static"
	"github.com/thecoooler/dleplugin/internal/modules/static/internal/static"
)

// fakeDocker is a "docker" executable on PATH that logs its arguments and
// answers `image inspect` with inspectCode and `run` with out/runCode.
const fakeDocker = `#!/bin/sh
echo "$@" >> "$FAKE_DOCKER_LOG"
case "$1" in
  image) exit "${FAKE_DOCKER_INSPECT:-0}" ;;
  build) cat > /dev/null; exit 0 ;;
  run) cat "$FAKE_DOCKER_OUT"; printf "%s" "$FAKE_DOCKER_STDERR" >&2; exit "${FAKE_DOCKER_EXIT:-1}" ;;
esac
exit 2
`

type env struct {
	rt               *app.Runtime
	out, errB        *bytes.Buffer
	home, plugin, wd string
	log, phpstanOut  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &env{
		out: &bytes.Buffer{}, errB: &bytes.Buffer{},
		home:   filepath.Join(tmp, "home"),
		plugin: filepath.Join(tmp, "plugin"),
		log:    filepath.Join(tmp, "docker.log"), phpstanOut: filepath.Join(tmp, "phpstan.json"),
	}
	e.wd = e.plugin
	bin := filepath.Join(tmp, "bin")
	write(t, filepath.Join(bin, "docker"), fakeDocker)
	if err := os.Chmod(filepath.Join(bin, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", e.home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "xdg"))
	t.Setenv("FAKE_DOCKER_LOG", e.log)
	t.Setenv("FAKE_DOCKER_OUT", e.phpstanOut)
	t.Setenv("FAKE_DOCKER_INSPECT", "0")
	t.Setenv("FAKE_DOCKER_EXIT", "1")
	t.Setenv("FAKE_DOCKER_STDERR", "")
	write(t, e.phpstanOut, `{"files":[]}`)
	write(t, filepath.Join(e.plugin, "dleplugin.yml"), "slug: TestPlugin\nname: Test Plugin\n")
	write(t, filepath.Join(e.plugin, "src", "index.php"), "<?php\n")
	return e
}

func (e *env) config(t *testing.T, content string) {
	t.Helper()
	write(t, filepath.Join(e.home, ".config", "dleplugin", "config.yml"), content)
}

func (e *env) run(args ...string) int {
	e.rt = app.NewRuntime(strings.NewReader(""), e.out, e.errB, filepath.Join(e.home, ".config", "dleplugin", "config.yml"))
	e.rt.SetWorkDir(e.wd)
	return app.Execute(context.Background(), e.rt, args, buildcmd.Module(), staticmod.Module())
}

// dockerRuns returns the logged `docker run` argument lines.
func (e *env) dockerRuns(t *testing.T) []string {
	t.Helper()
	b, _ := os.ReadFile(e.log)
	var runs []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.HasPrefix(l, "run ") {
			runs = append(runs, l)
		}
	}
	return runs
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const findings = `{"totals":{"errors":0,"file_errors":4},"files":{` +
	`"/work/src/index.php":{"errors":3,"messages":[` +
	`{"message":"Line two","line":2},{"message":"Line ten","line":10},{"message":"Ignored by config","line":11}]},` +
	`"/work/src/other.php":{"errors":2,"messages":[{"message":"First","line":4},{"message":"Second","line":4}]}},"errors":[]}`

func TestRegistersCleanly(t *testing.T) {
	e := newEnv(t)
	e.rt = app.NewRuntime(strings.NewReader(""), e.out, e.errB, "")
	app.NewRootCommand(e.rt, buildcmd.Module(), staticmod.Module())
	if errs := e.rt.ModuleErrors(); len(errs) != 0 {
		t.Fatalf("module errors: %v", errs)
	}
}

func TestHelp(t *testing.T) {
	for _, cmd := range []string{"static"} {
		e := newEnv(t)
		if code := e.run(cmd, "--help"); code != 0 || !strings.Contains(e.out.String(), "Usage:") {
			t.Fatalf("%s: exit %d: %s", cmd, code, e.errB)
		}
	}
}

func TestStaticLines(t *testing.T) {
	e := newEnv(t)
	write(t, e.phpstanOut, findings)
	write(t, filepath.Join(e.plugin, static.ConfFile), "ignoreErrors:\n  - Ignored by config\ndependencies: [Missing]\n")
	if code := e.run("static"); code != 1 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	want := "src/index.php:10: Line ten\nsrc/index.php:2: Line two\nsrc/other.php:4: First\nsrc/other.php:4: Second\n\n[INFO] 4 error(s)\n"
	if e.out.String() != want {
		t.Errorf("out = %q", e.out.String())
	}
	if e.errB.String() != "⚠ [WARNING] Dependency not found: Missing\n" {
		t.Errorf("err = %q", e.errB.String())
	}
	runs := e.dockerRuns(t)
	if len(runs) != 1 || !strings.HasSuffix(runs[0], "/opt/tools/vendor/bin/phpstan analyze --memory-limit 512M --error-format=json --no-progress -c phpstan.neon src") {
		t.Fatalf("runs = %v", runs)
	}
	if !strings.Contains(runs[0], " --volume "+filepath.Join(e.plugin, "src")+":/work/src:ro ") {
		t.Errorf("src not mounted: %s", runs[0])
	}
}

func TestStaticCountAndJSON(t *testing.T) {
	e := newEnv(t)
	write(t, e.phpstanOut, findings)
	if code := e.run("static", "--count"); code != 1 || e.out.String() != "4\n" {
		t.Fatalf("count: exit %d out %q err %q", code, e.out, e.errB)
	}

	e = newEnv(t)
	write(t, e.phpstanOut, findings)
	if code := e.run("static", "--json"); code != 1 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	var got struct {
		Messages []static.Message  `json:"messages"`
		Errors   map[string]string `json:"errors"`
		Warnings []string          `json:"warnings"`
		Count    int               `json:"count"`
	}
	if err := json.Unmarshal(e.out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, e.out)
	}
	var texts []string
	for _, m := range got.Messages {
		texts = append(texts, m.File+":"+m.Message)
	}
	if !reflect.DeepEqual(texts, []string{"src/index.php:Line two", "src/index.php:Line ten", "src/index.php:Ignored by config", "src/other.php:First"}) || got.Count != 4 {
		t.Errorf("json = %s", e.out)
	}
	if !strings.HasPrefix(e.out.String(), `{"messages":[{"provider":"phpstan","file":"src/index.php","line":2,"message":"Line two","tip":null}`) ||
		!strings.HasSuffix(e.out.String(), `"errors":{},"warnings":[],"count":4}`+"\n") {
		t.Errorf("json = %s", e.out)
	}
}

func TestStaticCleanExitsZero(t *testing.T) {
	e := newEnv(t)
	if code := e.run("static"); code != 0 || e.out.String() != "\n[INFO] 0 error(s)\n" || e.errB.String() != "" {
		t.Fatalf("exit %d out %q err %q", code, e.out, e.errB)
	}
	// findings removed by ignoreErrors do not fail the run
	e = newEnv(t)
	write(t, e.phpstanOut, `{"files":{"/work/src/index.php":{"messages":[{"message":"Ignored by config","line":1}]}}}`)
	write(t, filepath.Join(e.plugin, static.ConfFile), "ignoreErrors: [Ignored]\n")
	if code := e.run("static", "--json"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
}

func TestStaticFilesFromSubdirectory(t *testing.T) {
	e := newEnv(t)
	e.wd = filepath.Join(e.plugin, "src")
	if code := e.run("static", "index.php"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	runs := e.dockerRuns(t)
	if len(runs) != 1 || !strings.HasSuffix(runs[0], " -c phpstan.neon src/index.php") {
		t.Fatalf("runs = %v", runs)
	}
}

func TestStaticConfigAndFlags(t *testing.T) {
	e := newEnv(t)
	modules := filepath.Join(e.home, "modules")
	write(t, filepath.Join(modules, "Dep", "src", "y.php"), "")
	write(t, filepath.Join(e.plugin, static.ConfFile), "dependencies: [Dep]\nstrict: true\n")
	e.config(t, "dependency_roots: ["+modules+"]\n")
	if code := e.run("static"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	run := e.dockerRuns(t)[0]
	for _, s := range []string{filepath.Join(modules, "Dep", "src") + ":/include/0:ro"} {
		if !strings.Contains(run, s) {
			t.Errorf("run lacks %q: %s", s, run)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(e.home, "..", "xdg", "dleplugin", "static", "TestPlugin-*", "phpstan.neon"))
	if len(matches) != 1 {
		t.Fatalf("phpstan.neon: %v", matches)
	}
	neon, _ := os.ReadFile(matches[0])
	for _, s := range []string{"- /include/0", "phpstan-strict-rules/rules.neon"} {
		if !strings.Contains(string(neon), s) {
			t.Errorf("neon lacks %q:\n%s", s, neon)
		}
	}

	// Flags override dependency roots from config.
	other := filepath.Join(e.home, "other")
	write(t, filepath.Join(other, "Dep", "src", "z.php"), "")
	if code := e.run("static", "--dependency-root", "/missing", "--dependency-root", other); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	if run := e.dockerRuns(t)[1]; !strings.Contains(run, filepath.Join(other, "Dep", "src")+":/include/0:ro") {
		t.Errorf("run = %s", run)
	}
}

func TestStaticPHPVersion(t *testing.T) {
	e := newEnv(t)
	if code := e.run("static", "--php-version", "8.1"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	matches, _ := filepath.Glob(filepath.Join(e.home, "..", "xdg", "dleplugin", "static", "TestPlugin-*", "phpstan.neon"))
	if len(matches) != 1 {
		t.Fatalf("phpstan.neon: %v", matches)
	}
	if neon, _ := os.ReadFile(matches[0]); !strings.Contains(string(neon), "phpVersion: 80100") {
		t.Errorf("neon lacks phpVersion:\n%s", neon)
	}

	// an invalid version fails before docker runs
	e = newEnv(t)
	if code := e.run("static", "--php-version", "8"); code != 1 || !strings.Contains(e.errB.String(), `invalid PHP version "8"`) {
		t.Errorf("exit %d err %q", code, e.errB)
	}
	if b, _ := os.ReadFile(e.log); len(b) > 0 {
		t.Errorf("docker ran: %s", b)
	}
}

func TestStaticProviderFailure(t *testing.T) {
	e := newEnv(t)
	write(t, e.phpstanOut, "PHP Fatal error: oops")
	t.Setenv("FAKE_DOCKER_EXIT", "255")
	if code := e.run("static"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(e.out.String(), "[INFO] 0 error(s)") || !strings.Contains(e.errB.String(), "✖ [ERROR] PHPStanProvider: Command failed: docker run") {
		t.Errorf("out %q err %q", e.out, e.errB)
	}

	// stderr with exit code 0 is a failure too
	e = newEnv(t)
	t.Setenv("FAKE_DOCKER_EXIT", "0")
	t.Setenv("FAKE_DOCKER_STDERR", "warning")
	if code := e.run("static", "--json"); code != 1 || !strings.Contains(e.out.String(), `"errors":{"PHPStanProvider":"Command failed:`) {
		t.Errorf("exit %d out %q", code, e.out)
	}
}

func TestStaticBuildsMissingImage(t *testing.T) {
	e := newEnv(t)
	t.Setenv("FAKE_DOCKER_INSPECT", "1")
	if code := e.run("static", "--count"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	if e.out.String() != "0\n" {
		t.Errorf("stdout polluted: %q", e.out)
	}
	tag := static.ImageTag()
	if !strings.Contains(e.errB.String(), "Building static analysis image "+tag+"...") {
		t.Errorf("err = %q", e.errB)
	}
	b, _ := os.ReadFile(e.log)
	if !strings.Contains(string(b), "build --tag "+tag+" -\n") {
		t.Errorf("log = %s", b)
	}
}

func TestStaticErrors(t *testing.T) {
	e := newEnv(t)
	e.config(t, "static: invalid\n")
	if code := e.run("static"); code != 1 || !strings.Contains(e.errB.String(), "static must be a mapping") {
		t.Errorf("exit %d err %q", code, e.errB)
	}

	e = newEnv(t)
	e.wd = e.home
	if code := e.run("static"); code != 1 || !strings.Contains(e.errB.String(), "dleplugin.yml was not found") {
		t.Errorf("exit %d err %q", code, e.errB)
	}

	e = newEnv(t)
	t.Setenv("PATH", t.TempDir())
	if code := e.run("static"); code != 1 || !strings.Contains(e.errB.String(), "Docker not found") {
		t.Errorf("exit %d err %q", code, e.errB)
	}
}
