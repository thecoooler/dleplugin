package test_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/buildcmd"
	testmod "github.com/thecoooler/dleplugin/internal/modules/test"
)

type env struct {
	out, errB        *bytes.Buffer
	home, plugin, wd string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &env{out: &bytes.Buffer{}, errB: &bytes.Buffer{}, home: filepath.Join(tmp, "home"), plugin: filepath.Join(tmp, "plugin")}
	e.wd = e.plugin
	t.Setenv("HOME", e.home)
	// No docker: these tests must fail or finish before Docker is needed.
	t.Setenv("PATH", t.TempDir())
	write(t, filepath.Join(e.plugin, "dleplugin.yml"), "slug: TestPlugin\nname: Test Plugin\n")
	write(t, filepath.Join(e.plugin, "src", "index.php"), "<?php\n")
	return e
}

func (e *env) run(args ...string) int {
	rt := app.NewRuntime(strings.NewReader(""), e.out, e.errB, filepath.Join(e.home, ".config", "dleplugin", "config.yml"))
	rt.SetWorkDir(e.wd)
	return app.Execute(context.Background(), rt, args, buildcmd.Module(), testmod.Module())
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
	app.NewRootCommand(rt, buildcmd.Module(), testmod.Module())
	for _, m := range rt.Modules() {
		if m.Err != nil {
			t.Errorf("%s: %v", m.Name, m.Err)
		}
	}
}

func TestHelp(t *testing.T) {
	e := newEnv(t)
	if code := e.run("test", "--help"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, s := range []string{"test [FILES...]", "--filter", "--update-snapshots", "--coverage-min", "--coverage-html", "--refresh-dle", "--keep-run", "DLE trial"} {
		if !strings.Contains(e.out.String(), s) {
			t.Errorf("help lacks %q", s)
		}
	}
}

func TestNoTests(t *testing.T) {
	e := newEnv(t)
	if code := e.run("test"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.errB)
	}
	if !strings.Contains(e.errB.String(), "No tests found") {
		t.Errorf("err = %q", e.errB)
	}
}

func TestErrors(t *testing.T) {
	e := newEnv(t)
	write(t, filepath.Join(e.plugin, "tests", "ATest.php"), "<?php\n")
	write(t, filepath.Join(e.plugin, "dleplugin.yml"), "slug: TestPlugin\nname: Test Plugin\ndependencies: [Missing]\n")
	if code := e.run("test"); code != 1 || !strings.Contains(e.errB.String(), "Dependency not found: Missing") {
		t.Errorf("exit %d err %q", code, e.errB)
	}

	e = newEnv(t)
	write(t, filepath.Join(e.home, ".config", "dleplugin", "config.yml"), "test: invalid\n")
	if code := e.run("test"); code != 1 || !strings.Contains(e.errB.String(), "test must be a mapping") {
		t.Errorf("exit %d err %q", code, e.errB)
	}

	e = newEnv(t)
	e.wd = e.home
	if err := os.MkdirAll(e.home, 0o755); err != nil {
		t.Fatal(err)
	}
	if code := e.run("test"); code != 1 || !strings.Contains(e.errB.String(), "dleplugin.yml was not found") {
		t.Errorf("exit %d err %q", code, e.errB)
	}

	e = newEnv(t)
	write(t, filepath.Join(e.plugin, "tests", "ATest.php"), "<?php\n")
	if code := e.run("test"); code != 1 || !strings.Contains(e.errB.String(), "Docker not found") {
		t.Errorf("exit %d err %q", code, e.errB)
	}
}
