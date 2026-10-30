package cache_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thecoooler/dleplugin/internal/app"
	"github.com/thecoooler/dleplugin/internal/modules/cache"
	"github.com/thecoooler/dleplugin/internal/modules/moduletest"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newEnv(t *testing.T) (*moduletest.Env, string) {
	t.Helper()
	e := moduletest.NewEnv(t, "", cache.Module())
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", home)
	return e, filepath.Join(home, "dleplugin")
}

// layout creates one run site, an old and a current DLE base and a static
// work directory.
func layout(t *testing.T, root string) {
	t.Helper()
	write(t, filepath.Join(root, "test", "run", "demo-abc", "index.php"), strings.Repeat("r", 10))
	write(t, filepath.Join(root, "test", "dle-current", "site", "index.php"), "current")
	write(t, filepath.Join(root, "test", "dle-old", "site", "index.php"), strings.Repeat("o", 20))
	write(t, filepath.Join(root, "test", "dle-current", ".ready"), "21.0\n")
	write(t, filepath.Join(root, "test", "dle-old", ".ready"), "20.0\n")
	write(t, filepath.Join(root, "static", "Demo-abc", "phpstan.neon"), strings.Repeat("s", 30))
	now := time.Now()
	for path, mod := range map[string]time.Time{
		filepath.Join(root, "test", "dle-current"):     now.Add(-time.Minute),
		filepath.Join(root, "test", "dle-old"):         now.Add(-48 * time.Hour),
		filepath.Join(root, "test", "run", "demo-abc"): now.Add(-48 * time.Hour),
		filepath.Join(root, "static", "Demo-abc"):      now.Add(-48 * time.Hour),
	} {
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRegistersCleanly(t *testing.T) {
	e := moduletest.NewEnv(t, "", cache.Module())
	app.NewRootCommand(e.RT, cache.Module())
	if errs := e.RT.ModuleErrors(); len(errs) != 0 {
		t.Fatalf("module errors: %v", errs)
	}
}

func TestHelp(t *testing.T) {
	e, _ := newEnv(t)
	for _, args := range [][]string{{"cache", "--help"}, {"cache", "clean", "--help"}} {
		e.Out.Reset()
		if code := e.Run(args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, e.ErrB)
		}
		if !strings.Contains(e.Out.String(), "Usage:") {
			t.Fatalf("%v: %s", args, e.Out)
		}
	}
	e.Out.Reset()
	if code := e.Run("cache", "clean", "--help"); code != 0 {
		t.Fatal(code)
	}
	for _, s := range []string{"--all", "--older-than", "--docker", "--dry-run", "DLE base"} {
		if !strings.Contains(e.Out.String(), s) {
			t.Errorf("help lacks %q", s)
		}
	}
	e.NoConfig(t)
}

func TestCleanDryRunAndClean(t *testing.T) {
	e, root := newEnv(t)
	layout(t, root)
	if code := e.Run("cache", "clean", "--dry-run"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	for _, s := range []string{
		"Would remove DLE base dle-old",
		"Would remove run site demo-abc",
		"Would remove static cache Demo-abc",
		"Would free",
		"(3 entries).",
	} {
		if !strings.Contains(e.Out.String(), s) {
			t.Errorf("dry run output lacks %q:\n%s", s, e.Out)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "test", "run", "demo-abc"),
		filepath.Join(root, "test", "dle-old"),
		filepath.Join(root, "static", "Demo-abc"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("dry run removed %s: %v", path, err)
		}
	}

	e.Out.Reset()
	e.ErrB.Reset()
	if code := e.Run("cache", "clean"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	for _, s := range []string{"Removed DLE base dle-old", "Removed run site demo-abc", "Removed static cache Demo-abc", "Freed"} {
		if !strings.Contains(e.Out.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, e.Out)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "test", "run", "demo-abc"),
		filepath.Join(root, "test", "dle-old"),
		filepath.Join(root, "static", "Demo-abc"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s remains: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "test", "dle-current")); err != nil {
		t.Errorf("current base removed: %v", err)
	}
	e.NoConfig(t)

	// A second run has nothing left to do.
	e.Out.Reset()
	if code := e.Run("cache", "clean"); code != 0 || !strings.Contains(e.Out.String(), "Cache is clean.") {
		t.Errorf("empty clean: exit %d: %s", code, e.Out)
	}
}

func TestCleanAllAndOlderThan(t *testing.T) {
	e, root := newEnv(t)
	layout(t, root)
	write(t, filepath.Join(root, "test", "dle_trial.zip"), "trial")
	if code := e.Run("cache", "clean", "--all", "--dry-run"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	for _, s := range []string{"DLE base dle-current", "trial archive dle_trial.zip"} {
		if !strings.Contains(e.Out.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, e.Out)
		}
	}
	e.Out.Reset()
	// Older than 24h leaves the recent base and the trial archive alone.
	if code := e.Run("cache", "clean", "--older-than", "24h", "--dry-run"); code != 0 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	for _, s := range []string{"DLE base dle-current", "dle_trial.zip"} {
		if strings.Contains(e.Out.String(), s) {
			t.Errorf("older-than removed %q:\n%s", s, e.Out)
		}
	}
	if !strings.Contains(e.Out.String(), "Would remove DLE base dle-old") {
		t.Errorf("older-than kept an old base:\n%s", e.Out)
	}

	for _, value := range []string{"nonsense", "0s", "-1h"} {
		e.Out.Reset()
		e.ErrB.Reset()
		if code := e.Run("cache", "clean", "--older-than", value); code != 1 || !strings.Contains(e.ErrB.String(), "Invalid --older-than duration") {
			t.Errorf("invalid duration %q: exit %d, err %q", value, code, e.ErrB)
		}
	}
}

func TestCleanReportsErrors(t *testing.T) {
	e, root := newEnv(t)
	// A file where the run-site directory belongs makes the read fail.
	write(t, filepath.Join(root, "test", "run"), "not a directory")
	if code := e.Run("cache", "clean"); code != 1 {
		t.Fatalf("exit %d: %s", code, e.ErrB)
	}
	if strings.Contains(e.Out.String(), "Cache is clean.") || !strings.Contains(e.Out.String(), "Nothing was removed.") {
		t.Errorf("out = %q", e.Out)
	}
	if !strings.Contains(e.ErrB.String(), "read ") {
		t.Errorf("err = %q", e.ErrB)
	}

	e.Out.Reset()
	e.ErrB.Reset()
	if code := e.Run("cache", "clean", "--dry-run"); code != 1 {
		t.Fatalf("dry run exit %d: %s", code, e.ErrB)
	}
	if !strings.Contains(e.Out.String(), "Nothing would be removed.") || strings.Contains(e.Out.String(), "Nothing was removed.") {
		t.Errorf("dry run out = %q", e.Out)
	}
	e.NoConfig(t)
}

func TestCleanFatalError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME resolution differs on Windows")
	}
	e, _ := newEnv(t)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	if code := e.Run("cache", "clean"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if e.Out.Len() != 0 || strings.Contains(e.Out.String(), "Cache is clean.") {
		t.Errorf("out = %q", e.Out)
	}
	if !strings.Contains(e.ErrB.String(), "$HOME is not defined") {
		t.Errorf("err = %q", e.ErrB)
	}
}

func TestCleanDockerWithoutDocker(t *testing.T) {
	e, _ := newEnv(t)
	t.Setenv("PATH", t.TempDir())
	if code := e.Run("cache", "clean", "--docker"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(e.ErrB.String(), "Docker not found") {
		t.Errorf("err = %q", e.ErrB)
	}
}

func TestCacheWithoutSubcommand(t *testing.T) {
	e, _ := newEnv(t)
	if code := e.Run("cache"); code != 0 || !strings.Contains(e.Out.String(), "clean") {
		t.Fatalf("exit %d: %s", code, e.Out)
	}
	e.NoConfig(t)
}
