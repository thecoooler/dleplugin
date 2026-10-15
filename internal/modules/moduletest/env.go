// Package moduletest provides shared command-layer test helpers.
package moduletest

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
)

type Env struct {
	modules    []app.Module
	RT         *app.Runtime
	Out, ErrB  *bytes.Buffer
	Work       string
	ConfigPath string
}

func NewEnv(t *testing.T, stdin string, modules ...app.Module) *Env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, "cfg", "config.yml")
	e := &Env{modules: modules, Out: &bytes.Buffer{}, ErrB: &bytes.Buffer{}, Work: t.TempDir(), ConfigPath: cfg}
	e.RT = app.NewRuntime(strings.NewReader(stdin), e.Out, e.ErrB, cfg)
	e.RT.SetWorkDir(e.Work)
	return e
}

func (e *Env) Run(args ...string) int {
	return app.Execute(context.Background(), e.RT, args, e.modules...)
}

func (e *Env) NoConfig(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(e.ConfigPath); err == nil {
		t.Fatal("config file was created")
	}
}

func Golden(t *testing.T, parts ...string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "..", "testdata", "golden"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func ReadFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func CopyDir(t *testing.T, src, dst string) {
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
