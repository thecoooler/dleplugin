package initcmd_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/initcmd"
	"github.com/thecoooler/dleplugin/internal/modules/moduletest"
)

var cyrillic = regexp.MustCompile(`\p{Cyrillic}`)

func TestHelp(t *testing.T) {
	for _, args := range [][]string{
		{}, {"init"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := moduletest.NewEnv(t, "", initcmd.Module())
			if code := e.Run(append(args, "--help")...); code != 0 {
				t.Fatalf("exit %d: %s", code, e.ErrB)
			}
			out := e.Out.String()
			if !strings.Contains(out, "Usage:") {
				t.Fatalf("no Usage: %s", out)
			}
			if cyrillic.MatchString(out) {
				t.Fatalf("cyrillic in help: %s", out)
			}
			e.NoConfig(t)
		})
	}
}

func TestRegistersCleanly(t *testing.T) {
	e := moduletest.NewEnv(t, "", initcmd.Module())
	app.NewRootCommand(e.RT, initcmd.Module())
	if errs := e.RT.ModuleErrors(); len(errs) != 0 {
		t.Fatalf("module errors: %v", errs)
	}
}

func TestInitFlags(t *testing.T) {
	want := moduletest.ReadFile(t, moduletest.Golden(t, "init", "dleplugin.yml"))
	flags := []string{"--slug", "Demo", "--name", "Demo Plugin", "--description", "Демо описание: 'x'", "--namespace", `Acme\Demo`}

	e := moduletest.NewEnv(t, "", initcmd.Module())
	if code := e.Run(append([]string{"init", "sub/Demo"}, flags...)...); code != 0 {
		t.Fatalf("init DIR exit %d: %s", code, e.ErrB)
	}
	dir := filepath.Join(e.Work, "sub", "Demo")
	if got := moduletest.ReadFile(t, filepath.Join(dir, "dleplugin.yml")); got != want {
		t.Fatalf("init DIR yml mismatch:\n%s", got)
	}
	if !strings.Contains(e.Out.String(), `Plugin "Demo Plugin" initialized in `+dir) {
		t.Fatal(e.Out)
	}
	if code := e.Run(append([]string{"init", "sub/Demo"}, flags...)...); code != 1 {
		t.Fatalf("second init exit %d", code)
	}
	e.NoConfig(t)

	e = moduletest.NewEnv(t, "", initcmd.Module())
	if code := e.Run(append([]string{"init"}, flags...)...); code != 0 {
		t.Fatalf("init exit %d: %s", code, e.ErrB)
	}
	if got := moduletest.ReadFile(t, filepath.Join(e.Work, "dleplugin.yml")); got != want {
		t.Fatalf("init yml mismatch:\n%s", got)
	}
	e.NoConfig(t)
}

func TestInitPrompts(t *testing.T) {
	e := moduletest.NewEnv(t, "\n\n\n\n", initcmd.Module())
	dir := filepath.Join(e.Work, "My")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	e.RT.SetWorkDir(dir)
	if code := e.Run("init"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	got := moduletest.ReadFile(t, filepath.Join(dir, "dleplugin.yml"))
	for _, s := range []string{"slug: My\n", "name: My\n", "description: ''\n", "namespace: My\n", "mount: engine/My\n"} {
		if !strings.Contains(got, s) {
			t.Fatalf("missing %q in\n%s", s, got)
		}
	}
	if !strings.Contains(e.Out.String(), "Plugin slug [My]: ") {
		t.Fatal(e.Out)
	}

	e = moduletest.NewEnv(t, "s1\n\nsome desc\nVendor\\S1\n", initcmd.Module())
	if code := e.Run("init", "new"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	got = moduletest.ReadFile(t, filepath.Join(e.Work, "new", "dleplugin.yml"))
	for _, s := range []string{"slug: s1\n", "name: s1\n", "description: some desc\n", "namespace: Vendor\\S1\n", "mount: engine/Vendor/S1\n"} {
		if !strings.Contains(got, s) {
			t.Fatalf("missing %q in\n%s", s, got)
		}
	}
	if !strings.Contains(e.Out.String(), "Plugin slug [new]: ") || !strings.Contains(e.Out.String(), "PHP namespace [s1]: ") {
		t.Fatal(e.Out)
	}

	e = moduletest.NewEnv(t, "\n\n\n\nAcme\\MyPlugin\n", initcmd.Module())
	if code := e.Run("init", "my-plugin"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	if !strings.Contains(e.Out.String(), "PHP namespace: PHP namespace: ") {
		t.Fatal(e.Out)
	}
	got = moduletest.ReadFile(t, filepath.Join(e.Work, "my-plugin", "dleplugin.yml"))
	if !strings.Contains(got, "namespace: Acme\\MyPlugin\nmount: engine/Acme/MyPlugin\n") {
		t.Fatal(got)
	}
}
