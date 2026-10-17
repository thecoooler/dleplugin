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
			if len(args) > 0 {
				for _, flag := range []string{"--version", "--upgrade-url"} {
					if !strings.Contains(out, flag) {
						t.Errorf("help lacks %s", flag)
					}
				}
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

	manifest := filepath.Join(e.Work, "dleplugin.yml")
	b, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append([]byte("version: 2.0.0\n"), b...), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"build"}, "2.0.0"},
		{[]string{"build", "--version", "3.0.0"}, "3.0.0"},
	} {
		if code := e.Run(tc.args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", tc.args, code, e.ErrB)
		}
		got := moduletest.ReadFile(t, filepath.Join(e.Work, "dist", "ExamplePlugin.xml"))
		if !strings.Contains(got, "<version>"+tc.want+"</version>") {
			t.Errorf("%v: xml lacks version %s", tc.args, tc.want)
		}
	}
}

func TestBuildUpgradeURL(t *testing.T) {
	e := moduletest.NewEnv(t, "", buildcmd.Module())
	moduletest.CopyDir(t, moduletest.Golden(t, "plugin", "complete", "input"), e.Work)
	manifest := filepath.Join(e.Work, "dleplugin.yml")
	b, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append([]byte("upgrade_url: https://example.com/manifest.xml\n"), b...), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"build"}, "https://example.com/manifest.xml"},
		{[]string{"build", "--upgrade-url", "https://example.com/flag.xml"}, "https://example.com/flag.xml"},
	} {
		if code := e.Run(tc.args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", tc.args, code, e.ErrB)
		}
		got := moduletest.ReadFile(t, filepath.Join(e.Work, "dist", "ExamplePlugin.xml"))
		if !strings.Contains(got, "<upgradeurl>"+tc.want+"</upgradeurl>") {
			t.Errorf("%v: xml lacks upgradeurl %s", tc.args, tc.want)
		}
	}
}

func TestBuildUpgradeURLErrors(t *testing.T) {
	e := moduletest.NewEnv(t, "", buildcmd.Module())
	moduletest.CopyDir(t, moduletest.Golden(t, "plugin", "complete", "input"), e.Work)
	for _, tc := range []struct {
		name string
		args []string
		err  string
	}{
		{"http", []string{"build", "--upgrade-url", "http://example.com/plugin.xml"}, `invalid upgrade URL "http://example.com/plugin.xml": scheme must be https`},
		{"blank flag", []string{"build", "--upgrade-url", "  "}, "--upgrade-url is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := e.Run(tc.args...); code != 1 {
				t.Fatalf("exit %d", code)
			}
			if !strings.Contains(e.ErrB.String(), tc.err) {
				t.Errorf("err = %q, want %q", e.ErrB.String(), tc.err)
			}
		})
	}

	// A manifest upgrade URL is validated when the plugin loads.
	manifest := filepath.Join(e.Work, "dleplugin.yml")
	c, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append([]byte("upgrade_url: ftp://example.com/plugin.xml\n"), c...), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := e.Run("build"); code != 1 {
		t.Fatal("exit")
	}
	if !strings.Contains(e.ErrB.String(), "upgrade_url: invalid upgrade URL") {
		t.Errorf("err = %q", e.ErrB.String())
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
