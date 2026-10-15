package check_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/buildcmd"
	checkmod "github.com/thecoooler/dleplugin/internal/modules/check"
)

type env struct {
	out, errB         *bytes.Buffer
	home, plugin, tmp string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &env{out: &bytes.Buffer{}, errB: &bytes.Buffer{}, tmp: tmp, home: filepath.Join(tmp, "home"), plugin: filepath.Join(tmp, "plugin")}
	t.Setenv("HOME", e.home)
	write(t, filepath.Join(e.plugin, "dleplugin.yml"), `slug: example
name: Example
files:
  engine/modules/main.php:
    - before: {search: "needle", replace: "code"}
    - after: {search: "missing", replace: "code"}
  engine/modules/new.php:
    - create: new file
`)
	return e
}

func (e *env) run(args ...string) int {
	e.out.Reset()
	e.errB.Reset()
	rt := app.NewRuntime(strings.NewReader(""), e.out, e.errB, filepath.Join(e.home, ".config", "dleplugin", "config.yml"))
	rt.SetWorkDir(e.plugin)
	return app.Execute(context.Background(), rt, args, buildcmd.Module(), checkmod.Module())
}

// dist creates a DLE directory with engine/modules/main.php.
func (e *env) dist(t *testing.T, name, version, main string) string {
	t.Helper()
	dir := filepath.Join(e.tmp, "dists", name)
	write(t, filepath.Join(dir, "engine/inc/include/init.php"), "<?php define('VERSIONID', '"+version+"');")
	write(t, filepath.Join(dir, "engine/modules/main.php"), main)
	return dir
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

func TestRegistersCleanly(t *testing.T) {
	e := newEnv(t)
	rt := app.NewRuntime(strings.NewReader(""), e.out, e.errB, "")
	app.NewRootCommand(rt, buildcmd.Module(), checkmod.Module())
	for _, m := range rt.Modules() {
		if m.Err != nil {
			t.Errorf("%s: %v", m.Name, m.Err)
		}
	}
}

func TestHelp(t *testing.T) {
	e := newEnv(t)
	if code := e.run("check", "--help"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, s := range []string{"check DLE_DIR...", "--file", "init.php"} {
		if !strings.Contains(e.out.String(), s) {
			t.Errorf("help lacks %q", s)
		}
	}
}

func TestCheckSeveralDirs(t *testing.T) {
	e := newEnv(t)
	ok := e.dist(t, "ok", "18.1", "needle\nmissing\n")
	bad := e.dist(t, "bad", "17.0", "needle needle\n")

	if code := e.run("check", ok); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	if want := "DLE 18.1 (" + ok + "): OK\n"; e.out.String() != want {
		t.Errorf("out = %q, want %q", e.out, want)
	}

	if code := e.run("check", ok, bad); code != 1 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	want := "DLE 18.1 (" + ok + "): OK\n" +
		"DLE 17.0 (" + bad + "): ERROR\n" +
		`  [WARNING] DUPLICATE in engine/modules/main.php: "needle" (2 matches)` + "\n" +
		`  TEXT_NOT_FOUND in engine/modules/main.php: "missing"` + "\n"
	if e.out.String() != want {
		t.Errorf("out = %q\nwant %q", e.out, want)
	}
	if !strings.Contains(e.errB.String(), "do not apply to 1 of 2 DLE directories") {
		t.Errorf("err = %q", e.errB)
	}
}

func TestCheckXMLFile(t *testing.T) {
	e := newEnv(t)
	dir := e.dist(t, "ok", "18.1", "other\n")
	write(t, filepath.Join(e.plugin, "dist", "p.xml"), `<dleplugin><name>x</name>
<file name="engine/modules/main.php"><operation action="replace"><searchcode>other</searchcode></operation></file>
<file name="engine/modules/gone.php"><operation action="replace"><searchcode>x</searchcode></operation></file>
</dleplugin>`)
	if code := e.run("check", "-f", "dist/p.xml", dir); code != 1 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	if !strings.Contains(e.out.String(), "  FILE_NOT_FOUND: engine/modules/gone.php\n") || strings.Contains(e.out.String(), "TEXT_NOT_FOUND") {
		t.Errorf("out = %q", e.out)
	}
}

func TestErrors(t *testing.T) {
	e := newEnv(t)
	dir := e.dist(t, "ok", "18.1", "")
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"check"}, "requires at least 1 arg"},
		{[]string{"check", e.home}, "is not a DLE directory"},
		{[]string{"check", "-f", "nope.xml", dir}, "plugin file"},
	} {
		if code := e.run(tt.args...); code != 1 || !strings.Contains(e.errB.String(), tt.want) {
			t.Errorf("%v: exit %d err %q", tt.args, code, e.errB)
		}
	}

	write(t, filepath.Join(e.plugin, "dleplugin.yml"), "slug: example\n")
	if code := e.run("check", dir); code != 1 || !strings.Contains(e.errB.String(), "dleplugin.yml validation error") {
		t.Errorf("exit %d err %q", code, e.errB)
	}
}
