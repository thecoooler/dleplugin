package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/thecoooler/dleplugin/internal/config"
)

type fakeMod struct {
	name string
	cmds []string
	run  func(*cobra.Command, []string) error
}

func (f fakeMod) Name() string { return f.name }
func (f fakeMod) Commands(*Runtime) []*cobra.Command {
	var out []*cobra.Command
	for _, c := range f.cmds {
		run := f.run
		if run == nil {
			run = func(*cobra.Command, []string) error { return nil }
		}
		out = append(out, &cobra.Command{Use: c, Short: "cmd " + c, RunE: run})
	}
	return out
}

type verMod struct{ fakeMod }

func (verMod) Version() string { return "1.2.3" }

func setup(t *testing.T) (*Runtime, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	var o, e bytes.Buffer
	p := filepath.Join(t.TempDir(), ".config", "dleplugin", "config.yml")
	rt := NewRuntime(strings.NewReader(""), &o, &e, p)
	rt.Version = "9.9"
	return rt, &o, &e, p
}

func TestHelp(t *testing.T) {
	rt, o, _, p := setup(t)
	if code := Execute(context.Background(), rt, []string{"--help"}, fakeMod{name: "build", cmds: []string{"build"}}); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(o.String(), "--verbose") {
		t.Errorf("help = %q", o.String())
	}
	if regexp.MustCompile(`\p{Cyrillic}`).MatchString(o.String()) {
		t.Error("cyrillic in help")
	}
	if strings.Contains(o.String(), "completion") {
		t.Error("completion present")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("config created")
	}
}

func TestErrorOutput(t *testing.T) {
	rt, _, e, _ := setup(t)
	m := fakeMod{name: "build", cmds: []string{"boom"}, run: func(*cobra.Command, []string) error { return errors.New("Something broke") }}
	if code := Execute(context.Background(), rt, []string{"boom"}, m); code != 1 {
		t.Fatal(code)
	}
	if e.String() != "✖ Error: Something broke\n" {
		t.Errorf("err = %q", e.String())
	}
}

func TestVerboseChain(t *testing.T) {
	rt, _, e, _ := setup(t)
	inner := errors.New("root cause")
	m := fakeMod{name: "build", cmds: []string{"boom"}, run: func(*cobra.Command, []string) error {
		return fmt.Errorf("outer: %w", inner)
	}}
	if code := Execute(context.Background(), rt, []string{"-v", "boom"}, m); code != 1 {
		t.Fatal(code)
	}
	if !strings.Contains(e.String(), "✖ Error: outer: root cause\n") || !strings.Contains(e.String(), "  caused by [*errors.errorString]: root cause\n") {
		t.Errorf("err = %q", e.String())
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"nope"}, {"--nope"}} {
		rt, _, e, _ := setup(t)
		if code := Execute(context.Background(), rt, args, fakeMod{name: "build", cmds: []string{"build"}}); code != 1 || !strings.HasPrefix(e.String(), "✖ Error: ") {
			t.Errorf("%v: code %d err %q", args, code, e.String())
		}
	}
}

func TestModuleRegistration(t *testing.T) {
	rt, _, _, _ := setup(t)
	mods := []Module{
		fakeMod{name: "build", cmds: []string{"build"}},
		fakeMod{name: "zeta", cmds: []string{"z"}},
		verMod{fakeMod{name: "alpha", cmds: []string{"a"}}},
		fakeMod{name: "bad", cmds: []string{"build", "help", "q"}},
		fakeMod{name: "dup", cmds: []string{"d1"}},
		fakeMod{name: "dup", cmds: []string{"d2"}},
	}
	root := NewRootCommand(rt, mods...)
	var got []string
	for _, mi := range rt.Modules() {
		line := mi.Name + " " + mi.Version
		if mi.Err != nil {
			line += " error: " + mi.Err.Error()
		}
		got = append(got, line)
	}
	want := []string{
		"build 9.9",
		"zeta 9.9",
		"alpha 1.2.3",
		"bad 9.9 error: command conflict: 'build', 'help'",
		"dup 9.9 error: duplicate module name provided by 2 modules",
		"dup 9.9 error: duplicate module name provided by 2 modules",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("modules = %q", got)
	}
	if len(rt.ModuleErrors()) != 2 {
		t.Errorf("errors = %v", rt.ModuleErrors())
	}
	// good modules still work; rejected ones do not
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, n := range []string{"build", "z", "a"} {
		if !names[n] {
			t.Errorf("missing %s", n)
		}
	}
	for _, n := range []string{"q", "d1", "d2", "extensions"} {
		if names[n] {
			t.Errorf("unexpected %s", n)
		}
	}
}

func TestRequireConfig(t *testing.T) {
	rt, _, e, p := setup(t)
	called := false
	mod := fakeMod{name: "build", cmds: []string{"work"}, run: RequireConfig(rt, func(*cobra.Command, []string) error {
		called = true
		return nil
	})}
	if code := Execute(context.Background(), rt, []string{"work"}, mod); code != 1 {
		t.Fatal(code)
	}
	if called || !strings.Contains(e.String(), "Fill required configuration values") {
		t.Errorf("called=%v err=%q", called, e.String())
	}
	if _, err := os.Stat(p); err != nil {
		t.Error("config not created")
	}

	// valid config
	if err := os.WriteFile(p, []byte("host_dir: /h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt3, _, _, _ := setup(t)
	rt3.Config = newStore(p)
	mod = fakeMod{name: "build", cmds: []string{"work", "opt"}, run: RequireConfig(rt3, func(*cobra.Command, []string) error {
		called = true
		return nil
	})}
	if code := Execute(context.Background(), rt3, []string{"work"}, mod); code != 0 || !called {
		t.Fatalf("code %d called %v", code, called)
	}
	if rt3.CoreConfig() == nil || rt3.CoreConfig().HostDir != "/h" {
		t.Errorf("core = %v", rt3.CoreConfig())
	}
}

func TestOptionalConfig(t *testing.T) {
	rt, _, _, p := setup(t)
	called := false
	mod := fakeMod{name: "build", cmds: []string{"opt"}, run: OptionalConfig(rt, func(*cobra.Command, []string) error {
		called = true
		return nil
	})}
	if code := Execute(context.Background(), rt, []string{"opt"}, mod); code != 0 || !called {
		t.Fatal(code)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("config created")
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("host: ["), 0o644)
	rt2, _, e, _ := setup(t)
	rt2.Config = newStore(p)
	mod.run = OptionalConfig(rt2, func(*cobra.Command, []string) error { return nil })
	if code := Execute(context.Background(), rt2, []string{"opt"}, mod); code != 1 || !strings.Contains(e.String(), "Invalid YAML") {
		t.Fatalf("code %d %q", code, e.String())
	}
}

func newStore(p string) *config.Store { return config.NewStore(p) }

func TestSilentExit(t *testing.T) {
	rt, o, e, _ := setup(t)
	m := fakeMod{name: "build", cmds: []string{"check"}, run: func(*cobra.Command, []string) error {
		return fmt.Errorf("wrapped: %w", &SilentExit{Code: 3})
	}}
	if code := Execute(context.Background(), rt, []string{"-v", "check"}, m); code != 3 {
		t.Fatal(code)
	}
	if o.String() != "" || e.String() != "" {
		t.Errorf("out %q err %q", o, e)
	}
}

func TestUnknownGroupFallsBack(t *testing.T) {
	var out bytes.Buffer
	rt := NewRuntime(strings.NewReader(""), &out, &out, "")
	mod := fakeMod{name: "x", cmds: []string{"known", "unknown"}}
	root := NewRootCommand(rt, groupMod{mod, map[string]string{"known": GroupCheck, "unknown": "nope"}})
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "Plugin project:") || !regexp.MustCompile(`\n\nChecks:\n  known +cmd known\n\nAdditional Commands:\n  unknown +cmd unknown\n  help `).MatchString(s) {
		t.Errorf("help:\n%s", s)
	}
}

type groupMod struct {
	fakeMod
	groups map[string]string
}

func (g groupMod) Commands(rt *Runtime) []*cobra.Command {
	cmds := g.fakeMod.Commands(rt)
	for _, c := range cmds {
		c.GroupID = g.groups[c.Name()]
	}
	return cmds
}
