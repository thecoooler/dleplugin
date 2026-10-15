package buildcmd_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/buildcmd"
	"github.com/thecoooler/dleplugin/internal/modules/moduletest"
)

var cyrillic = regexp.MustCompile(`\p{Cyrillic}`)

func TestHelp(t *testing.T) {
	for _, args := range [][]string{
		{}, {"build"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := moduletest.NewEnv(t, "", buildcmd.Module())
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
	e := moduletest.NewEnv(t, "", buildcmd.Module())
	app.NewRootCommand(e.RT, buildcmd.Module())
	if errs := e.RT.ModuleErrors(); len(errs) != 0 {
		t.Fatalf("module errors: %v", errs)
	}
}

func TestBuild(t *testing.T) {
	e := moduletest.NewEnv(t, "", buildcmd.Module())
	moduletest.CopyDir(t, moduletest.Golden(t, "plugin", "complete", "input"), e.Work)
	if code := e.Run("build"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	got := moduletest.ReadFile(t, filepath.Join(e.Work, "dist", "ExamplePlugin.xml"))
	if want := moduletest.ReadFile(t, moduletest.Golden(t, "plugin", "complete", "build_nocommit.xml")); got != want {
		t.Fatal("xml differs from golden")
	}
	if _, err := os.Stat(filepath.Join(e.Work, "dist", "README.html")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Out.String(), "Plugin built successfully") {
		t.Fatal(e.Out)
	}
	e.NoConfig(t)

	e = moduletest.NewEnv(t, "", buildcmd.Module())
	moduletest.CopyDir(t, moduletest.Golden(t, "plugin", "quirks_files", "input"), e.Work)
	if code := e.Run("build"); code != 0 {
		t.Fatalf("split exit %d: %s", code, e.ErrB)
	}
	if _, err := os.Stat(filepath.Join(e.Work, "dist", "QF.zip")); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(e.Work, "dist", "*.xml"))
	if len(matches) != 0 {
		t.Fatalf("unexpected xml: %v", matches)
	}
}

func TestBuildVersion(t *testing.T) {
	e := moduletest.NewEnv(t, "", buildcmd.Module())
	moduletest.CopyDir(t, moduletest.Golden(t, "plugin", "complete", "input"), e.Work)
	if code := e.Run("build", "--version", "1.2.0"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	got := moduletest.ReadFile(t, filepath.Join(e.Work, "dist", "ExamplePlugin.xml"))
	want := strings.Replace(moduletest.ReadFile(t, moduletest.Golden(t, "plugin", "complete", "build_nocommit.xml")),
		"<version></version>", "<version>1.2.0</version>", 1)
	if got != want {
		t.Fatal("xml differs from golden with version")
	}
}

func TestBuildOutsidePlugin(t *testing.T) {
	e := moduletest.NewEnv(t, "", buildcmd.Module())
	if code := e.Run("build"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(e.ErrB.String(), "✖ Error: Configuration file dleplugin.yml was not found") {
		t.Fatal(e.ErrB)
	}
}
