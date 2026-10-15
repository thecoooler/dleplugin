package diffcmd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/buildcmd"
	"github.com/thecoooler/dleplugin/internal/modules/diffcmd"
	"github.com/thecoooler/dleplugin/internal/modules/moduletest"
)

var cyrillic = regexp.MustCompile(`\p{Cyrillic}`)

func TestHelp(t *testing.T) {
	for _, args := range [][]string{
		{}, {"diff"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := moduletest.NewEnv(t, "", buildcmd.Module(), diffcmd.Module())
			if code := e.Run(append(args, "--help")...); code != 0 {
				t.Fatalf("exit %d: %s", code, e.ErrB)
			}
			out := e.Out.String()
			if !strings.Contains(out, "Usage:") {
				t.Fatalf("no Usage: %s", out)
			}
			if len(args) == 1 && args[0] == "diff" && !strings.Contains(out, "diff SOURCE1 SOURCE2") {
				t.Fatalf("missing source usage: %s", out)
			}
			if cyrillic.MatchString(out) {
				t.Fatalf("cyrillic in help: %s", out)
			}
			e.NoConfig(t)
		})
	}
}

func TestRegistersCleanly(t *testing.T) {
	e := moduletest.NewEnv(t, "", buildcmd.Module(), diffcmd.Module())
	app.NewRootCommand(e.RT, diffcmd.Module())
	if errs := e.RT.ModuleErrors(); len(errs) != 0 {
		t.Fatalf("module errors: %v", errs)
	}
}

func TestDiff(t *testing.T) {
	e := moduletest.NewEnv(t, "", buildcmd.Module(), diffcmd.Module())
	xml := moduletest.ReadFile(t, moduletest.Golden(t, "convert", "Basic", "Basic.xml"))
	for _, n := range []string{"a.xml", "b.xml"} {
		if err := os.WriteFile(filepath.Join(e.Work, n), []byte(xml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.Work, "invalid.xml"), []byte("<broken>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		left, right, want string
	}{
		{"./missing.xml", "b.xml", "Source not found: ./missing.xml"},
		{"a.xml", "missing.xml", "Source not found: missing.xml"},
		{".", "a.xml", ".: Configuration file dleplugin.yml was not found"},
		{"a.xml", ".", ".: Configuration file dleplugin.yml was not found"},
		{"./invalid.xml", "b.xml", "./invalid.xml: parse xml: XML syntax error on line 1: unexpected EOF"},
		{"a.xml", "invalid.xml", "invalid.xml: parse xml: XML syntax error on line 1: unexpected EOF"},
	} {
		t.Run(tt.left+" -> "+tt.right, func(t *testing.T) {
			e.Out.Reset()
			e.ErrB.Reset()
			if code := e.Run("diff", tt.left, tt.right); code != 1 {
				t.Fatalf("exit %d", code)
			}
			if want := "✖ Error: " + tt.want + "\n"; e.ErrB.String() != want {
				t.Fatalf("stderr = %q, want %q", e.ErrB.String(), want)
			}
			if strings.Contains(e.Out.String(), "Comparing") {
				t.Fatalf("comparison announced before validation: %s", e.Out)
			}
		})
	}
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("no diff binary")
	}
	e.Out.Reset()
	if code := e.Run("diff", "a.xml", "b.xml"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	if !strings.Contains(e.Out.String(), "Comparing a.xml -> b.xml") {
		t.Fatal(e.Out)
	}
	e.NoConfig(t)
}

func TestDiffProject(t *testing.T) {
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("no diff binary")
	}
	for _, fixture := range []struct{ name, artifact string }{
		{"embedded", "ExamplePlugin.xml"},
		{"split", "ExamplePlugin.zip"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			e := moduletest.NewEnv(t, "", buildcmd.Module(), diffcmd.Module())
			moduletest.CopyDir(t, moduletest.Golden(t, "plugin", "complete", "input"), e.Work)
			if fixture.name == "split" {
				manifest := filepath.Join(e.Work, "dleplugin.yml")
				data := strings.Replace(moduletest.ReadFile(t, manifest), "split: false", "split: true", 1)
				if err := os.WriteFile(manifest, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(e.Work, "src", ".htaccess"), []byte("Deny from all\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if code := e.Run("build", "--version", "1.2.3"); code != 0 {
				t.Fatalf("build exit %d: %s", code, e.ErrB)
			}
			e.Out.Reset()
			artifact := filepath.Join("dist", fixture.artifact)
			if code := e.Run("diff", "src", artifact); code != 0 {
				t.Fatalf("diff exit %d: %s", code, e.ErrB)
			}
			want := "Comparing src -> " + artifact
			if !strings.Contains(e.Out.String(), want) || strings.Contains(e.Out.String(), "one.yaml") {
				t.Fatalf("unexpected output: %s", e.Out)
			}
			manifest := filepath.Join(e.Work, "dleplugin.yml")
			data := strings.Replace(moduletest.ReadFile(t, manifest), "description: Example DLE plugin", "description: Changed", 1)
			if err := os.WriteFile(manifest, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			e.Out.Reset()
			if code := e.Run("diff", artifact, "."); code != 0 {
				t.Fatalf("differences exit %d: %s", code, e.ErrB)
			}
			if !strings.Contains(e.Out.String(), "description: Changed") {
				t.Fatalf("missing differences: %s", e.Out)
			}
			e.NoConfig(t)
		})
	}
}
