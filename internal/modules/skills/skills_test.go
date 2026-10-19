package skills_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thecoooler/dleplugin/internal/modules/moduletest"
	"github.com/thecoooler/dleplugin/internal/modules/skills"
)

func TestLifecycle(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		for _, global := range []bool{false, true} {
			label := agent + "/project"
			if global {
				label = agent + "/global"
			}
			t.Run(label, func(t *testing.T) {
				e := moduletest.NewEnv(t, "", skills.Module())
				e.RT.Version = "v1.2.3"
				base, hidden := e.Work, ".agents"
				if global {
					base = os.Getenv("HOME")
				}
				if agent == "claude" {
					hidden = ".claude"
				}
				target := filepath.Join(base, hidden, "skills")
				run := func(action string, extra ...string) int {
					args := []string{"skills", action, "--agent", agent}
					if global {
						args = append(args, "--global")
					}
					return e.Run(append(args, extra...)...)
				}
				// Uninstall is idempotent and does not create directories.
				if code := run("uninstall"); code != 0 {
					t.Fatal(e.ErrB.String())
				}
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatalf("uninstall created directory: %v", err)
				}
				if code := run("install"); code != 0 {
					t.Fatal(e.ErrB.String())
				}
				for _, name := range []string{"dleplugin", "dle-admin-ui"} {
					content := moduletest.ReadFile(t, filepath.Join(target, name, "SKILL.md"))
					if !strings.Contains(content, "name: "+name) {
						t.Fatalf("wrong skill: %s", content)
					}
					receipt := moduletest.ReadFile(t, filepath.Join(target, name, ".dleplugin-managed.json"))
					if !strings.Contains(receipt, `"version": "v1.2.3"`) {
						t.Fatal(receipt)
					}
				}
				// Updating uses the current CLI version and leaves unrelated skills alone.
				other := filepath.Join(target, "other")
				if err := os.Mkdir(other, 0o755); err != nil {
					t.Fatal(err)
				}
				e.RT.Version = "v2.0.0"
				if code := run("install"); code != 0 {
					t.Fatal(e.ErrB.String())
				}
				if !strings.Contains(moduletest.ReadFile(t, filepath.Join(target, "dleplugin", ".dleplugin-managed.json")), `"version": "v2.0.0"`) {
					t.Fatal("version not updated")
				}
				changed := filepath.Join(target, "dle-admin-ui", "SKILL.md")
				if err := os.WriteFile(changed, []byte("user edit"), 0o644); err != nil {
					t.Fatal(err)
				}
				before := moduletest.ReadFile(t, filepath.Join(target, "dleplugin", ".dleplugin-managed.json"))
				e.RT.Version = "v3.0.0"
				for _, action := range []string{"install", "uninstall"} {
					if code := run(action); code != 1 {
						t.Fatalf("%s accepted changed files", action)
					}
					if got := moduletest.ReadFile(t, changed); got != "user edit" {
						t.Fatal("lost edit")
					}
					if got := moduletest.ReadFile(t, filepath.Join(target, "dleplugin", ".dleplugin-managed.json")); got != before {
						t.Fatal("partial operation")
					}
				}
				if code := run("install", "--force"); code != 0 {
					t.Fatal(e.ErrB.String())
				}
				if code := run("uninstall"); code != 0 {
					t.Fatal(e.ErrB.String())
				}
				if code := run("uninstall"); code != 0 {
					t.Fatal(e.ErrB.String())
				}
				if _, err := os.Stat(other); err != nil {
					t.Fatal("unrelated skill removed", err)
				}
				for _, name := range []string{"dleplugin", "dle-admin-ui"} {
					if _, err := os.Stat(filepath.Join(target, name)); !os.IsNotExist(err) {
						t.Fatalf("skill not removed: %v", err)
					}
				}
				e.NoConfig(t)
			})
		}
	}
}

func TestHelpListAndArguments(t *testing.T) {
	e := moduletest.NewEnv(t, "", skills.Module())
	root := []string{"--help"}
	for _, args := range [][]string{root, {"skills", "--help"}, {"skills", "list", "--help"}, {"skills", "install", "--help"}, {"skills", "uninstall", "--help"}, {"skills", "list"}} {
		e.Out.Reset()
		if code := e.Run(args...); code != 0 {
			t.Fatalf("%v: %s", args, e.ErrB)
		}
		if len(args) == 1 && !strings.Contains(e.Out.String(), "AI agents:") {
			t.Fatal(e.Out.String())
		}
		if args[len(args)-1] == "list" {
			for _, s := range []string{"dleplugin", "dle-admin-ui", e.RT.Version, "9a24d63"} {
				if !strings.Contains(e.Out.String(), s) {
					t.Fatalf("list lacks %q", s)
				}
			}
		}
	}
	for _, args := range [][]string{{"skills", "install"}, {"skills", "uninstall"}, {"skills", "install", "--agent", "unknown"}, {"skills", "install", "--agent", "codex", "extra"}, {"skills", "list", "extra"}, {"skills", "uninstall", "--agent", "claude", "--force"}} {
		if code := e.Run(args...); code != 1 {
			t.Fatalf("accepted %v", args)
		}
	}
	e.NoConfig(t)
}

func TestScopesStayIndependent(t *testing.T) {
	e := moduletest.NewEnv(t, "", skills.Module())
	for _, args := range [][]string{
		{"skills", "install", "--agent", "codex"},
		{"skills", "install", "--agent", "claude"},
		{"skills", "install", "--agent", "codex", "--global"},
		{"skills", "uninstall", "--agent", "codex"},
	} {
		if code := e.Run(args...); code != 0 {
			t.Fatal(e.ErrB.String())
		}
	}
	for _, dir := range []string{filepath.Join(e.Work, ".claude"), filepath.Join(os.Getenv("HOME"), ".agents")} {
		for _, name := range []string{"dleplugin", "dle-admin-ui"} {
			if _, err := os.Stat(filepath.Join(dir, "skills", name, "SKILL.md")); err != nil {
				t.Fatal("other scope removed", err)
			}
		}
	}
}

func TestDowngradeWarning(t *testing.T) {
	e := moduletest.NewEnv(t, "", skills.Module())
	install := func(version string) {
		t.Helper()
		e.RT.Version = version
		e.Out.Reset()
		e.ErrB.Reset()
		if code := e.Run("skills", "install", "--agent", "codex"); code != 0 {
			t.Fatal(e.ErrB.String())
		}
	}
	install("v2.0.0")
	install("v1.0.0")
	for _, name := range []string{"dleplugin", "dle-admin-ui"} {
		if !strings.Contains(e.ErrB.String(), "Downgrading "+name+" skills from CLI v2.0.0 to v1.0.0") {
			t.Fatal(e.ErrB.String())
		}
		receipt := moduletest.ReadFile(t, filepath.Join(e.Work, ".agents", "skills", name, ".dleplugin-managed.json"))
		if !strings.Contains(receipt, `"version": "v1.0.0"`) {
			t.Fatal("downgrade not installed", receipt)
		}
	}
	install("v3.0.0")
	if strings.Contains(e.ErrB.String(), "Downgrading") {
		t.Fatal("upgrade warned as downgrade")
	}
}
