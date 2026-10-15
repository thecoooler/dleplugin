package convertcmd_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/convertcmd"
	"github.com/thecoooler/dleplugin/internal/modules/moduletest"
)

var cyrillic = regexp.MustCompile(`\p{Cyrillic}`)

func TestHelp(t *testing.T) {
	for _, args := range [][]string{
		{}, {"convert"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := moduletest.NewEnv(t, "", convertcmd.Module())
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
	e := moduletest.NewEnv(t, "", convertcmd.Module())
	app.NewRootCommand(e.RT, convertcmd.Module())
	if errs := e.RT.ModuleErrors(); len(errs) != 0 {
		t.Fatalf("module errors: %v", errs)
	}
}

func TestConvert(t *testing.T) {
	e := moduletest.NewEnv(t, "", convertcmd.Module())
	xml := moduletest.Golden(t, "convert", "Basic", "Basic.xml")
	if code := e.Run("convert", xml, "out"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	got := moduletest.ReadFile(t, filepath.Join(e.Work, "out", "dleplugin.yml"))
	if want := moduletest.ReadFile(t, moduletest.Golden(t, "convert", "Basic", "expected.yml")); got != want {
		t.Fatalf("yml mismatch:\n%s", got)
	}
	if !strings.Contains(e.Out.String(), "File dleplugin.yml was created") {
		t.Fatal(e.Out)
	}
	if code := e.Run("convert", xml, "out"); code != 1 {
		t.Fatalf("second exit %d", code)
	}
	if !strings.Contains(e.ErrB.String(), "✖ Error: dleplugin.yml or the src directory already exists") {
		t.Fatal(e.ErrB)
	}
	e.NoConfig(t)
}
